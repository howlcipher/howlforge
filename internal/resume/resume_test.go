package resume

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/howlcipher/howlforge/internal/handoff"
)

const safeCommit = "abc123def456"

// fakeInspector is a scripted repository. Using it rather than a real git
// repository is what lets every resume rule be exercised, including the ones
// that only fire when git itself fails.
type fakeInspector struct {
	exists     bool
	existsErr  error
	head       string
	headErr    error
	isAncestor bool
	ancestErr  error
	dirty      bool
	dirtyErr   error
}

func (f *fakeInspector) CommitExists(context.Context, string, string) (bool, error) {
	return f.exists, f.existsErr
}
func (f *fakeInspector) HeadCommit(context.Context, string) (string, error) {
	return f.head, f.headErr
}
func (f *fakeInspector) IsAncestor(context.Context, string, string, string) (bool, error) {
	return f.isAncestor, f.ancestErr
}
func (f *fakeInspector) IsDirty(context.Context, string) (bool, error) {
	return f.dirty, f.dirtyErr
}

func healthyInspector() *fakeInspector {
	return &fakeInspector{exists: true, head: safeCommit, isAncestor: true, dirty: false}
}

func bundleFiles() map[string]string {
	return map[string]string{
		handoff.FileCheckpoint: `{
  "schema_version": "howlforge.checkpoint/v1",
  "task_id": "HOWL-0042",
  "role": "implementer",
  "runtime": "cursor",
  "model": "composer",
  "status": "partial",
  "safe_to_resume": true,
  "handoff_reason": "SESSION_LIMIT",
  "last_verified_commit": "` + safeCommit + `",
  "completed": ["registry"],
  "remaining": ["integration tests"]
}`,
		handoff.FileSummary:       "Built the registry.\n",
		handoff.FileRemainingWork: "- integration tests\n",
		handoff.FileChangedFiles:  "internal/runtimes/runtime.go\n",
		handoff.FileDecisions:     `{"schema_version":"howlforge.decisions/v1","decisions":[]}`,
		handoff.FileVerification:  `{"schema_version":"howlforge.verification/v1","independent":false,"checks":[{"name":"go test ./...","status":"passed"}]}`,
	}
}

func loadBundle(t *testing.T, overrides map[string]string) *handoff.Bundle {
	t.Helper()
	dir := t.TempDir()
	files := bundleFiles()
	for name, content := range overrides {
		files[name] = content
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	bundle, err := handoff.Load(dir)
	if err != nil {
		t.Fatalf("handoff.Load: %v", err)
	}
	return bundle
}

func evaluate(t *testing.T, bundle *handoff.Bundle, opts Options) *Report {
	t.Helper()
	if opts.RepoDir == "" {
		opts.RepoDir = t.TempDir()
	}
	if opts.Now.IsZero() {
		opts.Now = time.Date(2026, 9, 22, 15, 0, 0, 0, time.UTC)
	}
	return Evaluate(context.Background(), bundle, opts)
}

func checkByID(report *Report, id string) (Check, bool) {
	for _, check := range report.Checks {
		if check.ID == id {
			return check, true
		}
	}
	return Check{}, false
}

func TestSafeResume(t *testing.T) {
	report := evaluate(t, loadBundle(t, nil), Options{Inspector: healthyInspector()})
	if report.State != SAFE {
		t.Fatalf("state = %s, want SAFE\nchecks: %+v", report.State, report.Checks)
	}
	if !report.Safe() {
		t.Fatal("Safe() disagrees with the state")
	}
	if report.TaskID != "HOWL-0042" || report.Role != "implementer" {
		t.Fatalf("identity was not carried through: %+v", report)
	}
	if !strings.Contains(report.Summary, "HOWL-0042") {
		t.Fatalf("summary = %q", report.Summary)
	}
}

func TestVerdictPrecedence(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
		inspector Inspector
		skipGit   bool
		want      State
	}{
		{
			name:      "healthy bundle is safe",
			inspector: healthyInspector(),
			want:      SAFE,
		},
		{
			name: "an invalid bundle outranks everything",
			overrides: map[string]string{
				handoff.FileCheckpoint: `{"schema_version":"howlforge.checkpoint/v9"}`,
			},
			inspector: healthyInspector(),
			want:      INVALID,
		},
		{
			name: "a blocked checkpoint is blocked",
			overrides: map[string]string{
				handoff.FileCheckpoint: `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"blocked","safe_to_resume":false,"last_verified_commit":"` + safeCommit + `","completed":[],"remaining":["x"]}`,
			},
			inspector: healthyInspector(),
			want:      BLOCKED,
		},
		{
			name: "an abandoned checkpoint is blocked",
			overrides: map[string]string{
				handoff.FileCheckpoint: `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"abandoned","safe_to_resume":false,"last_verified_commit":"` + safeCommit + `","completed":[],"remaining":[]}`,
			},
			inspector: healthyInspector(),
			want:      BLOCKED,
		},
		{
			name:      "a missing commit is stale",
			inspector: &fakeInspector{exists: false},
			want:      STALE,
		},
		{
			name:      "diverged history needs reconciliation",
			inspector: &fakeInspector{exists: true, head: "fedcba987654", isAncestor: false},
			want:      NEEDS_RECONCILIATION,
		},
		{
			name:      "unrecorded uncommitted changes need reconciliation",
			inspector: &fakeInspector{exists: true, head: safeCommit, isAncestor: true, dirty: true},
			want:      NEEDS_RECONCILIATION,
		},
		{
			name:      "git failure never becomes confidence",
			inspector: &fakeInspector{existsErr: errors.New("git exploded")},
			want:      NEEDS_RECONCILIATION,
		},
		{
			name:    "skipping git inspection needs reconciliation",
			skipGit: true,
			want:    NEEDS_RECONCILIATION,
		},
		{
			name:      "no inspector needs reconciliation",
			inspector: nil,
			want:      NEEDS_RECONCILIATION,
		},
		{
			name: "safe_to_resume false is believed",
			overrides: map[string]string{
				handoff.FileCheckpoint: `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"partial","safe_to_resume":false,"last_verified_commit":"` + safeCommit + `","completed":[],"remaining":["x"]}`,
			},
			inspector: healthyInspector(),
			want:      NEEDS_RECONCILIATION,
		},
		{
			name: "unresolved questions need reconciliation",
			overrides: map[string]string{
				handoff.FileCheckpoint: `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"partial","safe_to_resume":true,"last_verified_commit":"` + safeCommit + `","completed":[],"remaining":["x"],"unresolved_questions":["which cache do we keep?"]}`,
			},
			inspector: healthyInspector(),
			want:      NEEDS_RECONCILIATION,
		},
		{
			name: "a failed recorded check needs reconciliation",
			overrides: map[string]string{
				handoff.FileVerification: `{"schema_version":"howlforge.verification/v1","independent":false,"checks":[{"name":"go test","status":"failed"}]}`,
			},
			inspector: healthyInspector(),
			want:      NEEDS_RECONCILIATION,
		},
		{
			name: "no verification evidence needs reconciliation",
			overrides: map[string]string{
				handoff.FileVerification: `{"schema_version":"howlforge.verification/v1","independent":false,"checks":[]}`,
			},
			inspector: healthyInspector(),
			want:      NEEDS_RECONCILIATION,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := loadBundle(t, tt.overrides)
			report := evaluate(t, bundle, Options{Inspector: tt.inspector, SkipGit: tt.skipGit})
			if report.State != tt.want {
				t.Fatalf("state = %s, want %s\nchecks: %+v", report.State, tt.want, report.Checks)
			}
		})
	}
}

