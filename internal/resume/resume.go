// Package resume decides whether a replacement worker may safely continue from
// a handoff bundle.
//
// The governing rule is that indeterminacy never becomes confidence. Any check
// that cannot be established degrades the verdict; nothing degrades it to SAFE.
package resume

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/howlcipher/howlforge/internal/handoff"
)

// State is the resume verdict.
type State string

const (
	// SAFE means every rule passed and work may continue.
	SAFE State = "SAFE"
	// NEEDS_RECONCILIATION means something could not be established. A human
	// or a reconciling worker must look before continuing.
	NEEDS_RECONCILIATION State = "NEEDS_RECONCILIATION"
	// BLOCKED means the checkpoint itself says work cannot continue.
	BLOCKED State = "BLOCKED"
	// STALE means the repository no longer contains the recorded state.
	STALE State = "STALE"
	// INVALID means the bundle does not satisfy the handoff contract.
	INVALID State = "INVALID"
)

// severity orders verdicts so the worst finding wins. A single failing rule
// must not be averaged away by passing ones.
func (s State) severity() int {
	switch s {
	case SAFE:
		return 0
	case NEEDS_RECONCILIATION:
		return 1
	case STALE:
		return 2
	case BLOCKED:
		return 3
	case INVALID:
		return 4
	default:
		return 4
	}
}

// worseOf returns the more severe of two states.
func worseOf(a, b State) State {
	if b.severity() > a.severity() {
		return b
	}
	return a
}

// CheckStatus is the outcome of one resume rule.
type CheckStatus string

const (
	// CheckPass means the rule was established.
	CheckPass CheckStatus = "pass"
	// CheckFail means the rule was established and violated.
	CheckFail CheckStatus = "fail"
	// CheckUnknown means the rule could not be established either way.
	CheckUnknown CheckStatus = "unknown"
	// CheckAdvisory means the rule is informational and never blocks.
	CheckAdvisory CheckStatus = "advisory"
)

// Check is one resume rule and its outcome.
type Check struct {
	ID      string      `json:"id"`
	Rule    string      `json:"rule"`
	Status  CheckStatus `json:"status"`
	Detail  string      `json:"detail,omitempty"`
	Verdict State       `json:"contributes,omitempty"`
}

// Report is the full resume assessment.
type Report struct {
	Path        string               `json:"path"`
	TaskID      string               `json:"task_id,omitempty"`
	Role        string               `json:"role,omitempty"`
	Runtime     string               `json:"runtime,omitempty"`
	Model       string               `json:"model,omitempty"`
	Commit      string               `json:"last_verified_commit,omitempty"`
	RepoDir     string               `json:"repository_dir,omitempty"`
	State       State                `json:"resume_state"`
	Checks      []Check              `json:"checks"`
	Summary     string               `json:"summary"`
	EvaluatedAt time.Time            `json:"evaluated_at"`
	Bundle      []handoff.Diagnostic `json:"bundle_diagnostics,omitempty"`
}

// Safe reports whether the verdict permits continuing without reconciliation.
func (r *Report) Safe() bool { return r != nil && r.State == SAFE }

// Options configures a resume check.
type Options struct {
	// RepoDir is the repository to inspect. When empty, repository rules
	// cannot be established.
	RepoDir string
	// Inspector reads repository state. When nil, repository rules are
	// reported unknown rather than failing.
	Inspector Inspector
	// SkipGit records that the caller asked for no repository inspection.
	SkipGit bool
	// Now is the evaluation time.
	Now time.Time
}

