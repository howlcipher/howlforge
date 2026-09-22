// Package matching turns a role plus declared runtime state into an ordered,
// explained list of eligible workers.
//
// Two properties are load bearing and are asserted by test:
//
//   - Determinism. Given identical role definitions, runtime definitions,
//     availability state and request, the output is byte identical, including
//     ordering. Every ranking term is an integer and the final tie-break is the
//     runtime id, so no two runs can disagree.
//   - Explainability. The ordered result and the explanation are produced by
//     the same pass, so `match` and `explain` can never tell different stories.
package matching

import (
	"fmt"
	"sort"
	"time"

	"github.com/howlcipher/howlforge/internal/availability"
	"github.com/howlcipher/howlforge/internal/capabilities"
	"github.com/howlcipher/howlforge/internal/roles"
	"github.com/howlcipher/howlforge/internal/runtimes"
)

// Stage names the filter stage that removed a runtime from consideration.
// Stages are evaluated in the order declared here.
type Stage string

const (
	// StageRequestExcluded is an explicit --exclude from the caller, which is
	// how HowlPlane asks for a rematch that skips a worker it just lost.
	StageRequestExcluded Stage = "request_excluded"
	// StageRoleExcluded is an excluded_runtimes entry on the role.
	StageRoleExcluded Stage = "role_excluded"
	// StageDisabled is an operator decision in the runtime's own state block.
	StageDisabled Stage = "disabled"
	// StageCapability is a role capability requirement the runtime cannot meet.
	StageCapability Stage = "capability"
	// StageRequirement is a per-request --require the runtime cannot meet.
	StageRequirement Stage = "requirement"
	// StageAvailability is a current availability state that is not usable.
	StageAvailability Stage = "availability"
)

// PreferenceTier describes where a runtime sits in a role's declared order.
type PreferenceTier string

const (
	// TierPreferred means the role names this runtime in preferred_runtimes.
	TierPreferred PreferenceTier = "preferred"
	// TierFallback means the role names it in fallback_runtimes.
	TierFallback PreferenceTier = "fallback"
	// TierEligible means the role does not name it but it meets every
	// requirement. Such a runtime is offered last rather than hidden, so a
	// newly added runtime is usable without editing every role.
	TierEligible PreferenceTier = "eligible"
)

// Request is one matching question.
type Request struct {
	// RoleID is the role to fill. Required.
	RoleID string
	// Require adds capability floors on top of the role's own requirements.
	Require capabilities.Set
	// Prefer lists soft ranking hints.
	Prefer []Hint
	// Exclude lists runtime ids that must not be offered.
	Exclude []string
	// Now is the evaluation time, used only to decide whether a capacity
	// limit has passed its reset. Callers pass an explicit time so output is
	// reproducible.
	Now time.Time
}

// Rejection records why a runtime was not offered.
type Rejection struct {
	RuntimeID string `json:"runtime_id"`
	Label     string `json:"label"`
	Stage     Stage  `json:"stage"`
	Reason    string `json:"reason"`
	Detail    string `json:"detail,omitempty"`

	// Capability fields are set when Stage is capability or requirement.
	Capability string              `json:"capability,omitempty"`
	Required   *capabilities.Level `json:"required,omitempty"`
	Offered    *capabilities.Level `json:"offered,omitempty"`

	// State is set when Stage is availability.
	State availability.State `json:"state,omitempty"`
	// ResetAt is set when an unusable state is expected to clear.
	ResetAt *time.Time `json:"reset_at,omitempty"`
	// EligibleAfterReset distinguishes "will come back" from "needs an
	// operator", which is the distinction that decides whether HowlPlane
	// should wait or rotate permanently.
	EligibleAfterReset bool `json:"eligible_after_reset"`
}

// Candidate is an eligible worker, ranked.
type Candidate struct {
	Rank              int                `json:"rank"`
	RuntimeID         string             `json:"runtime_id"`
	Label             string             `json:"label"`
	Provider          string             `json:"provider"`
	Model             string             `json:"model"`
	Client            string             `json:"runtime"`
	State             availability.State `json:"state"`
	StateReason       string             `json:"state_reason,omitempty"`
	PreferenceTier    PreferenceTier     `json:"preference_tier"`
	PreferenceIndex   int                `json:"preference_index"`
	HintScore         int                `json:"hint_score"`
	CapabilitySurplus int                `json:"capability_surplus"`
	// DecidedBy names the first ranking term that placed this candidate below
	// the one before it. The top candidate carries an empty value.
	DecidedBy string `json:"decided_by,omitempty"`

	key rankKey
}

