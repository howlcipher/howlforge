// Package runtimes defines the replaceable half of the workforce: the concrete
// execution environments a role may be filled by.
//
// Runtime definitions are strictly declarative. There is no command field, no
// argument template and no credential field, because HowlForge must never be
// able to execute a provider just because a configuration file described one.
package runtimes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/howlcipher/howlforge/internal/capabilities"
	"github.com/howlcipher/howlforge/internal/config"
)

// SchemaVersion is the only runtime document version this build understands.
const SchemaVersion = "howlforge.runtime/v1"

// EconomicClass describes how using a runtime is paid for. It is carried so
// HowlPlane can apply spend policy; HowlForge itself only ranks on it when the
// operator asks for a cost preference.
type EconomicClass string

const (
	// Subscription means the runtime draws on a flat-rate plan.
	Subscription EconomicClass = "subscription"
	// MeteredAPI means each call is separately billed.
	MeteredAPI EconomicClass = "metered_api"
	// Local means the runtime executes on operator hardware.
	Local EconomicClass = "local"
	// UnknownEconomics means the operator has not classified the runtime.
	UnknownEconomics EconomicClass = "unknown"
)

var economicClasses = []EconomicClass{Subscription, MeteredAPI, Local, UnknownEconomics}

// ParseEconomicClass validates an economic class name.
func ParseEconomicClass(s string) (EconomicClass, error) {
	normalized := EconomicClass(strings.ToLower(strings.TrimSpace(s)))
	for _, candidate := range economicClasses {
		if candidate == normalized {
			return candidate, nil
		}
	}
	names := make([]string, 0, len(economicClasses))
	for _, candidate := range economicClasses {
		names = append(names, string(candidate))
	}
	return UnknownEconomics, fmt.Errorf("unknown economic_class %q: expected one of %s",
		s, strings.Join(names, ", "))
}

// Locality describes where a runtime executes.
type Locality string

const (
	// Remote means execution leaves the operator's machine.
	Remote Locality = "remote"
	// OnDevice means execution stays on operator hardware.
	OnDevice Locality = "local"
	// UnknownLocality means the operator has not classified the runtime.
	UnknownLocality Locality = "unknown"
)

var localities = []Locality{Remote, OnDevice, UnknownLocality}

// ParseLocality validates a locality name.
func ParseLocality(s string) (Locality, error) {
	normalized := Locality(strings.ToLower(strings.TrimSpace(s)))
	for _, candidate := range localities {
		if candidate == normalized {
			return candidate, nil
		}
	}
	names := make([]string, 0, len(localities))
	for _, candidate := range localities {
		names = append(names, string(candidate))
	}
	return UnknownLocality, fmt.Errorf("unknown locality %q: expected one of %s", s, strings.Join(names, ", "))
}

// State is the operator's standing decision about a runtime. It is distinct
// from availability: enabled says the operator permits this runtime to be
// considered, while availability says whether it can be used right now.
type State struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Note    string `yaml:"note,omitempty" json:"note,omitempty"`
}

