package runtimes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/howlcipher/howlforge/internal/capabilities"
	"github.com/howlcipher/howlforge/internal/config"
)

func loaderFor(t *testing.T, files map[string]string) *config.Loader {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	loader, err := config.NewLoader(root)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	return loader
}

const validRuntime = `schema_version: howlforge.runtime/v1
id: cursor-sol
description: Sol through Cursor.
provider: openai
model: gpt-5.6-sol
runtime: cursor
economic_class: subscription
locality: remote
state:
  enabled: true
capabilities:
  reasoning: very_high
  coding: very_high
`

func TestLoadValidRuntime(t *testing.T) {
	loader := loaderFor(t, map[string]string{"runtimes/cursor-sol.yaml": validRuntime})
	parsed, err := Load(loader, "runtimes")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("loaded %d runtimes, want 1", len(parsed))
	}
	rt := parsed[0]
	if rt.Provider != "openai" || rt.Model != "gpt-5.6-sol" || rt.Client != "cursor" {
		t.Fatalf("identity = %s/%s/%s", rt.Provider, rt.Model, rt.Client)
	}
	if !rt.State.Enabled {
		t.Fatal("state.enabled was not read")
	}
	if rt.EconomicClass != Subscription || rt.Locality != Remote {
		t.Fatalf("classification = %s/%s", rt.EconomicClass, rt.Locality)
	}
	if got := rt.Capabilities.Get(capabilities.Coding); got != capabilities.LevelVeryHigh {
		t.Fatalf("coding = %v", got)
	}
	if rt.Label() != "cursor / gpt-5.6-sol" {
		t.Fatalf("Label() = %q", rt.Label())
	}
}

func TestRuntimeDefinitionsAreDeclarativeOnly(t *testing.T) {
	// A runtime file must not be able to smuggle an executable command into
	// HowlForge. Strict decoding is what enforces that, so it is asserted.
	hostile := validRuntime + "command: [\"/bin/sh\", \"-c\", \"curl evil\"]\n"
	loader := loaderFor(t, map[string]string{"runtimes/x.yaml": hostile})
	_, err := Load(loader, "runtimes")
	if err == nil {
		t.Fatal("a runtime definition carrying a command field was accepted")
	}
	if !strings.Contains(err.Error(), "command") {
		t.Fatalf("Load returned %q, want it to name the rejected field", err)
	}
}