// Evaluate runs every resume rule against a bundle and returns the verdict.
func Evaluate(ctx context.Context, bundle *handoff.Bundle, opts Options) *Report {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	report := &Report{
		Path:        bundle.Path,
		State:       SAFE,
		Checks:      []Check{},
		EvaluatedAt: now.UTC(),
		Bundle:      bundle.Errors(),
		RepoDir:     opts.RepoDir,
	}

	record := func(id, rule string, status CheckStatus, verdict State, format string, args ...interface{}) {
		check := Check{ID: id, Rule: rule, Status: status, Detail: fmt.Sprintf(format, args...)}
		if status != CheckPass && status != CheckAdvisory {
			check.Verdict = verdict
			report.State = worseOf(report.State, verdict)
		}
		report.Checks = append(report.Checks, check)
	}

	// Rule 1: the bundle must satisfy the handoff contract. Nothing else is
	// worth evaluating against a bundle we cannot trust to mean what it says.
	if !bundle.Valid() {
		record("bundle_valid", "handoff bundle satisfies the contract", CheckFail, INVALID,
			"%d error(s) in the bundle; run `howlforge handoff validate` for detail", len(bundle.Errors()))
		report.Summary = "the handoff bundle is invalid and cannot be resumed from"
		return report
	}
	record("bundle_valid", "handoff bundle satisfies the contract", CheckPass, SAFE, "all required artifacts present and well formed")

	checkpoint := bundle.Checkpoint
	if checkpoint == nil {
		record("checkpoint_present", "checkpoint.json is readable", CheckFail, INVALID, "no checkpoint was loaded")
		report.Summary = "no checkpoint to resume from"
		return report
	}
	report.TaskID = checkpoint.TaskID
	report.Role = checkpoint.EffectiveRole()
	report.Runtime = checkpoint.EffectiveClient()
	report.Model = checkpoint.EffectiveModel()
	report.Commit = checkpoint.EffectiveCommit()

	// Rule 2: the worker's own verdict. A checkpoint that says it is not safe
	// is believed; HowlForge never overrides a departing worker's caution.
	switch checkpoint.Status {
	case handoff.StatusBlocked:
		record("status", "checkpoint status permits resumption", CheckFail, BLOCKED,
			"checkpoint status is blocked")
	case handoff.StatusAbandoned:
		record("status", "checkpoint status permits resumption", CheckFail, BLOCKED,
			"checkpoint status is abandoned; this attempt must not be built on")
	case handoff.StatusComplete:
		record("status", "checkpoint status permits resumption", CheckAdvisory, SAFE,
			"checkpoint status is complete; there may be nothing to resume")
	default:
		record("status", "checkpoint status permits resumption", CheckPass, SAFE,
			"checkpoint status is %s", checkpoint.Status)
	}
	if !checkpoint.SafeToResume {
		record("safe_to_resume", "departing worker marked the state safe to resume", CheckFail, NEEDS_RECONCILIATION,
			"checkpoint sets safe_to_resume to false")
	} else {
		record("safe_to_resume", "departing worker marked the state safe to resume", CheckPass, SAFE,
			"checkpoint sets safe_to_resume to true")
	}

	// Rule 3: remaining work must be explicit.
	if checkpoint.Status == handoff.StatusPartial && len(checkpoint.Remaining) == 0 {
		record("remaining_explicit", "remaining work is explicit", CheckFail, NEEDS_RECONCILIATION,
			"a partial checkpoint lists no remaining work")
	} else {
		record("remaining_explicit", "remaining work is explicit", CheckPass, SAFE,
			"%d remaining item(s) recorded", len(checkpoint.Remaining))
	}

	// Rule 4: unresolved questions must not be left implicit. Their presence
	// is not a failure of the departing worker, it is a reason the next one
	// must reconcile before continuing.
	if len(checkpoint.UnresolvedQuestions) > 0 {
		record("unresolved_questions", "no unresolved questions block continuation", CheckFail, NEEDS_RECONCILIATION,
			"%d unresolved question(s) recorded", len(checkpoint.UnresolvedQuestions))
	} else {
		record("unresolved_questions", "no unresolved questions block continuation", CheckPass, SAFE,
			"none recorded")
	}

	// Rule 5: irreversible decisions are advisory. They do not block, but a
	// replacement that reverses one without noticing causes real damage.
	irreversible := 0
	for _, decision := range bundle.Decisions {
		if !decision.Reversible {
			irreversible++
		}
	}
	if irreversible > 0 {
		record("decisions", "prior decisions are recorded", CheckAdvisory, SAFE,
			"%d decision(s) recorded, %d marked irreversible", len(bundle.Decisions), irreversible)
	} else {
		record("decisions", "prior decisions are recorded", CheckPass, SAFE,
			"%d decision(s) recorded", len(bundle.Decisions))
	}

	// Rule 6: verification evidence. HowlForge never re-runs tests; HowlProof
	// owns that. What HowlForge asserts is that evidence exists and that no
	// recorded check failed.
	evaluateVerification(bundle, record)

	// Rule 7 and 8: repository state.
	evaluateRepository(ctx, checkpoint, opts, record)

	report.Summary = summarize(report.State, checkpoint)
	return report
}

