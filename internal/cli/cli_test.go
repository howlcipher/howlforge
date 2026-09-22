package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The CLI tests run against the shipped example configuration on purpose: if
// the defaults in config/ stop working, that is a defect an operator would hit
// on their first command.
const (
	shippedConfig = "../../config"
	shippedState  = "../../examples/state/availability.json"
	fixedNow      = "2026-09-22T15:00:00Z"
	afterReset    = "2026-09-22T19:00:00Z"
)

type result struct {
	code   int
	stdout string
	stderr string
}

// run invokes the CLI with the shipped configuration and a fixed clock, so
// output is reproducible.
func run(t *testing.T, args ...string) result {
	t.Helper()
	full := append([]string{"--config", shippedConfig, "--state", shippedState, "--now", fixedNow}, args...)
	return runRaw(t, full...)
}

func runRaw(t *testing.T, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, &stdout, &stderr)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func decode(t *testing.T, payload string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, payload)
	}
	return out
}

func TestExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"help", []string{"help"}, ExitOK},
		{"version", []string{"version"}, ExitOK},
		{"no arguments prints usage", nil, ExitOK},
		{"roles", []string{"roles"}, ExitOK},
		{"validate", []string{"validate"}, ExitOK},
		{"doctor", []string{"doctor"}, ExitOK},
		{"match", []string{"match", "foreman"}, ExitOK},
		{"explain", []string{"explain", "foreman", "cursor-sol"}, ExitOK},
		{"explain an ineligible runtime still explains", []string{"explain", "foreman", "cursor-composer"}, ExitOK},
		{"availability", []string{"availability"}, ExitOK},
		{"availability for a role", []string{"availability", "foreman"}, ExitOK},

		{"unknown command", []string{"frobnicate"}, ExitUsage},
		{"unknown flag", []string{"match", "foreman", "--nonsense"}, ExitUsage},
		{"match with no role", []string{"match"}, ExitUsage},
		{"explain with one argument", []string{"explain", "foreman"}, ExitUsage},
		{"unknown role", []string{"match", "nonexistent"}, ExitUsage},
		{"unknown runtime", []string{"explain", "foreman", "nonexistent"}, ExitUsage},
		{"bad require syntax", []string{"match", "foreman", "--require", "coding"}, ExitUsage},
		{"bad require level", []string{"match", "foreman", "--require", "coding=extreme"}, ExitUsage},
		{"bad prefer", []string{"match", "foreman", "--prefer", "vibes"}, ExitUsage},
		{"bad now", []string{"--now", "yesterday", "roles"}, ExitUsage},
		{"roles with an argument", []string{"roles", "foreman"}, ExitUsage},
		{"bad role subcommand", []string{"role", "list"}, ExitUsage},

		{"no eligible candidate", []string{"match", "foreman", "--require", "local_execution=very_high"}, ExitNoCandidate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := run(t, tt.args...)
			if got.code != tt.want {
				t.Fatalf("exit = %d, want %d\nstdout: %s\nstderr: %s", got.code, tt.want, got.stdout, got.stderr)
			}
		})
	}
}

