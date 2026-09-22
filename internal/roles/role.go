// Package roles defines the durable half of the workforce: what jobs exist and
// what those jobs require.
//
// A role never names a model as an identity. It names capability requirements
// and an ordered preference over runtime selectors. That indirection is the
// whole point: architect is a role, not a synonym for a particular model.
package roles

import (
	"fmt"
	"sort"
	"strings"

	"github.com/howlcipher/howlforge/internal/capabilities"
)

// SchemaVersion is the only role document version this build understands.
const SchemaVersion = "howlforge.role/v1"

// RuntimeRef selects one or more registered runtimes. Every field that is set
// must match; fields left empty are wildcards. A role therefore expresses
// "GPT-5.6 Sol wherever it is hosted" as provider plus model, or pins one
// specific resource by id.
type RuntimeRef struct {
	ID       string `yaml:"id,omitempty" json:"id,omitempty"`
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
	Model    string `yaml:"model,omitempty" json:"model,omitempty"`
	Runtime  string `yaml:"runtime,omitempty" json:"runtime,omitempty"`
}

// IsEmpty reports whether the reference constrains nothing. An empty reference
// would match every runtime, which is never what an author meant, so loaders
// reject it.
func (r RuntimeRef) IsEmpty() bool {
	return r.ID == "" && r.Provider == "" && r.Model == "" && r.Runtime == ""
}

// String renders the reference for diagnostics and explanation output.
func (r RuntimeRef) String() string {
	var parts []string
	if r.ID != "" {
		parts = append(parts, "id="+r.ID)
	}
	if r.Provider != "" {
		parts = append(parts, "provider="+r.Provider)
	}
	if r.Model != "" {
		parts = append(parts, "model="+r.Model)
	}
	if r.Runtime != "" {
		parts = append(parts, "runtime="+r.Runtime)
	}
	if len(parts) == 0 {
		return "<empty>"
	}
	return strings.Join(parts, " ")
}

// Verification records what HowlProof must arrange for work this role produces.
// HowlForge only states the requirement; it never performs verification.
type Verification struct {
	Required           bool   `yaml:"required" json:"required"`
	VerifierRole       string `yaml:"verifier_role,omitempty" json:"verifier_role,omitempty"`
	IndependentRuntime string `yaml:"independent_runtime,omitempty" json:"independent_runtime,omitempty"`
}

// IndependenceRequired reports whether the verifying runtime must differ from
// the producing runtime. "required" is a hard constraint; "preferred" is a
// ranking preference that never makes a role unfillable.
func (v *Verification) IndependenceRequired() bool {
	return v != nil && strings.EqualFold(v.IndependentRuntime, "required")
}

// IndependencePreferred reports whether a different verifying runtime is
// preferred where one is available.
func (v *Verification) IndependencePreferred() bool {
	return v != nil && strings.EqualFold(v.IndependentRuntime, "preferred")
}

// Role is a workforce position.
type Role struct {
	SchemaVersion        string            `yaml:"schema_version" json:"schema_version"`
	ID                   string            `yaml:"id" json:"id"`
	Description          string            `yaml:"description" json:"description"`
	Extends              string            `yaml:"extends,omitempty" json:"extends,omitempty"`
	Responsibilities     []string          `yaml:"responsibilities,omitempty" json:"responsibilities,omitempty"`
	RequiredCapabilities capabilities.Set  `yaml:"required_capabilities,omitempty" json:"required_capabilities,omitempty"`
	PreferredRuntimes    []RuntimeRef      `yaml:"preferred_runtimes,omitempty" json:"preferred_runtimes,omitempty"`
	FallbackRuntimes     []RuntimeRef      `yaml:"fallback_runtimes,omitempty" json:"fallback_runtimes,omitempty"`
	ExcludedRuntimes     []RuntimeRef      `yaml:"excluded_runtimes,omitempty" json:"excluded_runtimes,omitempty"`
	HandoffRequired      bool              `yaml:"handoff_required" json:"handoff_required"`
	VerificationRequired bool              `yaml:"verification_required" json:"verification_required"`
	Verification         *Verification     `yaml:"verification,omitempty" json:"verification,omitempty"`
	Metadata             map[string]string `yaml:"metadata,omitempty" json:"metadata,omitempty"`

	// SourcePath records the file the role was loaded from, for diagnostics.
	SourcePath string `yaml:"-" json:"source_path,omitempty"`
}