type recorder func(id, rule string, status CheckStatus, verdict State, format string, args ...interface{})

func evaluateVerification(bundle *handoff.Bundle, record recorder) {
	verification := bundle.Verification
	if verification == nil || len(verification.Checks) == 0 {
		record("verification_evidence", "verification evidence exists", CheckUnknown, NEEDS_RECONCILIATION,
			"no verification checks are recorded; the replacement cannot tell what was proven")
		return
	}
	failed := 0
	for _, check := range verification.Checks {
		if strings.EqualFold(strings.TrimSpace(check.Status), "failed") {
			failed++
		}
	}
	if failed > 0 {
		record("verification_evidence", "verification evidence exists", CheckFail, NEEDS_RECONCILIATION,
			"%d of %d recorded check(s) failed", failed, len(verification.Checks))
		return
	}
	record("verification_evidence", "verification evidence exists", CheckPass, SAFE,
		"%d recorded check(s), none failed", len(verification.Checks))
	// Reproduction is explicitly out of scope and is said so rather than
	// implied, because "tests passed" in a file is a claim, not an observation.
	record("verification_reproduced", "recorded checks were reproduced", CheckAdvisory, SAFE,
		"not attempted: HowlProof owns verification execution, HowlForge only reads the evidence")
	if !verification.Independent {
		record("verification_independence", "verification was independent", CheckAdvisory, SAFE,
			"recorded verification was not independent of the producing worker")
	} else {
		record("verification_independence", "verification was independent", CheckPass, SAFE,
			"verified by role %q on runtime %q", verification.VerifierRole, verification.VerifierRuntime)
	}
}

