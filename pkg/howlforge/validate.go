package howlforge

import (
	"context"
	"fmt"
	"sort"

	"github.com/howlcipher/howlforge/internal/capabilities"
	"github.com/howlcipher/howlforge/internal/roles"
	"github.com/howlcipher/howlforge/internal/runtimes"
)

// Severity distinguishes a configuration defect from an advisory.
type Severity string

const (
	// SeverityError means the configuration is not usable as written.
	SeverityError Severity = "error"
	// SeverityWarning means the configuration works but something is likely
	// not what the author intended.
	SeverityWarning Severity = "warning"
)

// Scope names the kind of object a problem concerns.
type Scope string

const (
	ScopeRole      Scope = "role"
	ScopeRuntime   Scope = "runtime"
	ScopePersona   Scope = "persona"
	ScopeState     Scope = "availability"
	ScopeWorkforce Scope = "workforce"
)

// Problem is one validation finding. Every field is populated where it is
// known, because a diagnostic that does not say which file and which field is
// a diagnostic an operator has to go hunting with.
type Problem struct {
	Severity Severity `json:"severity"`
	Scope    Scope    `json:"scope"`
	Subject  string   `json:"subject,omitempty"`
	File     string   `json:"file,omitempty"`
	Field    string   `json:"field,omitempty"`
	Message  string   `json:"message"`
}

// String renders a problem for terminal output.
func (p Problem) String() string {
	location := p.File
	if location == "" {
		location = string(p.Scope)
		if p.Subject != "" {
			location += " " + p.Subject
		}
	}
	if p.Field != "" {
		location += ":" + p.Field
	}
	return fmt.Sprintf("%s: %s: %s", p.Severity, location, p.Message)
}

// ValidationReport is the result of checking a loaded configuration.
type ValidationReport struct {
	Root            string    `json:"root"`
	StatePath       string    `json:"state_path,omitempty"`
	Roles           int       `json:"roles"`
	Runtimes        int       `json:"runtimes"`
	EnabledRuntimes int       `json:"enabled_runtimes"`
	Personas        int       `json:"personas"`
	Capabilities    int       `json:"capabilities"`
	Problems        []Problem `json:"problems"`
	OK              bool      `json:"ok"`
}

// Errors returns only the error-severity problems.
func (r *ValidationReport) Errors() []Problem {
	var out []Problem
	for _, problem := range r.Problems {
		if problem.Severity == SeverityError {
			out = append(out, problem)
		}
	}
	return out
}

// Warnings returns only the warning-severity problems.
func (r *ValidationReport) Warnings() []Problem {
	var out []Problem
	for _, problem := range r.Problems {
		if problem.Severity == SeverityWarning {
			out = append(out, problem)
		}
	}
	return out
}