func TestDirtyTreeRecordedInCheckpointIsAccepted(t *testing.T) {
	bundle := loadBundle(t, map[string]string{
		handoff.FileCheckpoint: `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"partial","safe_to_resume":true,"last_verified_commit":"` + safeCommit + `","completed":[],"remaining":["x"],"repository":{"last_verified_commit":"` + safeCommit + `","dirty":true}}`,
	})
	report := evaluate(t, bundle, Options{
		Inspector: &fakeInspector{exists: true, head: safeCommit, isAncestor: true, dirty: true},
	})
	if report.State != SAFE {
		t.Fatalf("state = %s, want SAFE when the checkpoint records the dirty tree\nchecks: %+v", report.State, report.Checks)
	}
}

func TestCheckpointClaimsDirtyButTreeIsClean(t *testing.T) {
	bundle := loadBundle(t, map[string]string{
		handoff.FileCheckpoint: `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"partial","safe_to_resume":true,"last_verified_commit":"` + safeCommit + `","completed":[],"remaining":["x"],"repository":{"last_verified_commit":"` + safeCommit + `","dirty":true}}`,
	})
	report := evaluate(t, bundle, Options{Inspector: healthyInspector()})
	if report.State != NEEDS_RECONCILIATION {
		t.Fatalf("state = %s, want NEEDS_RECONCILIATION", report.State)
	}
}

func TestHeadAheadOfCheckpointIsSafe(t *testing.T) {
	// Moving forward is normal: the recorded work is still in the history the
	// replacement will build on.
	report := evaluate(t, loadBundle(t, nil), Options{
		Inspector: &fakeInspector{exists: true, head: "fedcba987654", isAncestor: true},
	})
	if report.State != SAFE {
		t.Fatalf("state = %s, want SAFE\nchecks: %+v", report.State, report.Checks)
	}
	check, ok := checkByID(report, "head_related")
	if !ok || check.Status != CheckPass {
		t.Fatalf("head_related = %+v, want a pass", check)
	}
}

func TestVerificationIsNeverReproduced(t *testing.T) {
	report := evaluate(t, loadBundle(t, nil), Options{Inspector: healthyInspector()})
	check, ok := checkByID(report, "verification_reproduced")
	if !ok {
		t.Fatal("the reproduction rule was not reported at all")
	}
	if check.Status != CheckAdvisory {
		t.Fatalf("reproduction status = %s, want advisory", check.Status)
	}
	// HowlForge must be explicit that it read a claim rather than observing a
	// result, and must say who does own execution.
	if !strings.Contains(check.Detail, "HowlProof") {
		t.Fatalf("reproduction detail = %q, want it to name the owner of verification execution", check.Detail)
	}
}

