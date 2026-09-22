package roles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/howlcipher/howlforge/internal/capabilities"
	"github.com/howlcipher/howlforge/internal/config"
)

// loaderFor writes the given files into a temporary root and returns a loader.
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

const validRole = `schema_version: howlforge.role/v1
id: implementer
description: Writes code.
responsibilities:
  - implement
required_capabilities:
  coding: very_high
  testing: high
preferred_runtimes:
  - provider: cursor
    model: composer
fallback_runtimes:
  - id: claude-code-opus
handoff_required: true
verification_required: true
verification:
  required: true
  verifier_role: reviewer
  independent_runtime: required
`

func TestLoadValidRole(t *testing.T) {
	loader := loaderFor(t, map[string]string{"roles/implementer.yaml": validRole})
	parsed, err := Load(loader, "roles")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("loaded %d roles, want 1", len(parsed))
	}
	role := parsed[0]
	if role.ID != "implementer" {
		t.Fatalf("id = %q", role.ID)
	}
	if role.RequiredCapabilities[capabilities.Coding] != capabilities.LevelVeryHigh {
		t.Fatalf("coding requirement = %v", role.RequiredCapabilities[capabilities.Coding])
	}
	if !role.RequiresVerification() || role.VerifierRole() != "reviewer" {
		t.Fatal("verification block was not applied")
	}
	if !role.Verification.IndependenceRequired() {
		t.Fatal("independent_runtime: required was not recognized")
	}
	if role.SourcePath != "roles/implementer.yaml" {
		t.Fatalf("source path = %q", role.SourcePath)
	}
}