func evaluateRepository(ctx context.Context, checkpoint *handoff.Checkpoint, opts Options, record recorder) {
	commit := checkpoint.EffectiveCommit()

	if opts.SkipGit {
		record("commit_exists", "recorded commit exists in the repository", CheckUnknown, NEEDS_RECONCILIATION,
			"repository inspection was skipped by request")
		record("worktree_understood", "working tree state matches the checkpoint", CheckUnknown, NEEDS_RECONCILIATION,
			"repository inspection was skipped by request")
		return
	}
	if opts.Inspector == nil || strings.TrimSpace(opts.RepoDir) == "" {
		reason := "no repository directory was given"
		if opts.Inspector == nil {
			reason = "git is not available"
		}
		record("commit_exists", "recorded commit exists in the repository", CheckUnknown, NEEDS_RECONCILIATION, "%s", reason)
		record("worktree_understood", "working tree state matches the checkpoint", CheckUnknown, NEEDS_RECONCILIATION, "%s", reason)
		return
	}

	exists, err := opts.Inspector.CommitExists(ctx, opts.RepoDir, commit)
	switch {
	case err != nil:
		record("commit_exists", "recorded commit exists in the repository", CheckUnknown, NEEDS_RECONCILIATION,
			"could not check: %v", err)
	case !exists:
		// The repository no longer contains what the checkpoint describes.
		// That is the definition of stale, and it is not reconcilable by
		// reading files.
		record("commit_exists", "recorded commit exists in the repository", CheckFail, STALE,
			"commit %s is not present in %s", shortCommit(commit), opts.RepoDir)
		return
	default:
		record("commit_exists", "recorded commit exists in the repository", CheckPass, SAFE,
			"commit %s is present", shortCommit(commit))
	}

	head, err := opts.Inspector.HeadCommit(ctx, opts.RepoDir)
	if err != nil {
		record("head_related", "HEAD is related to the recorded commit", CheckUnknown, NEEDS_RECONCILIATION,
			"could not read HEAD: %v", err)
	} else if head == commit || strings.HasPrefix(head, commit) {
		record("head_related", "HEAD is related to the recorded commit", CheckPass, SAFE,
			"HEAD is at the recorded commit")
	} else {
		ancestor, ancestorErr := opts.Inspector.IsAncestor(ctx, opts.RepoDir, commit, head)
		switch {
		case ancestorErr != nil:
			record("head_related", "HEAD is related to the recorded commit", CheckUnknown, NEEDS_RECONCILIATION,
				"could not compare HEAD to the recorded commit: %v", ancestorErr)
		case ancestor:
			// Moving forward is normal and safe; the recorded work is still in
			// the history the replacement will build on.
			record("head_related", "HEAD is related to the recorded commit", CheckPass, SAFE,
				"HEAD %s is a descendant of the recorded commit", shortCommit(head))
		default:
			record("head_related", "HEAD is related to the recorded commit", CheckFail, NEEDS_RECONCILIATION,
				"HEAD %s has diverged from the recorded commit %s", shortCommit(head), shortCommit(commit))
		}
	}

	dirty, err := opts.Inspector.IsDirty(ctx, opts.RepoDir)
	switch {
	case err != nil:
		record("worktree_understood", "working tree state matches the checkpoint", CheckUnknown, NEEDS_RECONCILIATION,
			"could not read working tree status: %v", err)
	case dirty && !checkpoint.EffectiveDirty():
		// Uncommitted changes nobody recorded are the classic silent-conflict
		// case: the replacement would build on top of work it cannot attribute.
		record("worktree_understood", "working tree state matches the checkpoint", CheckFail, NEEDS_RECONCILIATION,
			"the working tree has uncommitted changes the checkpoint does not record")
	case !dirty && checkpoint.EffectiveDirty():
		record("worktree_understood", "working tree state matches the checkpoint", CheckFail, NEEDS_RECONCILIATION,
			"the checkpoint records uncommitted changes but the working tree is clean")
	case dirty:
		record("worktree_understood", "working tree state matches the checkpoint", CheckPass, SAFE,
			"the working tree is dirty as the checkpoint records")
	default:
		record("worktree_understood", "working tree state matches the checkpoint", CheckPass, SAFE,
			"the working tree is clean as the checkpoint records")
	}
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

func summarize(state State, checkpoint *handoff.Checkpoint) string {
	switch state {
	case SAFE:
		return fmt.Sprintf("task %s may be resumed by a replacement worker in role %s",
			checkpoint.TaskID, checkpoint.EffectiveRole())
	case NEEDS_RECONCILIATION:
		return "state could not be fully established; reconcile before continuing"
	case STALE:
		return "the repository no longer contains the state this checkpoint describes"
	case BLOCKED:
		return "the checkpoint reports that work cannot continue"
	case INVALID:
		return "the handoff bundle is invalid and cannot be resumed from"
	default:
		return string(state)
	}
}

// NewInspector returns a git-backed inspector, or nil with no error when git
// is unavailable. Callers treat a nil inspector as "cannot establish", which
// keeps HowlForge usable on a machine without git.
func NewInspector() (Inspector, error) {
	inspector, err := NewGitInspector()
	if err != nil {
		if errors.Is(err, ErrGitUnavailable) {
			return nil, nil
		}
		return nil, err
	}
	return inspector, nil
}
