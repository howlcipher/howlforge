package howlforge_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/howlcipher/howlforge/pkg/howlforge"
)

var evalTime = time.Date(2026, 9, 22, 15, 0, 0, 0, time.UTC)

// workspace writes a minimal but realistic configuration tree and returns its
// root. Overrides replace or, with an empty value, delete a file.
func workspace(t *testing.T, overrides map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"roles/product.yaml": `schema_version: howlforge.role/v1
id: product
description: Defines scope.
required_capabilities:
  planning: high
preferred_runtimes:
  - id: claude-code-opus
fallback_runtimes:
  - id: agy-gemini-flash
handoff_required: true
verification_required: false
`,
		"roles/implementer.yaml": `schema_version: howlforge.role/v1
id: implementer
description: Writes code.
required_capabilities:
  coding: high
preferred_runtimes:
  - id: agy-gemini-flash
fallback_runtimes:
  - id: claude-code-opus
handoff_required: true
verification_required: true
verification:
  required: true
  verifier_role: product
  independent_runtime: required
`,
		"runtimes/claude-code-opus.yaml": `schema_version: howlforge.runtime/v1
id: claude-code-opus
provider: anthropic
model: opus
runtime: claude-code
economic_class: subscription
locality: remote
state:
  enabled: true
capabilities:
  planning: very_high
  coding: very_high
  speed: medium
  cost_efficiency: medium
`,
		"runtimes/agy-gemini-flash.yaml": `schema_version: howlforge.runtime/v1
id: agy-gemini-flash
provider: google
model: gemini-flash
runtime: agy
economic_class: subscription
locality: remote
state:
  enabled: true
capabilities:
  planning: high
  coding: high
  speed: very_high
  cost_efficiency: very_high
`,
		"personas/rintaro.yaml": `schema_version: howlforge.persona/v1
id: rintaro
role: product
display_name: Rintaro
`,
	}
	for name, content := range overrides {
		if content == "" {
			delete(files, name)
			continue
		}
		files[name] = content
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

func load(t *testing.T, root string) *howlforge.Forge {
	t.Helper()
	forge, err := howlforge.Load(howlforge.Options{Root: root, StatePath: filepath.Join(t.TempDir(), "absent.json")})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return forge
}

func TestLoadAndInspect(t *testing.T) {
	forge := load(t, workspace(t, nil))

	if len(forge.Roles()) != 2 {
		t.Fatalf("loaded %d roles, want 2", len(forge.Roles()))
	}
	if len(forge.Runtimes()) != 2 {
		t.Fatalf("loaded %d runtimes, want 2", len(forge.Runtimes()))
	}
	if len(forge.Personas()) != 1 {
		t.Fatalf("loaded %d personas, want 1", len(forge.Personas()))
	}
	if len(forge.Capabilities()) != 14 {
		t.Fatalf("vocabulary has %d entries, want the 14 built-ins", len(forge.Capabilities()))
	}
	if forge.StatePath() != "" {
		t.Fatalf("StatePath() = %q, want empty when no state file exists", forge.StatePath())
	}
}

func TestPersonaResolvesToRoleThenRuntime(t *testing.T) {
	forge := load(t, workspace(t, nil))
	ctx := context.Background()

	// The full chain the architecture promises: persona to role to runtime,
	// with the persona never touching the runtime decision.
	viaPersona, err := forge.Match(ctx, howlforge.MatchRequest{Persona: "rintaro", Now: evalTime})
	if err != nil {
		t.Fatalf("Match by persona: %v", err)
	}
	viaRole, err := forge.Match(ctx, howlforge.MatchRequest{Role: "product", Now: evalTime})
	if err != nil {
		t.Fatalf("Match by role: %v", err)
	}
	if len(viaPersona) != len(viaRole) || len(viaPersona) == 0 {
		t.Fatalf("persona produced %d candidates, role produced %d", len(viaPersona), len(viaRole))
	}
	for i := range viaRole {
		if viaPersona[i].RuntimeID != viaRole[i].RuntimeID {
			t.Fatalf("persona and role disagree at %d: %s vs %s", i, viaPersona[i].RuntimeID, viaRole[i].RuntimeID)
		}
	}
	if viaPersona[0].RuntimeID != "claude-code-opus" {
		t.Fatalf("top candidate = %q, want the role's preferred runtime", viaPersona[0].RuntimeID)
	}
}

func TestPersonaAndRoleMustAgree(t *testing.T) {
	forge := load(t, workspace(t, nil))
	_, err := forge.Match(context.Background(), howlforge.MatchRequest{
		Role: "implementer", Persona: "rintaro", Now: evalTime,
	})
	if err == nil {
		t.Fatal("a persona conflicting with an explicit role was accepted")
	}
	if !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("error = %q, want it to explain the conflict", err)
	}
}

