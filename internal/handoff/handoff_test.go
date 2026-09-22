package handoff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bundleFiles is a complete, valid handoff bundle used as the starting point
// for every test that mutates one thing.
func bundleFiles() map[string]string {
	return map[string]string{
		FileCheckpoint: `{
  "schema_version": "howlforge.checkpoint/v1",
  "task_id": "HOWL-0042",
  "role": "implementer",
  "runtime": "cursor",
  "model": "composer",
  "status": "partial",
  "safe_to_resume": true,
  "handoff_reason": "SESSION_LIMIT",
  "last_verified_commit": "abc123def456",
  "completed": ["implemented runtime registry", "added unit tests"],
  "remaining": ["integration tests", "documentation"]
}`,
		FileSummary:       "# Summary\n\nBuilt the runtime registry.\n",
		FileRemainingWork: "- integration tests\n- documentation\n",
		FileChangedFiles:  "internal/runtimes/runtime.go\ninternal/runtimes/runtime_test.go\n",
		FileDecisions: `{
  "schema_version": "howlforge.decisions/v1",
  "decisions": [
    {"id": "D1", "decision": "Runtime files stay declarative", "rationale": "no execution from config", "reversible": false}
  ]
}`,
		FileVerification: `{
  "schema_version": "howlforge.verification/v1",
  "independent": false,
  "checks": [
    {"name": "go test ./internal/runtimes/", "status": "passed", "result": "ok"}
  ]
}`,
	}
}

