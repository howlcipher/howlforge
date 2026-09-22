package roles

import (
	"fmt"
	"sort"
	"strings"

	"github.com/howlcipher/howlforge/internal/capabilities"
	"github.com/howlcipher/howlforge/internal/config"
)

// Registry holds every loaded role, with inheritance already resolved.
type Registry struct {
	byID map[string]*Role
}

// NewRegistry resolves inheritance across a set of parsed roles and returns a
// registry. Duplicate ids, unknown parents and inheritance cycles are refused
// here, where all documents are visible at once.
func NewRegistry(parsed []*Role) (*Registry, error) {
	byID := make(map[string]*Role, len(parsed))
	for _, role := range parsed {
		if existing, exists := byID[role.ID]; exists {
			return nil, fmt.Errorf("duplicate role id %q defined in %s and %s",
				role.ID, existing.SourcePath, role.SourcePath)
		}
		byID[role.ID] = role
	}
	for _, id := range sortedIDs(byID) {
		if err := resolve(byID, id, nil); err != nil {
			return nil, err
		}
	}
	return &Registry{byID: byID}, nil
}

// resolve applies inheritance to one role, detecting cycles via the visiting
// chain. The chain doubles as the diagnostic so an operator sees the whole
// loop rather than just the last hop.
func resolve(byID map[string]*Role, id string, chain []string) error {
	role, ok := byID[id]
	if !ok {
		return fmt.Errorf("role %q is not defined", id)
	}
	if role.Extends == "" {
		return nil
	}
	for _, seen := range chain {
		if seen == id {
			return fmt.Errorf("circular role inheritance: %s", strings.Join(append(chain, id), " -> "))
		}
	}
	parent, ok := byID[role.Extends]
	if !ok {
		return fmt.Errorf("role %q (%s) extends unknown role %q", role.ID, role.SourcePath, role.Extends)
	}
	if err := resolve(byID, parent.ID, append(chain, id)); err != nil {
		return err
	}
	role.mergeFrom(parent)
	// Clearing Extends marks the role resolved so a diamond does not merge the
	// same parent twice.
	role.Extends = ""
	return nil
}

// Get returns a role by id.
func (r *Registry) Get(id string) (*Role, error) {
	if r == nil {
		return nil, fmt.Errorf("role %q is not defined", id)
	}
	role, ok := r.byID[id]
	if !ok {
		known := r.IDs()
		if len(known) == 0 {
			return nil, fmt.Errorf("role %q is not defined: no roles are configured", id)
		}
		return nil, fmt.Errorf("role %q is not defined: known roles are %s", id, strings.Join(known, ", "))
	}
	return role, nil
}

// Has reports whether a role id is defined.
func (r *Registry) Has(id string) bool {
	if r == nil {
		return false
	}
	_, ok := r.byID[id]
	return ok
}

// IDs returns every role id in lexical order.
func (r *Registry) IDs() []string {
	if r == nil {
		return nil
	}
	return sortedIDs(r.byID)
}

// All returns every role ordered by id.
func (r *Registry) All() []*Role {
	ids := r.IDs()
	out := make([]*Role, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.byID[id])
	}
	return out
}

// Len returns the number of defined roles.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.byID)
}

// ValidateCapabilities checks every role's requirements against the controlled
// vocabulary.
func (r *Registry) ValidateCapabilities(vocab *capabilities.Vocabulary) error {
	for _, role := range r.All() {
		if err := vocab.Validate(role.RequiredCapabilities); err != nil {
			return fmt.Errorf("role %q (%s): %w", role.ID, role.SourcePath, err)
		}
	}
	return nil
}

// ValidateVerifiers checks that every declared verifier_role exists and that no
// role is its own verifier, which would defeat the point of independent
// verification.
func (r *Registry) ValidateVerifiers() error {
	for _, role := range r.All() {
		verifier := role.VerifierRole()
		if verifier == "" {
			continue
		}
		if !r.Has(verifier) {
			return fmt.Errorf("role %q (%s) names unknown verifier_role %q", role.ID, role.SourcePath, verifier)
		}
		if verifier == role.ID {
			return fmt.Errorf("role %q names itself as its own verifier_role; a worker must not self certify", role.ID)
		}
	}
	return nil
}

// Load reads every role document from relDir under the loader's root.
func Load(loader *config.Loader, relDir string) ([]*Role, error) {
	paths, err := loader.ListFiles(relDir, ".yaml", ".yml")
	if err != nil {
		return nil, err
	}
	out := make([]*Role, 0, len(paths))
	for _, path := range paths {
		role, err := loadOne(loader, path)
		if err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func loadOne(loader *config.Loader, path string) (*Role, error) {
	data, err := loader.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var role Role
	if err := config.DecodeYAMLStrict(data, &role); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	role.SourcePath = path
	if err := role.validateShape(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &role, nil
}