func TestMatchRequiresARoleOrPersona(t *testing.T) {
	forge := load(t, workspace(t, nil))
	if _, err := forge.Match(context.Background(), howlforge.MatchRequest{Now: evalTime}); err == nil {
		t.Fatal("Match accepted a request naming neither a role nor a persona")
	}
}

func TestAddingARuntimeNeedsNoCodeChange(t *testing.T) {
	// The extensibility requirement: a brand new runtime must become eligible
	// through configuration alone.
	root := workspace(t, nil)
	ctx := context.Background()

	before := load(t, root)
	baseline, err := before.Match(ctx, howlforge.MatchRequest{Role: "implementer", Now: evalTime})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	for _, candidate := range baseline {
		if candidate.RuntimeID == "future-provider" {
			t.Fatal("the new runtime was somehow already present")
		}
	}

	newRuntime := `schema_version: howlforge.runtime/v1
id: future-provider
provider: notyetinvented
model: model-x
runtime: future-cli
economic_class: unknown
locality: remote
state:
  enabled: true
capabilities:
  planning: very_high
  coding: very_high
  speed: high
  cost_efficiency: high
`
	if err := os.WriteFile(filepath.Join(root, "runtimes", "future-provider.yaml"), []byte(newRuntime), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	after := load(t, root)
	candidates, err := after.Match(ctx, howlforge.MatchRequest{Role: "implementer", Now: evalTime})
	if err != nil {
		t.Fatalf("Match after adding a runtime: %v", err)
	}
	found := false
	for _, candidate := range candidates {
		if candidate.RuntimeID == "future-provider" {
			found = true
			// Not named by any role, so it is offered last rather than hidden.
			if candidate.PreferenceTier != howlforge.TierEligible {
				t.Fatalf("tier = %q, want eligible", candidate.PreferenceTier)
			}
		}
	}
	if !found {
		t.Fatal("a newly configured runtime was not considered")
	}

	// It must also be usable for a role that names it, again with no code
	// change, and validation must stay clean.
	report, err := after.Validate(ctx)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !report.OK {
		t.Fatalf("adding a runtime broke validation: %v", report.Errors())
	}
}

func TestCapabilityVocabularyExtension(t *testing.T) {
	root := workspace(t, map[string]string{
		"capabilities.yaml": "schema_version: howlforge.capabilities/v1\ncapabilities:\n  - negotiation\n",
		"roles/product.yaml": `schema_version: howlforge.role/v1
id: product
description: Defines scope.
required_capabilities:
  negotiation: medium
handoff_required: true
verification_required: false
`,
		"runtimes/claude-code-opus.yaml": `schema_version: howlforge.runtime/v1
id: claude-code-opus
provider: anthropic
model: opus
runtime: claude-code
state:
  enabled: true
capabilities:
  negotiation: high
  coding: very_high
`,
	})
	forge := load(t, root)
	report, err := forge.Validate(context.Background())
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !report.OK {
		t.Fatalf("an extended vocabulary failed validation: %v", report.Errors())
	}
	candidates, err := forge.Match(context.Background(), howlforge.MatchRequest{Role: "product", Now: evalTime})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("an extended capability did not match")
	}
}