// Validate checks a loaded configuration for coherence.
//
// It reports every problem it finds rather than stopping at the first one, so
// one run of `howlforge validate` is enough to fix a broken configuration.
// Defects that make loading impossible at all (duplicate ids, circular
// inheritance, unknown schema versions, malformed capability levels) are
// refused by Load before this runs, and the CLI reports those the same way.
func (f *Forge) Validate(ctx context.Context) (*ValidationReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	report := &ValidationReport{
		Root:            f.root,
		StatePath:       f.state.Path,
		Roles:           f.roles.Len(),
		Runtimes:        f.runtimes.Len(),
		EnabledRuntimes: len(f.runtimes.Enabled()),
		Personas:        f.personas.Len(),
		Capabilities:    len(f.vocab.Names()),
		Problems:        []Problem{},
	}

	f.validateVocabulary(report)
	f.validateRuntimeReferences(report)
	f.validateVerification(report)
	f.validatePersonas(report)
	f.validateFillability(report)
	f.validateStateReferences(report)

	sort.SliceStable(report.Problems, func(i, j int) bool {
		a, b := report.Problems[i], report.Problems[j]
		if a.Severity != b.Severity {
			// Errors first: they are what blocks the operator.
			return a.Severity == SeverityError
		}
		if a.Scope != b.Scope {
			return a.Scope < b.Scope
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		return a.Message < b.Message
	})
	report.OK = len(report.Errors()) == 0
	return report, nil
}

// validateVocabulary checks every declared capability name against the
// controlled vocabulary, on roles and runtimes alike.
func (f *Forge) validateVocabulary(report *ValidationReport) {
	for _, role := range f.roles.All() {
		for _, name := range role.RequiredCapabilities.Names() {
			if f.vocab.Known(name) {
				continue
			}
			report.Problems = append(report.Problems, Problem{
				Severity: SeverityError, Scope: ScopeRole, Subject: role.ID, File: role.SourcePath,
				Field:   "required_capabilities." + name,
				Message: unknownCapabilityMessage(f.vocab, name),
			})
		}
	}
	for _, rt := range f.runtimes.All() {
		for _, name := range rt.Capabilities.Names() {
			if f.vocab.Known(name) {
				continue
			}
			report.Problems = append(report.Problems, Problem{
				Severity: SeverityError, Scope: ScopeRuntime, Subject: rt.ID, File: rt.SourcePath,
				Field:   "capabilities." + name,
				Message: unknownCapabilityMessage(f.vocab, name),
			})
		}
	}
}

// unknownCapabilityMessage borrows the vocabulary's own suggestion logic so the
// CLI and the library produce the same wording.
func unknownCapabilityMessage(vocab *capabilities.Vocabulary, name string) string {
	err := vocab.Validate(capabilities.Set{name: capabilities.LevelNone})
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("unknown capability %q", name)
}

// validateRuntimeReferences checks that every runtime selector in every role
// actually selects something, and warns when a selector resolves only to
// runtimes the operator has disabled.
func (f *Forge) validateRuntimeReferences(report *ValidationReport) {
	for _, role := range f.roles.All() {
		for _, list := range []struct {
			field string
			refs  []roles.RuntimeRef
		}{
			{"preferred_runtimes", role.PreferredRuntimes},
			{"fallback_runtimes", role.FallbackRuntimes},
			{"excluded_runtimes", role.ExcludedRuntimes},
		} {
			for i, ref := range list.refs {
				field := fmt.Sprintf("%s[%d]", list.field, i)
				matched := f.runtimes.MatchRef(ref.ID, ref.Provider, ref.Model, ref.Runtime)
				if len(matched) == 0 {
					report.Problems = append(report.Problems, Problem{
						Severity: SeverityError, Scope: ScopeRole, Subject: role.ID, File: role.SourcePath,
						Field:   field,
						Message: fmt.Sprintf("selector %s matches no configured runtime", ref),
					})
					continue
				}
				// An exclusion that matches only disabled runtimes is
				// harmless, so only preference lists are worth a warning.
				if list.field == "excluded_runtimes" {
					continue
				}
				if !anyEnabled(matched) {
					report.Problems = append(report.Problems, Problem{
						Severity: SeverityWarning, Scope: ScopeRole, Subject: role.ID, File: role.SourcePath,
						Field: field,
						Message: fmt.Sprintf("selector %s matches only disabled runtimes (%s)",
							ref, joinRuntimeIDs(matched)),
					})
				}
			}
		}
	}
}

func anyEnabled(list []*runtimes.Runtime) bool {
	for _, rt := range list {
		if rt.State.Enabled {
			return true
		}
	}
	return false
}

func joinRuntimeIDs(list []*runtimes.Runtime) string {
	ids := make([]string, 0, len(list))
	for _, rt := range list {
		ids = append(ids, rt.ID)
	}
	sort.Strings(ids)
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ", "
		}
		out += id
	}
	return out
}

