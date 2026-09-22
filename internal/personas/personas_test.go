package personas

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/howlcipher/howlforge/internal/config"
	"github.com/howlcipher/howlforge/internal/roles"
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

func roleRegistry(t *testing.T, ids ...string) *roles.Registry {
	t.Helper()
	var list []*roles.Role
	for _, id := range ids {
		list = append(list, &roles.Role{
			SchemaVersion: roles.SchemaVersion,
			ID:            id,
			Description:   "test role",
		})
	}
	registry, err := roles.NewRegistry(list)
	if err != nil {
		t.Fatalf("build role registry: %v", err)
	}
	return registry
}

func TestPersonasAreOptional(t *testing.T) {
	// The whole directory being absent must not be an error. HowlForge has to
	// work perfectly with no persona configuration at all.
	loader := loaderFor(t, map[string]string{"roles/.keep": ""})
	parsed, err := Load(loader, "personas")
	if err != nil {
		t.Fatalf("Load on a missing personas directory returned %v", err)
	}
	if len(parsed) != 0 {
		t.Fatalf("loaded %d personas from a missing directory", len(parsed))
	}
	registry, err := NewRegistry(parsed)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if registry.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", registry.Len())
	}
	if err := registry.ValidateRoles(roleRegistry(t, "product")); err != nil {
		t.Fatalf("validating an empty persona registry returned %v", err)
	}
}

func TestLoadAndResolve(t *testing.T) {
	loader := loaderFor(t, map[string]string{
		"personas/rintaro.yaml": `schema_version: howlforge.persona/v1
id: rintaro
role: product
display_name: Rintaro
description: The standing product voice.
style_notes:
  - states scope before requirements
`,
	})
	parsed, err := Load(loader, "personas")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	registry, err := NewRegistry(parsed)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	role, err := registry.ResolveRole("rintaro")
	if err != nil {
		t.Fatalf("ResolveRole: %v", err)
	}
	if role != "product" {
		t.Fatalf("ResolveRole = %q, want product", role)
	}
	if err := registry.ValidateRoles(roleRegistry(t, "product")); err != nil {
		t.Fatalf("ValidateRoles: %v", err)
	}
}

func TestPersonaCannotNameARuntime(t *testing.T) {
	// A persona that could pin a model would become the thing that dies when
	// that model does, which is the failure this whole design avoids. Strict
	// decoding is what enforces it.
	for _, field := range []string{"runtime: cursor", "model: opus", "provider: anthropic", "preferred_runtimes: [{id: a}]"} {
		t.Run(field, func(t *testing.T) {
			loader := loaderFor(t, map[string]string{
				"personas/p.yaml": "schema_version: howlforge.persona/v1\nid: p\nrole: product\n" + field + "\n",
			})
			if _, err := Load(loader, "personas"); err == nil {
				t.Fatalf("a persona declaring %q was accepted", field)
			}
		})
	}
}

func TestLoadRejectsMalformedPersonas(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{"wrong schema version", "schema_version: howlforge.persona/v2\nid: p\nrole: product\n", "schema_version"},
		{"missing role", "schema_version: howlforge.persona/v1\nid: p\nrole: \"\"\n", "role"},
		{"bad id", "schema_version: howlforge.persona/v1\nid: Rintaro\nrole: product\n", "lowercase"},
		{"unknown field", "schema_version: howlforge.persona/v1\nid: p\nrole: product\nmystery: 1\n", "field mystery not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := loaderFor(t, map[string]string{"personas/p.yaml": tt.content})
			_, err := Load(loader, "personas")
			if err == nil {
				t.Fatalf("Load accepted %s", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load returned %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRolesRejectsMissingRole(t *testing.T) {
	loader := loaderFor(t, map[string]string{
		"personas/p.yaml": "schema_version: howlforge.persona/v1\nid: p\nrole: nonexistent\n",
	})
	parsed, err := Load(loader, "personas")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	registry, _ := NewRegistry(parsed)
	err = registry.ValidateRoles(roleRegistry(t, "product"))
	if err == nil {
		t.Fatal("a persona pointing at an undefined role was accepted")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("ValidateRoles returned %q", err)
	}
}

func TestDuplicatePersonaIDs(t *testing.T) {
	loader := loaderFor(t, map[string]string{
		"personas/a.yaml": "schema_version: howlforge.persona/v1\nid: dup\nrole: product\n",
		"personas/b.yaml": "schema_version: howlforge.persona/v1\nid: dup\nrole: product\n",
	})
	parsed, _ := Load(loader, "personas")
	if _, err := NewRegistry(parsed); err == nil {
		t.Fatal("NewRegistry accepted two personas with the same id")
	}
}

func TestGetListsKnownPersonas(t *testing.T) {
	loader := loaderFor(t, map[string]string{
		"personas/a.yaml": "schema_version: howlforge.persona/v1\nid: rintaro\nrole: product\n",
	})
	parsed, _ := Load(loader, "personas")
	registry, _ := NewRegistry(parsed)

	_, err := registry.Get("rintarou")
	if err == nil {
		t.Fatal("Get accepted an undefined persona")
	}
	if !strings.Contains(err.Error(), "rintaro") {
		t.Fatalf("Get error %q does not list the known personas", err)
	}

	empty := Empty()
	if _, err := empty.Get("anything"); err == nil || !strings.Contains(err.Error(), "no personas are configured") {
		t.Fatalf("Get on an empty registry returned %v", err)
	}
}