func TestValidateDetectsConfigurationDefects(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
		wantIn    string
		loadFails bool
	}{
		{
			name: "runtime reference matches nothing",
			overrides: map[string]string{
				"roles/product.yaml": `schema_version: howlforge.role/v1
id: product
description: x
required_capabilities:
  planning: high
preferred_runtimes:
  - id: does-not-exist
handoff_required: true
verification_required: false
`,
			},
			wantIn: "matches no configured runtime",
		},
		{
			name: "persona points at a missing role",
			overrides: map[string]string{
				"personas/ghost.yaml": "schema_version: howlforge.persona/v1\nid: ghost\nrole: nowhere\n",
			},
			wantIn: "references unknown role",
		},
		{
			name: "verifier role does not exist",
			overrides: map[string]string{
				"roles/implementer.yaml": `schema_version: howlforge.role/v1
id: implementer
description: x
required_capabilities:
  coding: high
handoff_required: true
verification_required: true
verification:
  required: true
  verifier_role: nobody
`,
			},
			wantIn: "unknown verifier_role",
		},
		{
			name: "a role verifies itself",
			overrides: map[string]string{
				"roles/implementer.yaml": `schema_version: howlforge.role/v1
id: implementer
description: x
required_capabilities:
  coding: high
handoff_required: true
verification_required: true
verification:
  required: true
  verifier_role: implementer
`,
			},
			wantIn: "must not self certify",
		},
		{
			name: "no runtime can ever fill the role",
			overrides: map[string]string{
				"roles/implementer.yaml": `schema_version: howlforge.role/v1
id: implementer
description: x
required_capabilities:
  local_execution: very_high
handoff_required: true
verification_required: false
`,
			},
			wantIn: "can never be filled",
		},
		{
			name: "unknown capability name",
			overrides: map[string]string{
				"roles/implementer.yaml": `schema_version: howlforge.role/v1
id: implementer
description: x
required_capabilities:
  codng: high
handoff_required: true
verification_required: false
`,
			},
			wantIn: "did you mean",
		},
		{
			name: "duplicate role ids refuse to load",
			overrides: map[string]string{
				"roles/copy.yaml": `schema_version: howlforge.role/v1
id: product
description: a copy
handoff_required: true
verification_required: false
`,
			},
			wantIn:    "duplicate role id",
			loadFails: true,
		},
		{
			name: "circular inheritance refuses to load",
			overrides: map[string]string{
				"roles/a.yaml": "schema_version: howlforge.role/v1\nid: a\ndescription: x\nextends: b\n",
				"roles/b.yaml": "schema_version: howlforge.role/v1\nid: b\ndescription: x\nextends: a\n",
			},
			wantIn:    "circular role inheritance",
			loadFails: true,
		},
		{
			name: "unsupported schema version refuses to load",
			overrides: map[string]string{
				"roles/product.yaml": "schema_version: howlforge.role/v99\nid: product\ndescription: x\n",
			},
			wantIn:    "schema_version",
			loadFails: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := workspace(t, tt.overrides)
			forge, err := howlforge.Load(howlforge.Options{
				Root: root, StatePath: filepath.Join(t.TempDir(), "absent.json"),
			})
			if tt.loadFails {
				if err == nil {
					t.Fatal("Load accepted a configuration it should refuse")
				}
				if !strings.Contains(err.Error(), tt.wantIn) {
					t.Fatalf("Load returned %q, want it to mention %q", err, tt.wantIn)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			report, err := forge.Validate(context.Background())
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if report.OK {
				t.Fatalf("Validate accepted %s", tt.name)
			}
			joined := ""
			for _, problem := range report.Problems {
				joined += problem.String() + "\n"
			}
			if !strings.Contains(joined, tt.wantIn) {
				t.Fatalf("problems %q do not mention %q", joined, tt.wantIn)
			}
		})
	}
}

func TestValidateWarnsOnSingleFillableRuntime(t *testing.T) {
	root := workspace(t, map[string]string{
		"roles/implementer.yaml": `schema_version: howlforge.role/v1
id: implementer
description: x
required_capabilities:
  coding: very_high
handoff_required: true
verification_required: false
`,
	})
	forge := load(t, root)
	report, err := forge.Validate(context.Background())
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// Only opus declares coding very_high, so the role has no fallback. That
	// is legal but is exactly the condition that strands work at a session
	// limit, so it must be surfaced.
	if !report.OK {
		t.Fatalf("a single fillable runtime should warn, not fail: %v", report.Errors())
	}
	found := false
	for _, warning := range report.Warnings() {
		if strings.Contains(warning.Message, "no fallback") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no warning about the missing fallback: %v", report.Problems)
	}
}

