// Package howlforge is the public library surface of HowlForge.
//
// HowlForge defines an AI workforce and selects candidates to fill its roles.
// It does not orchestrate, launch, retry or route anything. Selection is
// separated from execution on purpose: HowlPlane asks who is eligible,
// HowlForge answers and explains, and HowlPlane decides what to run.
//
// The surface is deliberately small. Three interfaces cover everything a
// consumer needs, and none of them can cause a provider to be invoked.
package howlforge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/howlcipher/howlforge/internal/availability"
	"github.com/howlcipher/howlforge/internal/capabilities"
	"github.com/howlcipher/howlforge/internal/config"
	"github.com/howlcipher/howlforge/internal/matching"
	"github.com/howlcipher/howlforge/internal/personas"
	"github.com/howlcipher/howlforge/internal/roles"
	"github.com/howlcipher/howlforge/internal/runtimes"
)

// Version is the HowlForge release this build implements.
const Version = "0.1.0"

// Published types. These are aliases rather than parallel definitions so the
// library and the implementation cannot drift apart; the alias is the stable
// name and the underlying package is an implementation detail.
type (
	// Level is an ordinal capability rating.
	Level = capabilities.Level
	// CapabilitySet maps capability names to levels.
	CapabilitySet = capabilities.Set
	// Hint is a soft ranking preference.
	Hint = matching.Hint
	// Candidate is an eligible, ranked worker.
	Candidate = matching.Candidate
	// Rejection explains why a runtime was not offered.
	Rejection = matching.Rejection
	// MatchResult is a full matching answer.
	MatchResult = matching.Result
	// Explanation accounts for one role and runtime pairing.
	Explanation = matching.Explanation
	// AvailabilityResult reports who can take a role right now.
	AvailabilityResult = matching.AvailabilityResult
	// Role is a workforce position.
	Role = roles.Role
	// RuntimeRef is a role's selector over configured runtimes.
	RuntimeRef = roles.RuntimeRef
	// Runtime is a configured execution resource.
	Runtime = runtimes.Runtime
	// Persona is an optional organizational identity.
	Persona = personas.Persona
	// State is an availability condition.
	State = availability.State
)

// Re-exported helpers so consumers do not need the internal packages.
var (
	// ParseLevel converts a capability level name into a Level.
	ParseLevel = capabilities.ParseLevel
	// ParseHint validates a preference hint name.
	ParseHint = matching.ParseHint
	// ParseHints validates a list of preference hint names.
	ParseHints = matching.ParseHints
	// ParseState validates an availability state name.
	ParseState = availability.ParseState
	// LevelNames returns the capability scale in ascending order.
	LevelNames = capabilities.LevelNames
	// CapabilityNames returns the built-in capability vocabulary.
	CapabilityNames = capabilities.BuiltinNames
	// AvailabilityStates returns every defined availability state.
	AvailabilityStates = availability.States
	// PreferenceHints returns every defined ranking hint.
	PreferenceHints = matching.Hints
)

// MatchRequest asks for candidates to fill a role.
type MatchRequest struct {
	// Role is the role id to fill. Exactly one of Role or Persona is required.
	Role string
	// Persona optionally names a persona, which resolves to a role. A persona
	// never selects a runtime.
	Persona string
	// Require adds capability floors on top of the role's own requirements.
	Require CapabilitySet
	// Prefer lists soft ranking hints.
	Prefer []Hint
	// Exclude lists runtime ids that must not be offered, which is how a
	// caller asks for a rematch that skips a worker it just lost.
	Exclude []string
	// Now fixes the evaluation time. Leave zero to use the wall clock.
	Now time.Time
}

// Matcher returns eligible workers for a role, in preference order.
type Matcher interface {
	Match(ctx context.Context, req MatchRequest) ([]Candidate, error)
}

// Explainer accounts for a single role and runtime pairing.
type Explainer interface {
	Explain(ctx context.Context, req MatchRequest, runtimeID string) (*Explanation, error)
}

// Validator checks that the loaded configuration is coherent.
type Validator interface {
	Validate(ctx context.Context) (*ValidationReport, error)
}

// Options configures loading.
type Options struct {
	// Root is the configuration directory holding roles, runtimes and
	// personas. Empty means DefaultRoot.
	Root string
	// StatePath is the availability state file. Empty means the default
	// location; a missing file yields an empty snapshot.
	StatePath string
	// RolesDir, RuntimesDir and PersonasDir override the subdirectory names.
	RolesDir    string
	RuntimesDir string
	PersonasDir string
}