func TestIrreversibleDecisionsAreAdvisoryNotBlocking(t *testing.T) {
	bundle := loadBundle(t, map[string]string{
		handoff.FileDecisions: `{"schema_version":"howlforge.decisions/v1","decisions":[{"id":"D1","decision":"dropped the v1 column","reversible":false}]}`,
	})
	report := evaluate(t, bundle, Options{Inspector: healthyInspector()})
	if report.State != SAFE {
		t.Fatalf("an irreversible decision blocked resumption: %s", report.State)
	}
	check, ok := checkByID(report, "decisions")
	if !ok || check.Status != CheckAdvisory {
		t.Fatalf("decisions check = %+v, want advisory", check)
	}
	if !strings.Contains(check.Detail, "irreversible") {
		t.Fatalf("decisions detail = %q, want it to surface the irreversible decision", check.Detail)
	}
}

func TestStateSeverityOrdering(t *testing.T) {
	ascending := []State{SAFE, NEEDS_RECONCILIATION, STALE, BLOCKED, INVALID}
	for i := 1; i < len(ascending); i++ {
		if ascending[i-1].severity() >= ascending[i].severity() {
			t.Fatalf("%s is not ranked below %s", ascending[i-1], ascending[i])
		}
	}
	if worseOf(SAFE, STALE) != STALE {
		t.Fatal("worseOf did not take the more severe verdict")
	}
	if worseOf(INVALID, NEEDS_RECONCILIATION) != INVALID {
		t.Fatal("worseOf downgraded a severe verdict")
	}
}

func TestCommitishGuardRefusesArguments(t *testing.T) {
	// The injection guard must reject before any process could be started.
	hostile := []string{"--upload-pack=evil", "HEAD", "abc123; rm -rf /", "$(id)", ""}
	for _, commit := range hostile {
		if err := checkCommitish(commit); err == nil {
			t.Errorf("checkCommitish(%q) accepted a value that must never reach git", commit)
		}
	}
	if err := checkCommitish(safeCommit); err != nil {
		t.Errorf("checkCommitish(%q) = %v, want nil", safeCommit, err)
	}
}

func TestSafeRepoDir(t *testing.T) {
	dir := t.TempDir()
	resolved, err := safeRepoDir(dir)
	if err != nil {
		t.Fatalf("safeRepoDir: %v", err)
	}
	if !filepath.IsAbs(resolved) {
		t.Fatalf("safeRepoDir returned %q, want an absolute path", resolved)
	}

	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := safeRepoDir(file); err == nil {
		t.Fatal("safeRepoDir accepted a regular file")
	}
	if _, err := safeRepoDir(""); err == nil {
		t.Fatal("safeRepoDir accepted an empty path")
	}
	if _, err := safeRepoDir(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("safeRepoDir accepted a path that does not exist")
	}
}

func TestGitInspectorAgainstARealRepository(t *testing.T) {
	inspector, err := NewGitInspector()
	if err != nil {
		t.Skipf("git is unavailable: %v", err)
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if _, err := inspector.run(context.Background(), dir, args...); err != nil {
			t.Fatalf("git %s: %v", strings.Join(args, " "), err)
		}
	}
	run("init", "--quiet")
	run("config", "user.email", "test@example.invalid")
	run("config", "user.name", "Test")
	run("config", "commit.gpgsign", "false")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("first\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("add", "a.txt")
	run("commit", "--quiet", "-m", "first")

	ctx := context.Background()
	head, err := inspector.HeadCommit(ctx, dir)
	if err != nil {
		t.Fatalf("HeadCommit: %v", err)
	}
	if !handoff.IsCommitish(head) {
		t.Fatalf("HeadCommit returned %q, which is not a commit hash", head)
	}

	exists, err := inspector.CommitExists(ctx, dir, head)
	if err != nil || !exists {
		t.Fatalf("CommitExists(head) = %v, %v", exists, err)
	}
	absent, err := inspector.CommitExists(ctx, dir, strings.Repeat("0", 40))
	if err != nil {
		t.Fatalf("CommitExists on an absent commit returned an error: %v", err)
	}
	if absent {
		t.Fatal("CommitExists reported a commit that is not in the repository")
	}

	dirty, err := inspector.IsDirty(ctx, dir)
	if err != nil || dirty {
		t.Fatalf("IsDirty on a clean tree = %v, %v", dirty, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	dirty, err = inspector.IsDirty(ctx, dir)
	if err != nil || !dirty {
		t.Fatalf("IsDirty on a modified tree = %v, %v", dirty, err)
	}

	if ancestor, err := inspector.IsAncestor(ctx, dir, head, head); err != nil || !ancestor {
		t.Fatalf("IsAncestor(head, head) = %v, %v", ancestor, err)
	}

	// The guard must hold on the real implementation too, not just in theory.
	if _, err := inspector.CommitExists(ctx, dir, "--upload-pack=evil"); err == nil {
		t.Fatal("the real inspector passed an option-shaped argument to git")
	}
}