// validateVerification enforces the HowlProof requirement that a worker must
// not certify its own work.
func (f *Forge) validateVerification(report *ValidationReport) {
	for _, role := range f.roles.All() {
		verifier := role.VerifierRole()
		if verifier == "" {
			if role.RequiresVerification() {
				report.Problems = append(report.Problems, Problem{
					Severity: SeverityWarning, Scope: ScopeRole, Subject: role.ID, File: role.SourcePath,
					Field:   "verification.verifier_role",
					Message: "verification is required but no verifier_role is named",
				})
			}
			continue
		}
		if !f.roles.Has(verifier) {
			report.Problems = append(report.Problems, Problem{
				Severity: SeverityError, Scope: ScopeRole, Subject: role.ID, File: role.SourcePath,
				Field:   "verification.verifier_role",
				Message: fmt.Sprintf("names unknown verifier_role %q", verifier),
			})
			continue
		}
		if verifier == role.ID {
			report.Problems = append(report.Problems, Problem{
				Severity: SeverityError, Scope: ScopeRole, Subject: role.ID, File: role.SourcePath,
				Field:   "verification.verifier_role",
				Message: "names itself as its own verifier; a worker must not self certify",
			})
		}
	}
}

// validatePersonas checks that every persona resolves to a defined role.
func (f *Forge) validatePersonas(report *ValidationReport) {
	for _, persona := range f.personas.All() {
		if f.roles.Has(persona.Role) {
			continue
		}
		report.Problems = append(report.Problems, Problem{
			Severity: SeverityError, Scope: ScopePersona, Subject: persona.ID, File: persona.SourcePath,
			Field:   "role",
			Message: fmt.Sprintf("references unknown role %q", persona.Role),
		})
	}
}

// validateFillability catches the impossible fallback chain: a role whose
// requirements no enabled runtime can meet. Such a role is not a configuration
// nobody has exercised yet, it is a role that can never be filled, and the
// operator should learn that from `validate` rather than from a failed match
// during an incident.
func (f *Forge) validateFillability(report *ValidationReport) {
	for _, role := range f.roles.All() {
		var capable, capableEnabled int
		for _, rt := range f.runtimes.All() {
			if _, _, _, ok := rt.Capabilities.Shortfall(role.RequiredCapabilities); !ok {
				continue
			}
			if excludedByRole(role, rt) {
				continue
			}
			capable++
			if rt.State.Enabled {
				capableEnabled++
			}
		}
		switch {
		case capable == 0:
			report.Problems = append(report.Problems, Problem{
				Severity: SeverityError, Scope: ScopeWorkforce, Subject: role.ID, File: role.SourcePath,
				Field:   "required_capabilities",
				Message: "no configured runtime can meet these requirements, so this role can never be filled",
			})
		case capableEnabled == 0:
			report.Problems = append(report.Problems, Problem{
				Severity: SeverityError, Scope: ScopeWorkforce, Subject: role.ID, File: role.SourcePath,
				Field:   "required_capabilities",
				Message: fmt.Sprintf("%d runtime(s) could meet these requirements but none of them is enabled", capable),
			})
		case capableEnabled == 1:
			report.Problems = append(report.Problems, Problem{
				Severity: SeverityWarning, Scope: ScopeWorkforce, Subject: role.ID, File: role.SourcePath,
				Field:   "fallback_runtimes",
				Message: "only one enabled runtime can fill this role, so it has no fallback if that runtime is limited",
			})
		}
	}
}

func excludedByRole(role *roles.Role, rt *runtimes.Runtime) bool {
	for _, ref := range role.ExcludedRuntimes {
		if rt.MatchesRef(ref.ID, ref.Provider, ref.Model, ref.Runtime) {
			return true
		}
	}
	return false
}

// validateStateReferences warns about availability reports for runtimes that
// are not configured. That usually means a renamed runtime left a stale entry
// behind, which would silently stop applying.
func (f *Forge) validateStateReferences(report *ValidationReport) {
	for _, id := range f.state.RuntimeIDs() {
		if f.runtimes.Has(id) {
			continue
		}
		report.Problems = append(report.Problems, Problem{
			Severity: SeverityWarning, Scope: ScopeState, Subject: id, File: f.state.Path,
			Message: "availability state names a runtime that is not configured; the entry has no effect",
		})
	}
}
