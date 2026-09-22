package resume

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/howlcipher/howlforge/internal/handoff"
)

// Inspector reads repository state. It is an interface so resume rules can be
// tested exhaustively without a real repository, and so a caller that has no
// git available can supply an inspector that declines instead of failing.
type Inspector interface {
	// CommitExists reports whether a commit object is present.
	CommitExists(ctx context.Context, repoDir, commit string) (bool, error)
	// HeadCommit returns the full hash currently checked out.
	HeadCommit(ctx context.Context, repoDir string) (string, error)
	// IsAncestor reports whether ancestor is reachable from descendant.
	IsAncestor(ctx context.Context, repoDir, ancestor, descendant string) (bool, error)
	// IsDirty reports whether the working tree has uncommitted changes.
	IsDirty(ctx context.Context, repoDir string) (bool, error)
}

// ErrGitUnavailable means no usable git inspection is possible.
var ErrGitUnavailable = errors.New("git inspection is unavailable")

// DefaultTimeout bounds every git invocation.
const DefaultTimeout = 15 * time.Second

// GitInspector shells out to git.
//
// This is the only place in HowlForge that starts a process. Everything about
// it is deliberately narrow: the binary is resolved once via PATH, the
// argument vector is fixed, no shell is involved, every commit argument is
// validated as a hexadecimal object name before it is passed, and each call is
// bounded by a context timeout. Runtime definitions cannot reach this code,
// which is what keeps a configuration file from becoming an execution vector.
type GitInspector struct {
	binary  string
	timeout time.Duration
}

// NewGitInspector locates git. It returns ErrGitUnavailable when git is not on
// PATH, so the caller can degrade to NEEDS_RECONCILIATION rather than fail.
func NewGitInspector() (*GitInspector, error) {
	binary, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGitUnavailable, err)
	}
	return &GitInspector{binary: binary, timeout: DefaultTimeout}, nil
}

// run executes one fixed git subcommand inside repoDir.
func (g *GitInspector) run(ctx context.Context, repoDir string, args ...string) (string, error) {
	dir, err := safeRepoDir(repoDir)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, g.binary, full...)
	// A fixed, minimal environment keeps operator shell configuration and any
	// inherited credential helper out of the inspection.
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"HOME=" + os.Getenv("HOME"),
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = nil

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("git %s timed out after %s", strings.Join(args, " "), g.timeout)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.String(), &gitExitError{
				args:     args,
				exitCode: exitErr.ExitCode(),
				stderr:   strings.TrimSpace(stderr.String()),
			}
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// gitExitError carries a non-zero exit, which several callers treat as a
// meaningful answer rather than a failure.
type gitExitError struct {
	args     []string
	exitCode int
	stderr   string
}

func (e *gitExitError) Error() string {
	if e.stderr != "" {
		return fmt.Sprintf("git %s exited %d: %s", strings.Join(e.args, " "), e.exitCode, e.stderr)
	}
	return fmt.Sprintf("git %s exited %d", strings.Join(e.args, " "), e.exitCode)
}

// safeRepoDir resolves a repository path and refuses anything that is not an
// existing directory. Resolving to an absolute path also guarantees the value
// cannot be read by git as an option.
func safeRepoDir(repoDir string) (string, error) {
	if strings.TrimSpace(repoDir) == "" {
		return "", errors.New("repository directory must not be empty")
	}
	abs, err := filepath.Abs(repoDir)
	if err != nil {
		return "", fmt.Errorf("resolve repository directory %q: %w", repoDir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("repository directory %q: %w", repoDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository path %q is not a directory", repoDir)
	}
	return abs, nil
}

// checkCommitish refuses any commit argument that is not a bare hexadecimal
// object name, before it can reach an argument vector.
func checkCommitish(commit string) error {
	if !handoff.IsCommitish(commit) {
		return fmt.Errorf("refusing to pass %q to git: not a hexadecimal commit hash", commit)
	}
	return nil
}

// CommitExists reports whether a commit object is present in the repository.
func (g *GitInspector) CommitExists(ctx context.Context, repoDir, commit string) (bool, error) {
	if err := checkCommitish(commit); err != nil {
		return false, err
	}
	// cat-file -e answers the question with an exit code and no output, so a
	// missing object is not an error condition here.
	_, err := g.run(ctx, repoDir, "cat-file", "-e", commit+"^{commit}")
	if err != nil {
		var exitErr *gitExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// HeadCommit returns the currently checked out commit.
func (g *GitInspector) HeadCommit(ctx context.Context, repoDir string) (string, error) {
	out, err := g.run(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// IsAncestor reports whether ancestor is reachable from descendant.
func (g *GitInspector) IsAncestor(ctx context.Context, repoDir, ancestor, descendant string) (bool, error) {
	if err := checkCommitish(ancestor); err != nil {
		return false, err
	}
	if err := checkCommitish(descendant); err != nil {
		return false, err
	}
	_, err := g.run(ctx, repoDir, "merge-base", "--is-ancestor", ancestor, descendant)
	if err != nil {
		var exitErr *gitExitError
		if errors.As(err, &exitErr) && exitErr.exitCode == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// IsDirty reports whether the working tree has uncommitted changes.
func (g *GitInspector) IsDirty(ctx context.Context, repoDir string) (bool, error) {
	out, err := g.run(ctx, repoDir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}