// Runtime is one configured execution resource.
//
// The four identity fields mirror HowlPlane's resource pool model so the two
// systems describe the same thing with the same words: ID is the operator
// configurable resource, Provider is the organization, Model is the served
// model, and Client is the tool used to reach it.
type Runtime struct {
	SchemaVersion string            `yaml:"schema_version" json:"schema_version"`
	ID            string            `yaml:"id" json:"id"`
	Description   string            `yaml:"description,omitempty" json:"description,omitempty"`
	Provider      string            `yaml:"provider" json:"provider"`
	Model         string            `yaml:"model" json:"model"`
	Client        string            `yaml:"runtime" json:"runtime"`
	Capabilities  capabilities.Set  `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	State         State             `yaml:"state" json:"state"`
	EconomicClass EconomicClass     `yaml:"economic_class,omitempty" json:"economic_class,omitempty"`
	Locality      Locality          `yaml:"locality,omitempty" json:"locality,omitempty"`
	Tags          []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
	Metadata      map[string]string `yaml:"metadata,omitempty" json:"metadata,omitempty"`

	// SourcePath records the file the runtime was loaded from.
	SourcePath string `yaml:"-" json:"source_path,omitempty"`
}

// Label renders the runtime the way the CLI presents a candidate.
func (r *Runtime) Label() string {
	return fmt.Sprintf("%s / %s", r.Client, r.Model)
}

// MatchesRef reports whether this runtime satisfies a role's runtime selector.
// Every non-empty field must match exactly; empty fields are wildcards. Taking
// primitives rather than a roles type keeps runtimes free of a dependency on
// roles, so the two packages stay independently testable.
func (r *Runtime) MatchesRef(id, provider, model, client string) bool {
	if id != "" && !strings.EqualFold(id, r.ID) {
		return false
	}
	if provider != "" && !strings.EqualFold(provider, r.Provider) {
		return false
	}
	if model != "" && !strings.EqualFold(model, r.Model) {
		return false
	}
	if client != "" && !strings.EqualFold(client, r.Client) {
		return false
	}
	return true
}

// validateShape checks a single runtime document in isolation.
func (r *Runtime) validateShape() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version must be %q, got %q", SchemaVersion, r.SchemaVersion)
	}
	if err := validateID(r.ID); err != nil {
		return fmt.Errorf("id: %w", err)
	}
	for _, field := range []struct{ name, value string }{
		{"provider", r.Provider},
		{"model", r.Model},
		{"runtime", r.Client},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("runtime %q: %s must not be empty", r.ID, field.name)
		}
		if len(field.value) > 128 {
			return fmt.Errorf("runtime %q: %s is longer than 128 characters", r.ID, field.name)
		}
	}
	if r.EconomicClass != "" {
		if _, err := ParseEconomicClass(string(r.EconomicClass)); err != nil {
			return fmt.Errorf("runtime %q: %w", r.ID, err)
		}
	}
	if r.Locality != "" {
		if _, err := ParseLocality(string(r.Locality)); err != nil {
			return fmt.Errorf("runtime %q: %w", r.ID, err)
		}
	}
	for _, level := range r.Capabilities {
		if !level.Valid() {
			return fmt.Errorf("runtime %q declares an invalid capability level", r.ID)
		}
	}
	return nil
}

// validateID mirrors the shared identifier rule. It is duplicated rather than
// imported to keep runtimes independent of roles; the shape is asserted
// identical by test.
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

// Registry holds every configured runtime.
type Registry struct {
	byID map[string]*Runtime
}

// NewRegistry builds a registry, refusing duplicate ids.
func NewRegistry(parsed []*Runtime) (*Registry, error) {
	byID := make(map[string]*Runtime, len(parsed))
	for _, rt := range parsed {
		if existing, exists := byID[rt.ID]; exists {
			return nil, fmt.Errorf("duplicate runtime id %q defined in %s and %s",
				rt.ID, existing.SourcePath, rt.SourcePath)
		}
		byID[rt.ID] = rt
	}
	return &Registry{byID: byID}, nil
}

// Get returns a runtime by id.
func (r *Registry) Get(id string) (*Runtime, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime %q is not defined", id)
	}
	rt, ok := r.byID[id]
	if !ok {
		known := r.IDs()
		if len(known) == 0 {
			return nil, fmt.Errorf("runtime %q is not defined: no runtimes are configured", id)
		}
		return nil, fmt.Errorf("runtime %q is not defined: known runtimes are %s", id, strings.Join(known, ", "))
	}
	return rt, nil
}

// Has reports whether a runtime id is defined.
func (r *Registry) Has(id string) bool {
	if r == nil {
		return false
	}
	_, ok := r.byID[id]
	return ok
}

// IDs returns every runtime id in lexical order.
func (r *Registry) IDs() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.byID))
	for id := range r.byID {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// All returns every runtime ordered by id, which is what makes downstream
// iteration deterministic regardless of map ordering.
func (r *Registry) All() []*Runtime {
	ids := r.IDs()
	out := make([]*Runtime, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.byID[id])
	}
	return out
}

// Len returns the number of configured runtimes.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.byID)
}

// Enabled returns only the runtimes the operator has enabled.
func (r *Registry) Enabled() []*Runtime {
	var out []*Runtime
	for _, rt := range r.All() {
		if rt.State.Enabled {
			out = append(out, rt)
		}
	}
	return out
}

// MatchRef returns every runtime satisfying a selector, in id order.
func (r *Registry) MatchRef(id, provider, model, client string) []*Runtime {
	var out []*Runtime
	for _, rt := range r.All() {
		if rt.MatchesRef(id, provider, model, client) {
			out = append(out, rt)
		}
	}
	return out
}

// ValidateCapabilities checks every runtime's declared capabilities against the
// controlled vocabulary.
func (r *Registry) ValidateCapabilities(vocab *capabilities.Vocabulary) error {
	for _, rt := range r.All() {
		if err := vocab.Validate(rt.Capabilities); err != nil {
			return fmt.Errorf("runtime %q (%s): %w", rt.ID, rt.SourcePath, err)
		}
	}
	return nil
}

// Load reads every runtime document from relDir under the loader's root.
func Load(loader *config.Loader, relDir string) ([]*Runtime, error) {
	paths, err := loader.ListFiles(relDir, ".yaml", ".yml")
	if err != nil {
		return nil, err
	}
	out := make([]*Runtime, 0, len(paths))
	for _, path := range paths {
		rt, err := loadOne(loader, path)
		if err != nil {
			return nil, err
		}
		out = append(out, rt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func loadOne(loader *config.Loader, path string) (*Runtime, error) {
	data, err := loader.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rt Runtime
	if err := config.DecodeYAMLStrict(data, &rt); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	rt.SourcePath = path
	if err := rt.validateShape(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &rt, nil
}