// Directory names inside a configuration root.
const (
	DefaultRolesDir    = "roles"
	DefaultRuntimesDir = "runtimes"
	DefaultPersonasDir = "personas"
	// CapabilitiesFile optionally extends the controlled vocabulary.
	CapabilitiesFile = "capabilities.yaml"
	// EnvConfigRoot names the configuration root environment variable.
	EnvConfigRoot = "HOWLFORGE_CONFIG"
)

// Forge is a loaded workforce definition.
type Forge struct {
	root     string
	roles    *roles.Registry
	runtimes *runtimes.Registry
	personas *personas.Registry
	state    *availability.Snapshot
	vocab    *capabilities.Vocabulary
	matcher  *matching.Matcher
}

// Compile-time proof that Forge satisfies the published interfaces.
var (
	_ Matcher   = (*Forge)(nil)
	_ Explainer = (*Forge)(nil)
	_ Validator = (*Forge)(nil)
)

// DefaultRoot resolves the configuration directory: an explicit value first,
// then the environment, then a config directory beside the working directory,
// then the user configuration directory.
func DefaultRoot(explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	if fromEnv := strings.TrimSpace(os.Getenv(EnvConfigRoot)); fromEnv != "" {
		return fromEnv
	}
	if info, err := os.Stat("config"); err == nil && info.IsDir() {
		return "config"
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "howlforge")
	}
	return "config"
}

// Load reads a workforce definition from disk.
func Load(opts Options) (*Forge, error) {
	root := DefaultRoot(opts.Root)
	loader, err := config.NewLoader(root)
	if err != nil {
		return nil, err
	}

	vocab, err := loadVocabulary(loader)
	if err != nil {
		return nil, err
	}

	rolesDir := orDefault(opts.RolesDir, DefaultRolesDir)
	runtimesDir := orDefault(opts.RuntimesDir, DefaultRuntimesDir)
	personasDir := orDefault(opts.PersonasDir, DefaultPersonasDir)

	parsedRuntimes, err := runtimes.Load(loader, runtimesDir)
	if err != nil {
		return nil, fmt.Errorf("load runtimes: %w", err)
	}
	runtimeRegistry, err := runtimes.NewRegistry(parsedRuntimes)
	if err != nil {
		return nil, fmt.Errorf("load runtimes: %w", err)
	}

	parsedRoles, err := roles.Load(loader, rolesDir)
	if err != nil {
		return nil, fmt.Errorf("load roles: %w", err)
	}
	roleRegistry, err := roles.NewRegistry(parsedRoles)
	if err != nil {
		return nil, fmt.Errorf("load roles: %w", err)
	}

	parsedPersonas, err := personas.Load(loader, personasDir)
	if err != nil {
		return nil, fmt.Errorf("load personas: %w", err)
	}
	personaRegistry, err := personas.NewRegistry(parsedPersonas)
	if err != nil {
		return nil, fmt.Errorf("load personas: %w", err)
	}

	snapshot, err := availability.LoadFile(availability.DefaultStatePath(opts.StatePath))
	if err != nil {
		return nil, err
	}

	return &Forge{
		root:     loader.Root(),
		roles:    roleRegistry,
		runtimes: runtimeRegistry,
		personas: personaRegistry,
		state:    snapshot,
		vocab:    vocab,
		matcher:  matching.NewMatcher(roleRegistry, runtimeRegistry, snapshot),
	}, nil
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// vocabularyDocument is the optional capability vocabulary extension file.
type vocabularyDocument struct {
	SchemaVersion string   `yaml:"schema_version"`
	Capabilities  []string `yaml:"capabilities"`
}

// VocabularySchemaVersion is the capability extension document version.
const VocabularySchemaVersion = "howlforge.capabilities/v1"

func loadVocabulary(loader *config.Loader) (*capabilities.Vocabulary, error) {
	vocab := capabilities.DefaultVocabulary()
	if !loader.Exists(CapabilitiesFile) {
		return vocab, nil
	}
	data, err := loader.ReadFile(CapabilitiesFile)
	if err != nil {
		return nil, err
	}
	var doc vocabularyDocument
	if err := config.DecodeYAMLStrict(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", CapabilitiesFile, err)
	}
	if doc.SchemaVersion != VocabularySchemaVersion {
		return nil, fmt.Errorf("%s: schema_version must be %q, got %q",
			CapabilitiesFile, VocabularySchemaVersion, doc.SchemaVersion)
	}
	extended, err := vocab.Extend(doc.Capabilities)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", CapabilitiesFile, err)
	}
	return extended, nil
}

// Root returns the resolved configuration directory.
func (f *Forge) Root() string { return f.root }