// validateShape checks a single role document in isolation. Cross-document
// checks (inheritance cycles, runtime references) belong to the registry,
// which is the only place that can see the other documents.
func (r *Role) validateShape() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version must be %q, got %q", SchemaVersion, r.SchemaVersion)
	}
	if err := validateID(r.ID); err != nil {
		return fmt.Errorf("id: %w", err)
	}
	if r.Extends != "" {
		if err := validateID(r.Extends); err != nil {
			return fmt.Errorf("extends: %w", err)
		}
		if r.Extends == r.ID {
			return fmt.Errorf("role %q extends itself", r.ID)
		}
	}
	// A base role in an inheritance chain may legitimately omit a description,
	// but a role that stands alone should say what it is for.
	if strings.TrimSpace(r.Description) == "" && r.Extends == "" {
		return fmt.Errorf("role %q has an empty description", r.ID)
	}
	for _, list := range []struct {
		name string
		refs []RuntimeRef
	}{
		{"preferred_runtimes", r.PreferredRuntimes},
		{"fallback_runtimes", r.FallbackRuntimes},
		{"excluded_runtimes", r.ExcludedRuntimes},
	} {
		for i, ref := range list.refs {
			if ref.IsEmpty() {
				return fmt.Errorf("%s[%d] selects nothing: set at least one of id, provider, model, runtime", list.name, i)
			}
		}
	}
	for _, level := range r.RequiredCapabilities {
		if !level.Valid() {
			return fmt.Errorf("role %q declares an invalid capability level", r.ID)
		}
	}
	if r.Verification != nil && r.Verification.IndependentRuntime != "" {
		switch strings.ToLower(r.Verification.IndependentRuntime) {
		case "required", "preferred", "none":
		default:
			return fmt.Errorf("verification.independent_runtime must be required, preferred or none, got %q",
				r.Verification.IndependentRuntime)
		}
	}
	return nil
}

// validateID enforces the shared identifier shape used by roles, runtimes and
// personas: lower_snake_case or kebab-case, no path separators, no spaces.
func validateID(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("must not be empty")
	}
	if len(id) > 64 {
		return fmt.Errorf("%q is longer than 64 characters", id)
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return fmt.Errorf("%q must use lowercase letters, digits, underscore or hyphen", id)
		}
	}
	return nil
}

// ValidateID exposes the shared identifier rule to sibling packages.
func ValidateID(id string) error { return validateID(id) }

// mergeFrom applies inherited values from a parent role. The child always wins
// where it declares something; inheritance fills gaps, it never overrides.
func (r *Role) mergeFrom(parent *Role) {
	if strings.TrimSpace(r.Description) == "" {
		r.Description = parent.Description
	}
	if len(r.Responsibilities) == 0 {
		r.Responsibilities = append([]string(nil), parent.Responsibilities...)
	}
	if len(r.PreferredRuntimes) == 0 {
		r.PreferredRuntimes = append([]RuntimeRef(nil), parent.PreferredRuntimes...)
	}
	if len(r.FallbackRuntimes) == 0 {
		r.FallbackRuntimes = append([]RuntimeRef(nil), parent.FallbackRuntimes...)
	}
	if len(r.ExcludedRuntimes) == 0 {
		r.ExcludedRuntimes = append([]RuntimeRef(nil), parent.ExcludedRuntimes...)
	}
	if r.Verification == nil && parent.Verification != nil {
		copied := *parent.Verification
		r.Verification = &copied
	}
	// Capability requirements merge per key so a child can raise one bar
	// without restating the whole profile.
	merged := parent.RequiredCapabilities.Clone()
	if merged == nil {
		merged = capabilities.Set{}
	}
	for name, level := range r.RequiredCapabilities {
		merged[name] = level
	}
	if len(merged) > 0 {
		r.RequiredCapabilities = merged
	}
	if r.Metadata == nil && parent.Metadata != nil {
		r.Metadata = make(map[string]string, len(parent.Metadata))
		for k, v := range parent.Metadata {
			r.Metadata[k] = v
		}
	}
}

// RequiresVerification reports whether work by this role must be verified by
// someone else. The dedicated verification block wins when present.
func (r *Role) RequiresVerification() bool {
	if r.Verification != nil {
		return r.Verification.Required
	}
	return r.VerificationRequired
}

// VerifierRole returns the role that must verify this role's output, or empty
// when none is declared.
func (r *Role) VerifierRole() string {
	if r.Verification != nil {
		return r.Verification.VerifierRole
	}
	return ""
}

// CapabilityNames returns the required capability names in lexical order.
func (r *Role) CapabilityNames() []string {
	return r.RequiredCapabilities.Names()
}

// sortedIDs is a small helper shared by diagnostics.
func sortedIDs[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