func TestLoadRejectsMalformedRuntimes(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{"wrong schema version", "schema_version: howlforge.runtime/v2\nid: a\nprovider: p\nmodel: m\nruntime: r\nstate:\n  enabled: true\n", "schema_version"},
		{"missing provider", "schema_version: howlforge.runtime/v1\nid: a\nprovider: \"\"\nmodel: m\nruntime: r\nstate:\n  enabled: true\n", "provider must not be empty"},
		{"missing model", "schema_version: howlforge.runtime/v1\nid: a\nprovider: p\nmodel: \"\"\nruntime: r\nstate:\n  enabled: true\n", "model must not be empty"},
		{"missing runtime", "schema_version: howlforge.runtime/v1\nid: a\nprovider: p\nmodel: m\nruntime: \"\"\nstate:\n  enabled: true\n", "runtime must not be empty"},
		{"bad economic class", "schema_version: howlforge.runtime/v1\nid: a\nprovider: p\nmodel: m\nruntime: r\neconomic_class: free_beer\nstate:\n  enabled: true\n", "economic_class"},
		{"bad locality", "schema_version: howlforge.runtime/v1\nid: a\nprovider: p\nmodel: m\nruntime: r\nlocality: orbital\nstate:\n  enabled: true\n", "locality"},
		{"bad capability level", "schema_version: howlforge.runtime/v1\nid: a\nprovider: p\nmodel: m\nruntime: r\nstate:\n  enabled: true\ncapabilities:\n  coding: extreme\n", "unknown capability level"},
		{"bad id", "schema_version: howlforge.runtime/v1\nid: Cursor Sol\nprovider: p\nmodel: m\nruntime: r\nstate:\n  enabled: true\n", "lowercase"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := loaderFor(t, map[string]string{"runtimes/x.yaml": tt.content})
			_, err := Load(loader, "runtimes")
			if err == nil {
				t.Fatalf("Load accepted %s", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load returned %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestMatchesRef(t *testing.T) {
	rt := &Runtime{ID: "cursor-sol", Provider: "openai", Model: "gpt-5.6-sol", Client: "cursor"}
	tests := []struct {
		name                        string
		id, provider, model, client string
		want                        bool
	}{
		{"an empty selector matches everything", "", "", "", "", true},
		{"id alone matches", "cursor-sol", "", "", "", true},
		{"provider and model match", "", "openai", "gpt-5.6-sol", "", true},
		{"client narrows the match", "", "openai", "gpt-5.6-sol", "cursor", true},
		{"a different client does not match", "", "openai", "gpt-5.6-sol", "codex-cli", false},
		{"a different model does not match", "", "openai", "gpt-5.5", "", false},
		{"a different id does not match", "codex-cli-sol", "", "", "", false},
		{"matching is case insensitive", "CURSOR-SOL", "OpenAI", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rt.MatchesRef(tt.id, tt.provider, tt.model, tt.client); got != tt.want {
				t.Fatalf("MatchesRef = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegistryRejectsDuplicateIDs(t *testing.T) {
	loader := loaderFor(t, map[string]string{
		"runtimes/a.yaml": "schema_version: howlforge.runtime/v1\nid: dup\nprovider: p\nmodel: m\nruntime: r\nstate:\n  enabled: true\n",
		"runtimes/b.yaml": "schema_version: howlforge.runtime/v1\nid: dup\nprovider: q\nmodel: n\nruntime: s\nstate:\n  enabled: true\n",
	})
	parsed, err := Load(loader, "runtimes")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := NewRegistry(parsed); err == nil {
		t.Fatal("NewRegistry accepted two runtimes with the same id")
	}
}

func TestRegistryOrderingIsDeterministic(t *testing.T) {
	files := map[string]string{}
	for _, id := range []string{"zulu", "alpha", "mike"} {
		files["runtimes/"+id+".yaml"] = "schema_version: howlforge.runtime/v1\nid: " + id +
			"\nprovider: p\nmodel: m\nruntime: r\nstate:\n  enabled: true\n"
	}
	loader := loaderFor(t, files)
	parsed, _ := Load(loader, "runtimes")
	registry, _ := NewRegistry(parsed)

	want := []string{"alpha", "mike", "zulu"}
	// Repeated calls must not depend on Go map iteration order.
	for range 20 {
		got := registry.IDs()
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("IDs() = %v, want %v", got, want)
			}
		}
	}
}

func TestEnabledFiltersDisabledRuntimes(t *testing.T) {
	loader := loaderFor(t, map[string]string{
		"runtimes/on.yaml":  "schema_version: howlforge.runtime/v1\nid: on\nprovider: p\nmodel: m\nruntime: r\nstate:\n  enabled: true\n",
		"runtimes/off.yaml": "schema_version: howlforge.runtime/v1\nid: off\nprovider: p\nmodel: m\nruntime: r\nstate:\n  enabled: false\n",
	})
	parsed, _ := Load(loader, "runtimes")
	registry, _ := NewRegistry(parsed)
	enabled := registry.Enabled()
	if len(enabled) != 1 || enabled[0].ID != "on" {
		t.Fatalf("Enabled() = %v, want only the enabled runtime", enabled)
	}
	if registry.Len() != 2 {
		t.Fatalf("Len() = %d, want both runtimes registered", registry.Len())
	}
}

func TestValidateCapabilitiesAgainstVocabulary(t *testing.T) {
	loader := loaderFor(t, map[string]string{
		"runtimes/a.yaml": "schema_version: howlforge.runtime/v1\nid: a\nprovider: p\nmodel: m\nruntime: r\nstate:\n  enabled: true\ncapabilities:\n  telepathy: high\n",
	})
	parsed, err := Load(loader, "runtimes")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	registry, _ := NewRegistry(parsed)
	if err := registry.ValidateCapabilities(capabilities.DefaultVocabulary()); err == nil {
		t.Fatal("ValidateCapabilities accepted an undefined capability name")
	}
}

func TestIdentifierRuleMatchesRoles(t *testing.T) {
	// runtimes deliberately duplicates the identifier rule rather than
	// importing roles. The two must stay identical, so the shapes are
	// compared here instead of trusted.
	cases := []string{"foreman", "claude-code-opus", "agent_1", "", "Foreman", "a b", "a/b", strings.Repeat("a", 65)}
	for _, id := range cases {
		local := validateID(id) == nil
		shared := sharedIDRule(id)
		if local != shared {
			t.Fatalf("identifier rules disagree for %q: runtimes=%v roles=%v", id, local, shared)
		}
	}
}

// sharedIDRule mirrors roles.ValidateID without importing it, so the two
// packages stay independent while the rule stays pinned by test.
func sharedIDRule(id string) bool {
	if strings.TrimSpace(id) == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}
