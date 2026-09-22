// Package personas provides an optional naming layer over roles.
//
// A persona is organizational identity and nothing more. It resolves to a role
// and stops there. It deliberately cannot name a provider, a model or a
// runtime, because the moment a persona could pin a model, the persona would
// become the thing that dies when that model does.
//
// HowlForge works completely without personas. The config/personas directory
// may be absent.
package personas

import (
	"fmt"
	"sort"
	"strings"

	"github.com/howlcipher/howlforge/internal/config"
	"github.com/howlcipher/howlforge/internal/roles"
)

// SchemaVersion is the only persona document version this build understands.
const SchemaVersion = "howlforge.persona/v1"

// Persona is a persistent organizational identity bound to a role.
type Persona struct {
	SchemaVersion string   `yaml:"schema_version" json:"schema_version"`
	ID            string   `yaml:"id" json:"id"`
	Role          string   `yaml:"role" json:"role"`
	DisplayName   string   `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	Description   string   `yaml:"description,omitempty" json:"description,omitempty"`
	StyleNotes    []string `yaml:"style_notes,omitempty" json:"style_notes,omitempty"`

	// SourcePath records the file the persona was loaded from.
	SourcePath string `yaml:"-" json:"source_path,omitempty"`
}

func (p *Persona) validateShape() error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version must be %q, got %q", SchemaVersion, p.SchemaVersion)
	}
	if err := roles.ValidateID(p.ID); err != nil {
		return fmt.Errorf("id: %w", err)
	}
	if err := roles.ValidateID(p.Role); err != nil {
		return fmt.Errorf("role: %w", err)
	}
	return nil
}

// Registry holds every loaded persona.
type Registry struct {
	byID map[string]*Persona
}

// NewRegistry builds a registry, refusing duplicate ids.
func NewRegistry(parsed []*Persona) (*Registry, error) {
	byID := make(map[string]*Persona, len(parsed))
	for _, persona := range parsed {
		if existing, exists := byID[persona.ID]; exists {
			return nil, fmt.Errorf("duplicate persona id %q defined in %s and %s",
				persona.ID, existing.SourcePath, persona.SourcePath)
		}
		byID[persona.ID] = persona
	}
	return &Registry{byID: byID}, nil
}

// Empty returns a registry with no personas.
func Empty() *Registry { return &Registry{byID: map[string]*Persona{}} }

// Get returns a persona by id.
func (r *Registry) Get(id string) (*Persona, error) {
	if r == nil {
		return nil, fmt.Errorf("persona %q is not defined", id)
	}
	persona, ok := r.byID[id]
	if !ok {
		known := r.IDs()
		if len(known) == 0 {
			return nil, fmt.Errorf("persona %q is not defined: no personas are configured", id)
		}
		return nil, fmt.Errorf("persona %q is not defined: known personas are %s", id, strings.Join(known, ", "))
	}
	return persona, nil
}

// Has reports whether a persona id is defined.
func (r *Registry) Has(id string) bool {
	if r == nil {
		return false
	}
	_, ok := r.byID[id]
	return ok
}

// IDs returns every persona id in lexical order.
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

// All returns every persona ordered by id.
func (r *Registry) All() []*Persona {
	ids := r.IDs()
	out := make([]*Persona, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.byID[id])
	}
	return out
}

// Len returns the number of configured personas.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.byID)
}

// ResolveRole maps a persona id to its role id.
func (r *Registry) ResolveRole(personaID string) (string, error) {
	persona, err := r.Get(personaID)
	if err != nil {
		return "", err
	}
	return persona.Role, nil
}

// ValidateRoles checks that every persona names a defined role.
func (r *Registry) ValidateRoles(roleRegistry *roles.Registry) error {
	for _, persona := range r.All() {
		if !roleRegistry.Has(persona.Role) {
			return fmt.Errorf("persona %q (%s) references unknown role %q",
				persona.ID, persona.SourcePath, persona.Role)
		}
	}
	return nil
}

// Load reads every persona document from relDir. A missing directory yields no
// personas rather than an error, which is what keeps personas optional.
func Load(loader *config.Loader, relDir string) ([]*Persona, error) {
	if !loader.Exists(relDir) {
		return nil, nil
	}
	paths, err := loader.ListFiles(relDir, ".yaml", ".yml")
	if err != nil {
		return nil, err
	}
	out := make([]*Persona, 0, len(paths))
	for _, path := range paths {
		persona, err := loadOne(loader, path)
		if err != nil {
			return nil, err
		}
		out = append(out, persona)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func loadOne(loader *config.Loader, path string) (*Persona, error) {
	data, err := loader.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var persona Persona
	if err := config.DecodeYAMLStrict(data, &persona); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	persona.SourcePath = path
	if err := persona.validateShape(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &persona, nil
}