func TestInvalidConfigurationExitCode(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "roles"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "runtimes"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	broken := "schema_version: howlforge.role/v1\nid: broken\ndescription: x\npreferred_runtimes:\n  - id: nowhere\nhandoff_required: true\nverification_required: false\n"
	if err := os.WriteFile(filepath.Join(root, "roles", "broken.yaml"), []byte(broken), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := runRaw(t, "--config", root, "--state", filepath.Join(root, "absent.json"), "validate")
	if got.code != ExitInvalidConfig {
		t.Fatalf("exit = %d, want %d\n%s%s", got.code, ExitInvalidConfig, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "matches no configured runtime") {
		t.Fatalf("validate output does not explain the defect:\n%s", got.stdout)
	}
}

func TestUnparseableConfigurationExitCode(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"roles", "runtimes"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "roles", "bad.yaml"), []byte("schema_version: howlforge.role/v99\nid: x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := runRaw(t, "--config", root, "validate")
	if got.code != ExitInvalidConfig {
		t.Fatalf("exit = %d, want %d\n%s", got.code, ExitInvalidConfig, got.stderr)
	}
}

func TestGlobalFlagsWorkBeforeAndAfterTheCommand(t *testing.T) {
	before := runRaw(t, "--config", shippedConfig, "--state", shippedState, "--now", fixedNow, "--json", "match", "foreman")
	after := runRaw(t, "match", "foreman", "--config", shippedConfig, "--state", shippedState, "--now", fixedNow, "--json")

	if before.code != after.code {
		t.Fatalf("exit codes differ: %d then %d", before.code, after.code)
	}
	if before.stdout != after.stdout {
		t.Fatalf("flag position changed the output:\n--- before ---\n%s\n--- after ---\n%s", before.stdout, after.stdout)
	}
}

func TestFlagsAfterPositionalArguments(t *testing.T) {
	// Regression: the flag package stops at the first positional argument, so
	// this form used to be a usage error even though it is the form the
	// documentation uses.
	got := run(t, "match", "implementer", "--require", "coding=high")
	if got.code != ExitOK {
		t.Fatalf("exit = %d, want 0\nstderr: %s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "Role: implementer") {
		t.Fatalf("output did not describe the requested role:\n%s", got.stdout)
	}
}

func TestJSONOutputIsSchemaStamped(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		schema string
	}{
		{"roles", []string{"roles"}, SchemaRoles},
		{"role show", []string{"role", "show", "foreman"}, SchemaRole},
		{"runtimes", []string{"runtimes"}, SchemaRuntimes},
		{"runtime show", []string{"runtime", "show", "cursor-sol"}, SchemaRuntime},
		{"personas", []string{"personas"}, SchemaPersonas},
		{"persona show", []string{"persona", "show", "rintaro"}, SchemaPersona},
		{"capabilities", []string{"capabilities"}, SchemaCapabilities},
		{"match", []string{"match", "foreman"}, SchemaMatch},
		{"explain", []string{"explain", "foreman", "cursor-sol"}, SchemaExplain},
		{"availability", []string{"availability"}, SchemaAvailability},
		{"availability for a role", []string{"availability", "foreman"}, SchemaAvailability},
		{"validate", []string{"validate"}, SchemaValidation},
		{"doctor", []string{"doctor"}, SchemaDoctor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := run(t, append(tt.args, "--json")...)
			if got.code != ExitOK {
				t.Fatalf("exit = %d\nstderr: %s", got.code, got.stderr)
			}
			payload := decode(t, got.stdout)
			if payload["schema_version"] != tt.schema {
				t.Fatalf("schema_version = %v, want %q", payload["schema_version"], tt.schema)
			}
		})
	}
}

func TestJSONMatchStructure(t *testing.T) {
	got := run(t, "match", "foreman", "--json")
	if got.code != ExitOK {
		t.Fatalf("exit = %d\n%s", got.code, got.stderr)
	}
	payload := decode(t, got.stdout)

	if payload["role"] != "foreman" {
		t.Fatalf("role = %v", payload["role"])
	}
	candidates, ok := payload["candidates"].([]any)
	if !ok || len(candidates) == 0 {
		t.Fatalf("candidates = %v", payload["candidates"])
	}
	first, ok := candidates[0].(map[string]any)
	if !ok {
		t.Fatalf("first candidate = %v", candidates[0])
	}
	// These fields are the contract HowlPlane consumes.
	for _, key := range []string{"rank", "runtime_id", "label", "provider", "model", "runtime", "state", "preference_tier"} {
		if _, present := first[key]; !present {
			t.Errorf("candidate is missing %q: %v", key, first)
		}
	}
	if first["runtime_id"] != "claude-code-opus" {
		t.Fatalf("top candidate during the session limit = %v, want claude-code-opus", first["runtime_id"])
	}
	if _, present := payload["rejected"]; !present {
		t.Fatal("the rejected list is missing from JSON output")
	}
}

func TestJSONOutputIsDeterministic(t *testing.T) {
	// Byte identical across runs is the property automation depends on.
	baseline := run(t, "match", "foreman", "--prefer", "quality", "--json")
	if baseline.code != ExitOK {
		t.Fatalf("exit = %d\n%s", baseline.code, baseline.stderr)
	}
	for i := range 25 {
		got := run(t, "match", "foreman", "--prefer", "quality", "--json")
		if got.stdout != baseline.stdout {
			t.Fatalf("run %d differed from the baseline:\n--- baseline ---\n%s\n--- run %d ---\n%s",
				i, baseline.stdout, i, got.stdout)
		}
	}
}

func TestSessionLimitWorkflow(t *testing.T) {
	// The end to end workflow from section 25 of the specification.
	t.Run("validate passes", func(t *testing.T) {
		if got := run(t, "validate"); got.code != ExitOK {
			t.Fatalf("validate exit = %d\n%s%s", got.code, got.stdout, got.stderr)
		}
	})

	t.Run("availability shows the limited preferred runtime", func(t *testing.T) {
		got := run(t, "availability", "foreman")
		if got.code != ExitOK {
			t.Fatalf("exit = %d\n%s", got.code, got.stderr)
		}
		for _, want := range []string{"cursor-sol", "SESSION_LIMIT", "eligible after reset", "claude-code-opus", "AVAILABLE"} {
			if !strings.Contains(got.stdout, want) {
				t.Fatalf("availability output is missing %q:\n%s", want, got.stdout)
			}
		}
		if !strings.Contains(got.stdout, "recommended runtime: claude-code-opus") {
			t.Fatalf("no recommendation was made:\n%s", got.stdout)
		}
	})

	t.Run("match rotates to the fallback", func(t *testing.T) {
		got := run(t, "match", "foreman", "--json")
		payload := decode(t, got.stdout)
		candidates := payload["candidates"].([]any)
		first := candidates[0].(map[string]any)
		if first["runtime_id"] != "claude-code-opus" {
			t.Fatalf("during the session limit the top candidate is %v, want claude-code-opus", first["runtime_id"])
		}
	})

	t.Run("the preferred runtime returns after the reset", func(t *testing.T) {
		got := runRaw(t, "--config", shippedConfig, "--state", shippedState, "--now", afterReset, "match", "foreman", "--json")
		payload := decode(t, got.stdout)
		candidates := payload["candidates"].([]any)
		first := candidates[0].(map[string]any)
		if first["runtime_id"] != "cursor-sol" {
			t.Fatalf("after the reset the top candidate is %v, want cursor-sol", first["runtime_id"])
		}
	})
}

func TestExplainRendersRejections(t *testing.T) {
	got := run(t, "explain", "foreman", "cursor-composer")
	if got.code != ExitOK {
		t.Fatalf("exit = %d\n%s", got.code, got.stderr)
	}
	for _, want := range []string{"Role: foreman", "Required:", "Candidate:", "Matched:", "Eligible: no", "Rejected: cursor-composer", "Reason:"} {
		if !strings.Contains(got.stdout, want) {
			t.Fatalf("explain output is missing %q:\n%s", want, got.stdout)
		}
	}
}

func TestExplainJSONCarriesEligibility(t *testing.T) {
	eligible := decode(t, run(t, "explain", "foreman", "claude-code-opus", "--json").stdout)
	if eligible["eligible"] != true {
		t.Fatalf("eligible = %v, want true", eligible["eligible"])
	}
	rejected := decode(t, run(t, "explain", "foreman", "cursor-composer", "--json").stdout)
	if rejected["eligible"] != false {
		t.Fatalf("eligible = %v, want false", rejected["eligible"])
	}
	if _, present := rejected["rejection"]; !present {
		t.Fatal("a rejected explanation carries no rejection object")
	}
}

func TestHandoffCommands(t *testing.T) {
	dir := writeValidBundle(t)

	t.Run("validate accepts a complete bundle", func(t *testing.T) {
		got := runRaw(t, "handoff", "validate", dir)
		if got.code != ExitOK {
			t.Fatalf("exit = %d\n%s%s", got.code, got.stdout, got.stderr)
		}
		if !strings.Contains(got.stdout, "VALID") {
			t.Fatalf("output:\n%s", got.stdout)
		}
	})

	t.Run("inspect summarizes the bundle", func(t *testing.T) {
		got := runRaw(t, "handoff", "inspect", dir)
		if got.code != ExitOK {
			t.Fatalf("exit = %d\n%s", got.code, got.stderr)
		}
		for _, want := range []string{"HOWL-0042", "implementer", "Completed", "Remaining", "Decisions", "Verification"} {
			if !strings.Contains(got.stdout, want) {
				t.Fatalf("inspect output is missing %q:\n%s", want, got.stdout)
			}
		}
	})

	t.Run("a malformed bundle exits with the handoff code", func(t *testing.T) {
		broken := t.TempDir()
		if err := os.WriteFile(filepath.Join(broken, "checkpoint.json"), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		got := runRaw(t, "handoff", "validate", broken)
		if got.code != ExitBadHandoff {
			t.Fatalf("exit = %d, want %d\n%s", got.code, ExitBadHandoff, got.stdout)
		}
	})

	t.Run("a missing directory exits with the handoff code", func(t *testing.T) {
		got := runRaw(t, "handoff", "validate", filepath.Join(t.TempDir(), "nope"))
		if got.code != ExitBadHandoff {
			t.Fatalf("exit = %d, want %d", got.code, ExitBadHandoff)
		}
	})

	t.Run("JSON output is schema stamped", func(t *testing.T) {
		got := runRaw(t, "handoff", "validate", dir, "--json")
		payload := decode(t, got.stdout)
		if payload["schema_version"] != SchemaHandoff {
			t.Fatalf("schema_version = %v", payload["schema_version"])
		}
		if payload["valid"] != true {
			t.Fatalf("valid = %v", payload["valid"])
		}
	})
}

func TestResumeCheck(t *testing.T) {
	dir := writeValidBundle(t)

	t.Run("an unverifiable repository is not safe", func(t *testing.T) {
		got := runRaw(t, "resume", "check", dir, "--repo", t.TempDir())
		if got.code != ExitNotResumable {
			t.Fatalf("exit = %d, want %d\n%s", got.code, ExitNotResumable, got.stdout)
		}
		if !strings.Contains(got.stdout, "NEEDS_RECONCILIATION") && !strings.Contains(got.stdout, "STALE") {
			t.Fatalf("output:\n%s", got.stdout)
		}
	})

	t.Run("skipping git never yields SAFE", func(t *testing.T) {
		got := runRaw(t, "resume", "check", dir, "--no-git")
		if got.code != ExitNotResumable {
			t.Fatalf("exit = %d, want %d\n%s", got.code, ExitNotResumable, got.stdout)
		}
		if !strings.Contains(got.stdout, "NEEDS_RECONCILIATION") {
			t.Fatalf("output:\n%s", got.stdout)
		}
	})

	t.Run("JSON output carries the verdict", func(t *testing.T) {
		got := runRaw(t, "resume", "check", dir, "--no-git", "--json")
		payload := decode(t, got.stdout)
		if payload["schema_version"] != SchemaResume {
			t.Fatalf("schema_version = %v", payload["schema_version"])
		}
		if payload["resume_state"] != "NEEDS_RECONCILIATION" {
			t.Fatalf("resume_state = %v", payload["resume_state"])
		}
		checks, ok := payload["checks"].([]any)
		if !ok || len(checks) == 0 {
			t.Fatal("no per rule checks were reported")
		}
	})

	t.Run("usage errors", func(t *testing.T) {
		if got := runRaw(t, "resume"); got.code != ExitUsage {
			t.Fatalf("exit = %d, want %d", got.code, ExitUsage)
		}
		if got := runRaw(t, "resume", "inspect", dir); got.code != ExitUsage {
			t.Fatalf("exit = %d, want %d", got.code, ExitUsage)
		}
	})
}

func TestRoleShowRendersTheContract(t *testing.T) {
	got := run(t, "role", "show", "implementer")
	if got.code != ExitOK {
		t.Fatalf("exit = %d\n%s", got.code, got.stderr)
	}
	for _, want := range []string{"Role: implementer", "Responsibilities", "Required capabilities", "Preferred runtimes", "handoff required", "verifier role"} {
		if !strings.Contains(got.stdout, want) {
			t.Fatalf("role show output is missing %q:\n%s", want, got.stdout)
		}
	}
}

func TestShippedExamplePersonaResolves(t *testing.T) {
	got := run(t, "match", "--persona", "rintaro", "--json")
	if got.code != ExitOK {
		t.Fatalf("exit = %d\n%s", got.code, got.stderr)
	}
	payload := decode(t, got.stdout)
	if payload["role"] != "product" {
		t.Fatalf("persona resolved to role %v, want product", payload["role"])
	}
}

// writeValidBundle materializes a complete handoff bundle in a temp directory.
func writeValidBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"checkpoint.json": `{
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
		"summary.md":        "# Summary\n\nBuilt the runtime registry.\n",
		"remaining-work.md": "- integration tests\n- documentation\n",
		"changed-files.txt": "internal/runtimes/runtime.go\n",
		"decisions.json":    `{"schema_version":"howlforge.decisions/v1","decisions":[{"id":"D1","decision":"runtime files stay declarative","reversible":false}]}`,
		"verification.json": `{"schema_version":"howlforge.verification/v1","independent":false,"checks":[{"name":"go test ./...","status":"passed"}]}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}