func TestLoadRejectsMalformedRoles(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "wrong schema version",
			content: "schema_version: howlforge.role/v2\nid: a\ndescription: x\n",
			wantErr: "schema_version",
		},
		{
			name:    "missing schema version",
			content: "id: a\ndescription: x\n",
			wantErr: "schema_version",
		},
		{
			name:    "unknown field",
			content: "schema_version: howlforge.role/v1\nid: a\ndescription: x\nmystery: 1\n",
			wantErr: "field mystery not found",
		},
		{
			name:    "invalid capability level",
			content: "schema_version: howlforge.role/v1\nid: a\ndescription: x\nrequired_capabilities:\n  coding: extreme\n",
			wantErr: "unknown capability level",
		},
		{
			name:    "uppercase id",
			content: "schema_version: howlforge.role/v1\nid: Architect\ndescription: x\n",
			wantErr: "lowercase",
		},
		{
			name:    "id with a path separator",
			content: "schema_version: howlforge.role/v1\nid: a/b\ndescription: x\n",
			wantErr: "lowercase",
		},
		{
			name:    "empty description with no parent",
			content: "schema_version: howlforge.role/v1\nid: a\ndescription: \"\"\n",
			wantErr: "empty description",
		},
		{
			name:    "empty runtime selector",
			content: "schema_version: howlforge.role/v1\nid: a\ndescription: x\npreferred_runtimes:\n  - {}\n",
			wantErr: "selects nothing",
		},
		{
			name:    "self extension",
			content: "schema_version: howlforge.role/v1\nid: a\ndescription: x\nextends: a\n",
			wantErr: "extends itself",
		},
		{
			name:    "bad independent_runtime",
			content: "schema_version: howlforge.role/v1\nid: a\ndescription: x\nverification:\n  required: true\n  independent_runtime: maybe\n",
			wantErr: "independent_runtime",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := loaderFor(t, map[string]string{"roles/role.yaml": tt.content})
			_, err := Load(loader, "roles")
			if err == nil {
				t.Fatalf("Load accepted %s", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load returned %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestRegistryRejectsDuplicateIDs(t *testing.T) {
	loader := loaderFor(t, map[string]string{
		"roles/a.yaml": "schema_version: howlforge.role/v1\nid: dup\ndescription: first\n",
		"roles/b.yaml": "schema_version: howlforge.role/v1\nid: dup\ndescription: second\n",
	})
	parsed, err := Load(loader, "roles")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = NewRegistry(parsed)
	if err == nil {
		t.Fatal("NewRegistry accepted two roles with the same id")
	}
	if !strings.Contains(err.Error(), "duplicate role id") {
		t.Fatalf("NewRegistry returned %q", err)
	}
	// The diagnostic must name both files so the operator knows where to look.
	if !strings.Contains(err.Error(), "roles/a.yaml") || !strings.Contains(err.Error(), "roles/b.yaml") {
		t.Fatalf("NewRegistry error %q does not name both source files", err)
	}
}

func TestInheritance(t *testing.T) {
	loader := loaderFor(t, map[string]string{
		"roles/base.yaml": `schema_version: howlforge.role/v1
id: base
description: A base role.
responsibilities:
  - inherited_responsibility
required_capabilities:
  reasoning: high
  coding: medium
preferred_runtimes:
  - id: claude-code-opus
verification:
  required: true
  verifier_role: reviewer
`,
		"roles/child.yaml": `schema_version: howlforge.role/v1
id: child
extends: base
description: A child role.
required_capabilities:
  coding: very_high
`,
	})
	parsed, err := Load(loader, "roles")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	registry, err := NewRegistry(parsed)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	child, err := registry.Get("child")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// Inheritance fills gaps; the child always wins where it declares.
	if got := child.RequiredCapabilities[capabilities.Coding]; got != capabilities.LevelVeryHigh {
		t.Fatalf("child coding = %v, want the child's own very_high", got)
	}
	if got := child.RequiredCapabilities[capabilities.Reasoning]; got != capabilities.LevelHigh {
		t.Fatalf("child reasoning = %v, want the inherited high", got)
	}
	if len(child.Responsibilities) != 1 || child.Responsibilities[0] != "inherited_responsibility" {
		t.Fatalf("responsibilities were not inherited: %v", child.Responsibilities)
	}
	if len(child.PreferredRuntimes) != 1 {
		t.Fatalf("preferred runtimes were not inherited: %v", child.PreferredRuntimes)
	}
	if child.VerifierRole() != "reviewer" {
		t.Fatal("the verification block was not inherited")
	}
	if child.Description != "A child role." {
		t.Fatalf("the child description was overwritten: %q", child.Description)
	}
	// The base must be untouched by the merge.
	base, _ := registry.Get("base")
	if got := base.RequiredCapabilities[capabilities.Coding]; got != capabilities.LevelMedium {
		t.Fatalf("the base role was mutated by inheritance: coding = %v", got)
	}
}

func TestCircularInheritanceIsRefused(t *testing.T) {
	tests := map[string]map[string]string{
		"two role cycle": {
			"roles/a.yaml": "schema_version: howlforge.role/v1\nid: a\ndescription: x\nextends: b\n",
			"roles/b.yaml": "schema_version: howlforge.role/v1\nid: b\ndescription: x\nextends: a\n",
		},
		"three role cycle": {
			"roles/a.yaml": "schema_version: howlforge.role/v1\nid: a\ndescription: x\nextends: b\n",
			"roles/b.yaml": "schema_version: howlforge.role/v1\nid: b\ndescription: x\nextends: c\n",
			"roles/c.yaml": "schema_version: howlforge.role/v1\nid: c\ndescription: x\nextends: a\n",
		},
	}
	for name, files := range tests {
		t.Run(name, func(t *testing.T) {
			loader := loaderFor(t, files)
			parsed, err := Load(loader, "roles")
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			_, err = NewRegistry(parsed)
			if err == nil {
				t.Fatal("NewRegistry accepted a circular inheritance chain")
			}
			if !strings.Contains(err.Error(), "circular role inheritance") {
				t.Fatalf("NewRegistry returned %q", err)
			}
		})
	}
}

func TestUnknownParentIsRefused(t *testing.T) {
	loader := loaderFor(t, map[string]string{
		"roles/a.yaml": "schema_version: howlforge.role/v1\nid: a\ndescription: x\nextends: nowhere\n",
	})
	parsed, _ := Load(loader, "roles")
	if _, err := NewRegistry(parsed); err == nil {
		t.Fatal("NewRegistry accepted a role extending an undefined parent")
	}
}

func TestRuntimeRefString(t *testing.T) {
	tests := []struct {
		ref  RuntimeRef
		want string
	}{
		{RuntimeRef{ID: "cursor-sol"}, "id=cursor-sol"},
		{RuntimeRef{Provider: "openai", Model: "gpt-5.6-sol"}, "provider=openai model=gpt-5.6-sol"},
		{RuntimeRef{}, "<empty>"},
	}
	for _, tt := range tests {
		if got := tt.ref.String(); got != tt.want {
			t.Errorf("RuntimeRef.String() = %q, want %q", got, tt.want)
		}
	}
}

func TestGetReportsKnownRoles(t *testing.T) {
	loader := loaderFor(t, map[string]string{"roles/a.yaml": "schema_version: howlforge.role/v1\nid: architect\ndescription: x\n"})
	parsed, _ := Load(loader, "roles")
	registry, _ := NewRegistry(parsed)

	_, err := registry.Get("architekt")
	if err == nil {
		t.Fatal("Get accepted an undefined role")
	}
	if !strings.Contains(err.Error(), "architect") {
		t.Fatalf("Get error %q does not list the known roles", err)
	}
}

func TestValidateID(t *testing.T) {
	valid := []string{"foreman", "claude-code-opus", "agent_1", "a"}
	for _, id := range valid {
		if err := ValidateID(id); err != nil {
			t.Errorf("ValidateID(%q) = %v, want nil", id, err)
		}
	}
	invalid := []string{"", "   ", "Foreman", "fore man", "fore/man", "fore.man", strings.Repeat("a", 65)}
	for _, id := range invalid {
		if err := ValidateID(id); err == nil {
			t.Errorf("ValidateID(%q) accepted an invalid id", id)
		}
	}
}