func TestValidateWarnsOnStaleAvailabilityEntry(t *testing.T) {
	root := workspace(t, nil)
	statePath := filepath.Join(t.TempDir(), "state.json")
	state := `{"schema_version":"howlforge.availability/v1","runtimes":[{"runtime_id":"renamed-runtime","state":"AVAILABLE"}]}`
	if err := os.WriteFile(statePath, []byte(state), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	forge, err := howlforge.Load(howlforge.Options{Root: root, StatePath: statePath})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	report, err := forge.Validate(context.Background())
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	found := false
	for _, warning := range report.Warnings() {
		if strings.Contains(warning.Message, "not configured") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a stale availability entry was not reported: %v", report.Problems)
	}
}

func TestPublicInterfacesAreSatisfied(t *testing.T) {
	forge := load(t, workspace(t, nil))
	ctx := context.Background()

	// The documented consumption path for HowlPlane, exercised through the
	// interfaces rather than the concrete type.
	var matcher howlforge.Matcher = forge
	candidates, err := matcher.Match(ctx, howlforge.MatchRequest{Role: "implementer", Now: evalTime})
	if err != nil || len(candidates) == 0 {
		t.Fatalf("Matcher.Match = %v, %v", candidates, err)
	}

	var explainer howlforge.Explainer = forge
	explanation, err := explainer.Explain(ctx, howlforge.MatchRequest{Role: "implementer", Now: evalTime}, candidates[0].RuntimeID)
	if err != nil {
		t.Fatalf("Explainer.Explain: %v", err)
	}
	if !explanation.Eligible {
		t.Fatal("Explain contradicted Match")
	}

	var validator howlforge.Validator = forge
	report, err := validator.Validate(ctx)
	if err != nil || !report.OK {
		t.Fatalf("Validator.Validate = %v, %v", report, err)
	}
}

func TestCancelledContextIsRefused(t *testing.T) {
	forge := load(t, workspace(t, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := forge.Match(ctx, howlforge.MatchRequest{Role: "product", Now: evalTime}); err == nil {
		t.Fatal("Match ignored a cancelled context")
	}
	if _, err := forge.Validate(ctx); err == nil {
		t.Fatal("Validate ignored a cancelled context")
	}
}

func TestExcludeSupportsRematch(t *testing.T) {
	// The HowlPlane rematch path: ask again, excluding the worker just lost.
	forge := load(t, workspace(t, nil))
	ctx := context.Background()

	first, err := forge.Match(ctx, howlforge.MatchRequest{Role: "product", Now: evalTime})
	if err != nil || len(first) < 2 {
		t.Fatalf("Match = %v, %v", first, err)
	}
	lost := first[0].RuntimeID

	second, err := forge.Match(ctx, howlforge.MatchRequest{
		Role: "product", Exclude: []string{lost}, Now: evalTime,
	})
	if err != nil {
		t.Fatalf("rematch: %v", err)
	}
	for _, candidate := range second {
		if candidate.RuntimeID == lost {
			t.Fatalf("the excluded runtime %q was offered again", lost)
		}
	}
	if len(second) != len(first)-1 {
		t.Fatalf("rematch returned %d candidates, want %d", len(second), len(first)-1)
	}
	if second[0].RuntimeID != first[1].RuntimeID {
		t.Fatalf("rematch promoted %q, want the previous runner up %q", second[0].RuntimeID, first[1].RuntimeID)
	}
}

func TestLoadRefusesAMissingRoot(t *testing.T) {
	_, err := howlforge.Load(howlforge.Options{Root: filepath.Join(t.TempDir(), "nope")})
	if err == nil {
		t.Fatal("Load accepted a configuration root that does not exist")
	}
}

func TestDefaultRootHonorsEnvironment(t *testing.T) {
	t.Setenv(howlforge.EnvConfigRoot, "/from/env")
	if got := howlforge.DefaultRoot(""); got != "/from/env" {
		t.Fatalf("DefaultRoot = %q, want the environment value", got)
	}
	if got := howlforge.DefaultRoot("/explicit"); got != "/explicit" {
		t.Fatalf("DefaultRoot = %q, want the explicit value", got)
	}
}