// writeBundle materializes a bundle, applying overrides. A nil override value
// deletes the file.
func writeBundle(t *testing.T, overrides map[string]*string) string {
	t.Helper()
	dir := t.TempDir()
	files := bundleFiles()
	for name, value := range overrides {
		if value == nil {
			delete(files, name)
			continue
		}
		files[name] = *value
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func ptr(s string) *string { return &s }

func TestLoadValidBundle(t *testing.T) {
	bundle, err := Load(writeBundle(t, nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bundle.Valid() {
		t.Fatalf("a complete bundle was rejected: %v", bundle.Diagnostics)
	}
	if bundle.Checkpoint.TaskID != "HOWL-0042" {
		t.Fatalf("task id = %q", bundle.Checkpoint.TaskID)
	}
	if bundle.Checkpoint.Reason != ReasonSessionLimit {
		t.Fatalf("handoff reason = %q", bundle.Checkpoint.Reason)
	}
	if len(bundle.ChangedFiles) != 2 {
		t.Fatalf("changed files = %v", bundle.ChangedFiles)
	}
	if len(bundle.Decisions) != 1 || bundle.Decisions[0].Reversible {
		t.Fatalf("decisions = %v", bundle.Decisions)
	}
	if bundle.Verification == nil || len(bundle.Verification.Checks) != 1 {
		t.Fatal("verification was not loaded")
	}
	if len(bundle.Missing) != 0 {
		t.Fatalf("missing files = %v", bundle.Missing)
	}
}

func TestEveryRequiredFileIsRequired(t *testing.T) {
	for _, name := range RequiredFiles {
		t.Run(name, func(t *testing.T) {
			bundle, err := Load(writeBundle(t, map[string]*string{name: nil}))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if bundle.Valid() {
				t.Fatalf("a bundle missing %s was accepted", name)
			}
			found := false
			for _, diagnostic := range bundle.Errors() {
				if diagnostic.File == name && strings.Contains(diagnostic.Message, "missing") {
					found = true
				}
			}
			if !found {
				t.Fatalf("no diagnostic names the missing %s: %v", name, bundle.Diagnostics)
			}
		})
	}
}

func TestMalformedCheckpoint(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantIn  string
	}{
		{"not json", `this is not json`, ""},
		{"wrong schema version", `{"schema_version":"howlforge.checkpoint/v2","task_id":"T","role":"r","runtime":"c","model":"m","status":"partial","safe_to_resume":false,"last_verified_commit":"abc123def456","completed":[],"remaining":["x"]}`, "schema_version"},
		{"unknown field", `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"partial","safe_to_resume":false,"last_verified_commit":"abc123def456","completed":[],"remaining":["x"],"mystery":1}`, "unknown field"},
		{"empty task id", `{"schema_version":"howlforge.checkpoint/v1","task_id":"","role":"r","runtime":"c","model":"m","status":"partial","safe_to_resume":false,"last_verified_commit":"abc123def456","completed":[],"remaining":["x"]}`, "task_id"},
		{"unknown status", `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"vibing","safe_to_resume":false,"last_verified_commit":"abc123def456","completed":[],"remaining":["x"]}`, "status"},
		{"unknown reason", `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"partial","handoff_reason":"BORED","safe_to_resume":false,"last_verified_commit":"abc123def456","completed":[],"remaining":["x"]}`, "handoff_reason"},
		{"missing commit", `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"partial","safe_to_resume":false,"last_verified_commit":"","completed":[],"remaining":["x"]}`, "last_verified_commit"},
		{"non hex commit", `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"partial","safe_to_resume":false,"last_verified_commit":"HEAD~1; rm -rf /","completed":[],"remaining":["x"]}`, "not a hexadecimal"},
		{"partial with no remaining work", `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"partial","safe_to_resume":false,"last_verified_commit":"abc123def456","completed":[],"remaining":[]}`, "remaining work"},
		{"abandoned but safe to resume", `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"abandoned","safe_to_resume":true,"last_verified_commit":"abc123def456","completed":[],"remaining":[]}`, "must not be marked safe to resume"},
		{"absolute changed file", `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"partial","safe_to_resume":false,"last_verified_commit":"abc123def456","completed":[],"remaining":["x"],"changed_files":["/etc/passwd"]}`, "repository relative"},
		{"traversing changed file", `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"partial","safe_to_resume":false,"last_verified_commit":"abc123def456","completed":[],"remaining":["x"],"changed_files":["../../etc/passwd"]}`, "escapes the repository root"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle, err := Load(writeBundle(t, map[string]*string{FileCheckpoint: ptr(tt.content)}))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if bundle.Valid() {
				t.Fatalf("an invalid checkpoint was accepted: %s", tt.name)
			}
			if tt.wantIn == "" {
				return
			}
			joined := diagnosticText(bundle)
			if !strings.Contains(joined, tt.wantIn) {
				t.Fatalf("diagnostics %q do not mention %q", joined, tt.wantIn)
			}
		})
	}
}

func diagnosticText(bundle *Bundle) string {
	parts := make([]string, 0, len(bundle.Diagnostics))
	for _, diagnostic := range bundle.Diagnostics {
		parts = append(parts, diagnostic.String())
	}
	return strings.Join(parts, "\n")
}

func TestChangedFilesPathsAreValidated(t *testing.T) {
	bundle, err := Load(writeBundle(t, map[string]*string{
		FileChangedFiles: ptr("ok/path.go\n/etc/shadow\n../escape.go\n# a comment\n\n"),
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if bundle.Valid() {
		t.Fatal("a changed file list containing an escape was accepted")
	}
	// The legitimate entry still loads; comments and blanks are skipped.
	if len(bundle.ChangedFiles) != 1 || bundle.ChangedFiles[0] != "ok/path.go" {
		t.Fatalf("changed files = %v, want only the legitimate entry", bundle.ChangedFiles)
	}
}

func TestEmptyChangedFilesIsAllowed(t *testing.T) {
	// A worker that changed nothing yet is a legitimate handoff.
	bundle, err := Load(writeBundle(t, map[string]*string{
		FileChangedFiles: ptr(""),
		FileCheckpoint:   ptr(`{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"partial","safe_to_resume":true,"last_verified_commit":"abc123def456","completed":[],"remaining":["everything"]}`),
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bundle.Valid() {
		t.Fatalf("an empty changed file list was rejected: %v", bundle.Diagnostics)
	}
}

func TestCrossCheckRefusesContradictions(t *testing.T) {
	t.Run("changed files disagree", func(t *testing.T) {
		checkpoint := `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"partial","safe_to_resume":true,"last_verified_commit":"abc123def456","completed":[],"remaining":["x"],"changed_files":["a.go"]}`
		bundle, err := Load(writeBundle(t, map[string]*string{
			FileCheckpoint:   ptr(checkpoint),
			FileChangedFiles: ptr("b.go\n"),
		}))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if bundle.Valid() {
			t.Fatal("contradictory changed file records were accepted")
		}
		if !strings.Contains(diagnosticText(bundle), "disagrees with checkpoint.json") {
			t.Fatalf("diagnostics %q do not explain the contradiction", diagnosticText(bundle))
		}
	})

	t.Run("matching changed files are accepted", func(t *testing.T) {
		checkpoint := `{"schema_version":"howlforge.checkpoint/v1","task_id":"T","role":"r","runtime":"c","model":"m","status":"partial","safe_to_resume":true,"last_verified_commit":"abc123def456","completed":[],"remaining":["x"],"changed_files":["b.go","a.go"]}`
		bundle, err := Load(writeBundle(t, map[string]*string{
			FileCheckpoint:   ptr(checkpoint),
			FileChangedFiles: ptr("a.go\nb.go\n"),
		}))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !bundle.Valid() {
			t.Fatalf("matching records in a different order were rejected: %v", bundle.Diagnostics)
		}
	})
}

func TestVerificationValidation(t *testing.T) {
	tests := []struct {
		name    string
		content string
		valid   bool
		wantIn  string
	}{
		{"valid", `{"schema_version":"howlforge.verification/v1","independent":true,"verifier_role":"reviewer","checks":[{"name":"go test","status":"passed"}]}`, true, ""},
		{"wrong schema", `{"schema_version":"howlforge.verification/v2","independent":false,"checks":[]}`, false, "schema_version"},
		{"bad status", `{"schema_version":"howlforge.verification/v1","independent":false,"checks":[{"name":"go test","status":"probably"}]}`, false, "must be passed, failed, skipped or errored"},
		{"unnamed check", `{"schema_version":"howlforge.verification/v1","independent":false,"checks":[{"name":"","status":"passed"}]}`, false, "must not be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle, err := Load(writeBundle(t, map[string]*string{FileVerification: ptr(tt.content)}))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if bundle.Valid() != tt.valid {
				t.Fatalf("valid = %v, want %v (%v)", bundle.Valid(), tt.valid, bundle.Diagnostics)
			}
			if tt.wantIn != "" && !strings.Contains(diagnosticText(bundle), tt.wantIn) {
				t.Fatalf("diagnostics %q do not mention %q", diagnosticText(bundle), tt.wantIn)
			}
		})
	}
}

func TestFailedCheckWithSafeToResumeWarns(t *testing.T) {
	bundle, err := Load(writeBundle(t, map[string]*string{
		FileVerification: ptr(`{"schema_version":"howlforge.verification/v1","independent":false,"checks":[{"name":"go test","status":"failed","result":"2 failures"}]}`),
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// A failing check does not invalidate the bundle, but it must be visible.
	if !bundle.Valid() {
		t.Fatalf("a failing check invalidated the bundle: %v", bundle.Diagnostics)
	}
	if !strings.Contains(diagnosticText(bundle), "failed while the checkpoint claims it is safe to resume") {
		t.Fatalf("no warning was raised: %v", bundle.Diagnostics)
	}
}

func TestDuplicateDecisionIDs(t *testing.T) {
	bundle, err := Load(writeBundle(t, map[string]*string{
		FileDecisions: ptr(`{"schema_version":"howlforge.decisions/v1","decisions":[{"id":"D1","decision":"a","reversible":true},{"id":"D1","decision":"b","reversible":true}]}`),
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if bundle.Valid() {
		t.Fatal("duplicate decision ids were accepted")
	}
}

func TestLoadRejectsMissingDirectory(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("Load accepted a directory that does not exist")
	}
}

func TestIsCommitish(t *testing.T) {
	valid := []string{"abc123d", "abc123def456", strings.Repeat("a", 40), strings.Repeat("0", 64)}
	for _, commit := range valid {
		if !IsCommitish(commit) {
			t.Errorf("IsCommitish(%q) = false, want true", commit)
		}
	}
	// These are exactly the shapes that must never reach a git argument list.
	invalid := []string{
		"", "abc", "HEAD", "HEAD~1", "main",
		"abc123def456; rm -rf /", "--upload-pack=evil", "$(whoami)",
		"`id`", "abc123 def456", strings.Repeat("a", 65), "ABC123DEF456",
	}
	for _, commit := range invalid {
		if IsCommitish(commit) {
			t.Errorf("IsCommitish(%q) = true, want false", commit)
		}
	}
}

func TestParseStatusAndReason(t *testing.T) {
	for _, status := range allStatuses {
		if _, err := ParseStatus(string(status)); err != nil {
			t.Errorf("ParseStatus(%q) = %v", status, err)
		}
	}
	if _, err := ParseStatus("done"); err == nil {
		t.Error("ParseStatus accepted an undefined status")
	}
	for _, reason := range allReasons {
		if _, err := ParseReason(string(reason)); err != nil {
			t.Errorf("ParseReason(%q) = %v", reason, err)
		}
	}
	if _, err := ParseReason("tired"); err == nil {
		t.Error("ParseReason accepted an undefined reason")
	}
}

func TestCheckPassedIsStrict(t *testing.T) {
	// Anything that is not an explicit pass counts as not passed. A malformed
	// or absent status must never read as success.
	if !(Check{Status: "passed"}).Passed() {
		t.Error("an explicit pass was not recognized")
	}
	for _, status := range []string{"", "PASS", "ok", "failed", "skipped", "probably passed"} {
		if (Check{Status: status}).Passed() {
			t.Errorf("status %q was read as passed", status)
		}
	}
	if !(Check{Status: "  PASSED  "}).Passed() {
		t.Error("case and whitespace should be normalized")
	}
}