// StatePath returns the availability state file that was read, or empty when
// no file was present.
func (f *Forge) StatePath() string { return f.state.Path }

// Roles returns every defined role, ordered by id.
func (f *Forge) Roles() []*Role { return f.roles.All() }

// Role returns one role by id.
func (f *Forge) Role(id string) (*Role, error) { return f.roles.Get(id) }

// Runtimes returns every configured runtime, ordered by id.
func (f *Forge) Runtimes() []*Runtime { return f.runtimes.All() }

// Runtime returns one runtime by id.
func (f *Forge) Runtime(id string) (*Runtime, error) { return f.runtimes.Get(id) }

// Personas returns every configured persona, ordered by id.
func (f *Forge) Personas() []*Persona { return f.personas.All() }

// Persona returns one persona by id.
func (f *Forge) Persona(id string) (*Persona, error) { return f.personas.Get(id) }

// Capabilities returns the admitted capability vocabulary.
func (f *Forge) Capabilities() []string { return f.vocab.Names() }

// AvailabilityOf returns the availability report for a runtime.
func (f *Forge) AvailabilityOf(runtimeID string) availability.Report {
	return f.state.Lookup(runtimeID)
}

// resolveRole turns a request into a role id, resolving a persona when one is
// named. A persona resolves to a role and stops there.
func (f *Forge) resolveRole(req MatchRequest) (string, error) {
	role := strings.TrimSpace(req.Role)
	persona := strings.TrimSpace(req.Persona)
	switch {
	case role != "" && persona != "":
		resolved, err := f.personas.ResolveRole(persona)
		if err != nil {
			return "", err
		}
		if resolved != role {
			return "", fmt.Errorf("persona %q resolves to role %q, which conflicts with the requested role %q",
				persona, resolved, role)
		}
		return resolved, nil
	case persona != "":
		return f.personas.ResolveRole(persona)
	case role != "":
		return role, nil
	default:
		return "", fmt.Errorf("a role or a persona is required")
	}
}

// toInternal converts a public request into the internal matching request.
func (f *Forge) toInternal(req MatchRequest) (matching.Request, error) {
	roleID, err := f.resolveRole(req)
	if err != nil {
		return matching.Request{}, err
	}
	if len(req.Require) > 0 {
		if err := f.vocab.Validate(req.Require); err != nil {
			return matching.Request{}, err
		}
	}
	return matching.Request{
		RoleID:  roleID,
		Require: req.Require,
		Prefer:  req.Prefer,
		Exclude: req.Exclude,
		Now:     req.Now,
	}, nil
}

// Match returns eligible workers for a role in preference order.
func (f *Forge) Match(ctx context.Context, req MatchRequest) ([]Candidate, error) {
	result, err := f.MatchResult(ctx, req)
	if err != nil {
		return nil, err
	}
	return result.Candidates, nil
}

// MatchResult returns the full matching answer, including every rejection and
// the reason for it. Callers that need to explain a decision want this rather
// than Match.
func (f *Forge) MatchResult(ctx context.Context, req MatchRequest) (*MatchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	internal, err := f.toInternal(req)
	if err != nil {
		return nil, err
	}
	return f.matcher.Match(internal)
}

// Explain accounts for one role and runtime pairing.
func (f *Forge) Explain(ctx context.Context, req MatchRequest, runtimeID string) (*Explanation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	internal, err := f.toInternal(req)
	if err != nil {
		return nil, err
	}
	return f.matcher.Explain(internal, runtimeID)
}

// Availability reports who can take a role right now, including runtimes that
// are currently limited and when they are expected back.
func (f *Forge) Availability(ctx context.Context, req MatchRequest) (*AvailabilityResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	internal, err := f.toInternal(req)
	if err != nil {
		return nil, err
	}
	return f.matcher.Availability(internal)
}

// Preference tiers, re-exported so consumers can branch on a candidate's
// standing without importing an internal package.
const (
	// TierPreferred means the role names the runtime in preferred_runtimes.
	TierPreferred = matching.TierPreferred
	// TierFallback means the role names it in fallback_runtimes.
	TierFallback = matching.TierFallback
	// TierEligible means the role does not name it but it qualifies.
	TierEligible = matching.TierEligible
)

// Rejection stages, re-exported for the same reason. StageAvailability is the
// one consumers branch on most: it separates "currently limited" from
// "structurally unqualified".
const (
	StageRequestExcluded = matching.StageRequestExcluded
	StageRoleExcluded    = matching.StageRoleExcluded
	StageDisabled        = matching.StageDisabled
	StageCapability      = matching.StageCapability
	StageRequirement     = matching.StageRequirement
	StageAvailability    = matching.StageAvailability
)