// Result is the answer to a Request.
type Result struct {
	Role        string      `json:"role"`
	Hints       []string    `json:"hints,omitempty"`
	Candidates  []Candidate `json:"candidates"`
	Rejected    []Rejection `json:"rejected"`
	EvaluatedAt time.Time   `json:"evaluated_at"`
}

// Recommended returns the top candidate.
func (r *Result) Recommended() (Candidate, bool) {
	if r == nil || len(r.Candidates) == 0 {
		return Candidate{}, false
	}
	return r.Candidates[0], true
}

// rankKey is the deterministic sort key. Every term is an integer except the
// final id, which guarantees a total order with no ties.
type rankKey struct {
	// hintScore is negated so a plain ascending sort puts the best first.
	hintScore    int
	preference   int
	availability int
	surplus      int
	id           string
}

// rankTerms names the key fields in comparison order, for DecidedBy.
var rankTerms = []string{"preference hint", "declared preference", "availability", "capability surplus", "runtime id"}

// compare returns a negative, zero or positive result and the index of the
// first differing term.
func (k rankKey) compare(other rankKey) (int, int) {
	if k.hintScore != other.hintScore {
		return sign(k.hintScore - other.hintScore), 0
	}
	if k.preference != other.preference {
		return sign(k.preference - other.preference), 1
	}
	if k.availability != other.availability {
		return sign(k.availability - other.availability), 2
	}
	if k.surplus != other.surplus {
		return sign(k.surplus - other.surplus), 3
	}
	if k.id != other.id {
		if k.id < other.id {
			return -1, 4
		}
		return 1, 4
	}
	return 0, -1
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

// Matcher answers matching requests against a fixed configuration.
type Matcher struct {
	roles    *roles.Registry
	runtimes *runtimes.Registry
	state    *availability.Snapshot
}

// NewMatcher builds a matcher. The registries and snapshot are treated as
// immutable for the matcher's lifetime.
func NewMatcher(roleRegistry *roles.Registry, runtimeRegistry *runtimes.Registry, snapshot *availability.Snapshot) *Matcher {
	if snapshot == nil {
		snapshot = availability.EmptySnapshot()
	}
	return &Matcher{roles: roleRegistry, runtimes: runtimeRegistry, state: snapshot}
}

// Match evaluates a request and returns ranked candidates plus every
// rejection, in a single pass.
func (m *Matcher) Match(req Request) (*Result, error) {
	role, err := m.roles.Get(req.RoleID)
	if err != nil {
		return nil, err
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	excluded := make(map[string]bool, len(req.Exclude))
	for _, id := range req.Exclude {
		excluded[id] = true
	}

	result := &Result{
		Role:        role.ID,
		Hints:       HintNames(req.Prefer),
		Candidates:  []Candidate{},
		Rejected:    []Rejection{},
		EvaluatedAt: now.UTC(),
	}

	var eligible []Candidate
	// Registry.All is id-ordered, so evaluation order never depends on map
	// iteration.
	for _, rt := range m.runtimes.All() {
		candidate, rejection := m.evaluate(role, rt, req, excluded, now)
		if rejection != nil {
			result.Rejected = append(result.Rejected, *rejection)
			continue
		}
		eligible = append(eligible, *candidate)
	}

	sort.SliceStable(eligible, func(i, j int) bool {
		order, _ := eligible[i].key.compare(eligible[j].key)
		return order < 0
	})
	for i := range eligible {
		eligible[i].Rank = i + 1
		if i > 0 {
			_, term := eligible[i].key.compare(eligible[i-1].key)
			if term >= 0 && term < len(rankTerms) {
				eligible[i].DecidedBy = rankTerms[term]
			}
		}
	}
	result.Candidates = eligible

	// Rejections sort by stage order then id so the list is stable too.
	stageOrder := map[Stage]int{
		StageRequestExcluded: 0,
		StageRoleExcluded:    1,
		StageDisabled:        2,
		StageCapability:      3,
		StageRequirement:     4,
		StageAvailability:    5,
	}
	sort.SliceStable(result.Rejected, func(i, j int) bool {
		a, b := result.Rejected[i], result.Rejected[j]
		if stageOrder[a.Stage] != stageOrder[b.Stage] {
			return stageOrder[a.Stage] < stageOrder[b.Stage]
		}
		return a.RuntimeID < b.RuntimeID
	})
	return result, nil
}

// evaluate runs the hard filter for one runtime and, on success, builds its
// ranked candidate. Exactly one of the two return values is non-nil.
func (m *Matcher) evaluate(
	role *roles.Role,
	rt *runtimes.Runtime,
	req Request,
	excluded map[string]bool,
	now time.Time,
) (*Candidate, *Rejection) {
	reject := func(stage Stage, reason, detail string) *Rejection {
		return &Rejection{
			RuntimeID: rt.ID,
			Label:     rt.Label(),
			Stage:     stage,
			Reason:    reason,
			Detail:    detail,
		}
	}

	if excluded[rt.ID] {
		return nil, reject(StageRequestExcluded, "EXCLUDED_BY_REQUEST",
			"the caller excluded this runtime from this match")
	}
	for _, ref := range role.ExcludedRuntimes {
		if rt.MatchesRef(ref.ID, ref.Provider, ref.Model, ref.Runtime) {
			return nil, reject(StageRoleExcluded, "EXCLUDED_BY_ROLE",
				fmt.Sprintf("role %q excludes runtimes matching %s", role.ID, ref))
		}
	}
	if !rt.State.Enabled {
		detail := "the operator has not enabled this runtime"
		if rt.State.Note != "" {
			detail = rt.State.Note
		}
		return nil, reject(StageDisabled, "OPERATOR_DISABLED", detail)
	}

	// Capability before availability: a runtime that is merely limited right
	// now should be reported as an availability rejection, because that is the
	// list `howlforge availability <role>` exists to show.
	if name, want, have, ok := rt.Capabilities.Shortfall(role.RequiredCapabilities); !ok {
		rejection := reject(StageCapability, "MISSING_REQUIRED_CAPABILITY",
			fmt.Sprintf("%s required %s, candidate capability %s", name, want, have))
		rejection.Capability = name
		rejection.Required = &want
		rejection.Offered = &have
		return nil, rejection
	}
	if name, want, have, ok := rt.Capabilities.Shortfall(req.Require); !ok {
		rejection := reject(StageRequirement, "MISSING_REQUESTED_CAPABILITY",
			fmt.Sprintf("%s requested %s, candidate capability %s", name, want, have))
		rejection.Capability = name
		rejection.Required = &want
		rejection.Offered = &have
		return nil, rejection
	}

	report := m.state.Lookup(rt.ID)
	state := report.EffectiveState(now)
	if !state.Usable() {
		rejection := reject(StageAvailability, "STATE_"+string(state), report.Reason)
		rejection.State = state
		rejection.ResetAt = report.ResetAt
		rejection.EligibleAfterReset = report.EligibleAfterReset(now)
		if rejection.Detail == "" {
			if rejection.EligibleAfterReset {
				rejection.Detail = fmt.Sprintf("state %s, eligible after reset at %s",
					state, report.ResetAt.UTC().Format(time.RFC3339))
			} else {
				rejection.Detail = fmt.Sprintf("state %s", state)
			}
		}
		return nil, rejection
	}

	tier, index := preferenceOf(role, rt)
	// Requested floors are folded into the surplus so a --require that a
	// runtime clears comfortably still counts in its favor.
	surplus := rt.Capabilities.Surplus(role.RequiredCapabilities) + rt.Capabilities.Surplus(req.Require)
	hintScore := totalHintScore(req.Prefer, rt)

	candidate := &Candidate{
		RuntimeID:         rt.ID,
		Label:             rt.Label(),
		Provider:          rt.Provider,
		Model:             rt.Model,
		Client:            rt.Client,
		State:             state,
		StateReason:       report.Reason,
		PreferenceTier:    tier,
		PreferenceIndex:   index,
		HintScore:         hintScore,
		CapabilitySurplus: surplus,
		key: rankKey{
			hintScore:    -hintScore,
			preference:   index,
			availability: state.Rank(),
			surplus:      -surplus,
			id:           rt.ID,
		},
	}
	return candidate, nil
}

// preferenceOf locates a runtime in a role's declared order. Preferred entries
// come first, then fallbacks, then everything else eligible.
func preferenceOf(role *roles.Role, rt *runtimes.Runtime) (PreferenceTier, int) {
	for i, ref := range role.PreferredRuntimes {
		if rt.MatchesRef(ref.ID, ref.Provider, ref.Model, ref.Runtime) {
			return TierPreferred, i
		}
	}
	for i, ref := range role.FallbackRuntimes {
		if rt.MatchesRef(ref.ID, ref.Provider, ref.Model, ref.Runtime) {
			return TierFallback, len(role.PreferredRuntimes) + i
		}
	}
	return TierEligible, len(role.PreferredRuntimes) + len(role.FallbackRuntimes)
}
