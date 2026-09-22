package matching

import (
	"fmt"
	"time"

	"github.com/howlcipher/howlforge/internal/availability"
	"github.com/howlcipher/howlforge/internal/capabilities"
	"github.com/howlcipher/howlforge/internal/roles"
)

// CapabilityCheck is one requirement measured against one candidate.
type CapabilityCheck struct {
	Capability string             `json:"capability"`
	Required   capabilities.Level `json:"required"`
	Offered    capabilities.Level `json:"offered"`
	Satisfied  bool               `json:"satisfied"`
	// Source says whether the floor came from the role or from the request.
	Source string `json:"source"`
}

// Explanation is the full account of one role and runtime pairing.
type Explanation struct {
	Role            string             `json:"role"`
	RuntimeID       string             `json:"runtime_id"`
	Label           string             `json:"label"`
	Provider        string             `json:"provider"`
	Model           string             `json:"model"`
	Client          string             `json:"runtime"`
	Checks          []CapabilityCheck  `json:"checks"`
	State           availability.State `json:"state"`
	StateReason     string             `json:"state_reason,omitempty"`
	ResetAt         *time.Time         `json:"reset_at,omitempty"`
	PreferenceTier  PreferenceTier     `json:"preference_tier"`
	PreferenceIndex int                `json:"preference_index"`
	PreferenceLabel string             `json:"preference_label"`
	Enabled         bool               `json:"enabled"`
	Eligible        bool               `json:"eligible"`
	Rank            int                `json:"rank,omitempty"`
	HintScore       int                `json:"hint_score,omitempty"`
	Rejection       *Rejection         `json:"rejection,omitempty"`
	EvaluatedAt     time.Time          `json:"evaluated_at"`
}

// PreferenceDescription renders a role's standing on a runtime the way the
// spec's explain output does, for example "preferred runtime #1".
func PreferenceDescription(tier PreferenceTier, index, preferredCount int) string {
	switch tier {
	case TierPreferred:
		return fmt.Sprintf("preferred runtime #%d", index+1)
	case TierFallback:
		return fmt.Sprintf("fallback runtime #%d", index-preferredCount+1)
	default:
		return "eligible but not named by the role"
	}
}

// Explain accounts for one role and runtime pairing.
//
// It answers by running the same Match the CLI would run and then locating the
// runtime in the result. Deriving the explanation from the real match, rather
// than recomputing it, is what guarantees `explain` can never contradict
// `match`.
func (m *Matcher) Explain(req Request, runtimeID string) (*Explanation, error) {
	role, err := m.roles.Get(req.RoleID)
	if err != nil {
		return nil, err
	}
	rt, err := m.runtimes.Get(runtimeID)
	if err != nil {
		return nil, err
	}
	result, err := m.Match(req)
	if err != nil {
		return nil, err
	}

	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	report := m.state.Lookup(rt.ID)
	tier, index := preferenceOf(role, rt)

	explanation := &Explanation{
		Role:            role.ID,
		RuntimeID:       rt.ID,
		Label:           rt.Label(),
		Provider:        rt.Provider,
		Model:           rt.Model,
		Client:          rt.Client,
		State:           report.EffectiveState(now),
		StateReason:     report.Reason,
		ResetAt:         report.ResetAt,
		PreferenceTier:  tier,
		PreferenceIndex: index,
		PreferenceLabel: PreferenceDescription(tier, index, len(role.PreferredRuntimes)),
		Enabled:         rt.State.Enabled,
		EvaluatedAt:     result.EvaluatedAt,
	}

	// Role floors first, then request floors, each in lexical order so the
	// rendered table is stable.
	for _, name := range role.RequiredCapabilities.Names() {
		want := role.RequiredCapabilities[name]
		have := rt.Capabilities.Get(name)
		explanation.Checks = append(explanation.Checks, CapabilityCheck{
			Capability: name,
			Required:   want,
			Offered:    have,
			Satisfied:  capabilities.Satisfies(want, have),
			Source:     "role",
		})
	}
	for _, name := range req.Require.Names() {
		want := req.Require[name]
		have := rt.Capabilities.Get(name)
		explanation.Checks = append(explanation.Checks, CapabilityCheck{
			Capability: name,
			Required:   want,
			Offered:    have,
			Satisfied:  capabilities.Satisfies(want, have),
			Source:     "request",
		})
	}

	for _, candidate := range result.Candidates {
		if candidate.RuntimeID != rt.ID {
			continue
		}
		explanation.Eligible = true
		explanation.Rank = candidate.Rank
		explanation.HintScore = candidate.HintScore
		return explanation, nil
	}
	for i := range result.Rejected {
		if result.Rejected[i].RuntimeID != rt.ID {
			continue
		}
		rejection := result.Rejected[i]
		explanation.Rejection = &rejection
		return explanation, nil
	}
	// Unreachable while every runtime is either offered or rejected, but an
	// explicit error beats a silently empty explanation if that ever changes.
	return nil, fmt.Errorf("runtime %q was neither offered nor rejected for role %q", rt.ID, role.ID)
}

// Availability reports the state of every runtime that is capability eligible
// for a role, whether or not it is usable right now.
//
// This is the question HowlPlane asks when a worker dies: not "who is best"
// but "who could actually take this over, and when does the preferred one come
// back".
type AvailabilityEntry struct {
	RuntimeID          string             `json:"runtime_id"`
	Label              string             `json:"label"`
	State              availability.State `json:"state"`
	Reason             string             `json:"reason,omitempty"`
	ResetAt            *time.Time         `json:"reset_at,omitempty"`
	Usable             bool               `json:"usable"`
	EligibleAfterReset bool               `json:"eligible_after_reset"`
	PreferenceTier     PreferenceTier     `json:"preference_tier"`
	Rank               int                `json:"rank,omitempty"`
}

// AvailabilityResult is the answer to an availability query.
type AvailabilityResult struct {
	Role        string              `json:"role,omitempty"`
	Entries     []AvailabilityEntry `json:"entries"`
	Recommended string              `json:"recommended,omitempty"`
	Reason      string              `json:"reason,omitempty"`
	EvaluatedAt time.Time           `json:"evaluated_at"`
}

// Availability answers "who can take this role right now".
func (m *Matcher) Availability(req Request) (*AvailabilityResult, error) {
	role, err := m.roles.Get(req.RoleID)
	if err != nil {
		return nil, err
	}
	result, err := m.Match(req)
	if err != nil {
		return nil, err
	}
	out := &AvailabilityResult{Role: role.ID, Entries: []AvailabilityEntry{}, EvaluatedAt: result.EvaluatedAt}

	for _, candidate := range result.Candidates {
		out.Entries = append(out.Entries, AvailabilityEntry{
			RuntimeID:      candidate.RuntimeID,
			Label:          candidate.Label,
			State:          candidate.State,
			Reason:         candidate.StateReason,
			Usable:         true,
			PreferenceTier: candidate.PreferenceTier,
			Rank:           candidate.Rank,
		})
	}
	// Only availability rejections belong here. A runtime excluded for lacking
	// a capability is not "unavailable"; it is unqualified, and conflating the
	// two is exactly the mistake this command exists to prevent.
	for _, rejection := range result.Rejected {
		if rejection.Stage != StageAvailability {
			continue
		}
		out.Entries = append(out.Entries, AvailabilityEntry{
			RuntimeID:          rejection.RuntimeID,
			Label:              rejection.Label,
			State:              rejection.State,
			Reason:             rejection.Detail,
			ResetAt:            rejection.ResetAt,
			Usable:             false,
			EligibleAfterReset: rejection.EligibleAfterReset,
			PreferenceTier:     preferenceTierFor(role, rejection.RuntimeID, m),
		})
	}

	if recommended, ok := result.Recommended(); ok {
		out.Recommended = recommended.RuntimeID
		out.Reason = recommendationReason(role, result, recommended)
	} else {
		out.Reason = "no eligible runtime is currently available for this role"
	}
	return out, nil
}

// preferenceTierFor resolves a runtime's tier for a role, tolerating a runtime
// id that is no longer registered.
func preferenceTierFor(role *roles.Role, runtimeID string, m *Matcher) PreferenceTier {
	rt, err := m.runtimes.Get(runtimeID)
	if err != nil {
		return TierEligible
	}
	tier, _ := preferenceOf(role, rt)
	return tier
}

// recommendationReason explains in one sentence why the top candidate is the
// top candidate, which is what HowlPlane logs when it launches a worker.
func recommendationReason(role *roles.Role, result *Result, top Candidate) string {
	if top.PreferenceTier == TierPreferred && top.PreferenceIndex == 0 && len(result.Hints) == 0 {
		return "the role's first preferred runtime is available"
	}
	blocked := 0
	for _, rejection := range result.Rejected {
		if rejection.Stage == StageAvailability {
			blocked++
		}
	}
	if blocked > 0 && top.PreferenceTier != TierPreferred {
		return fmt.Sprintf("preferred runtime currently unavailable; %s is the next eligible runtime", top.Label)
	}
	if len(result.Hints) > 0 {
		return fmt.Sprintf("ranked first under preference %v", result.Hints)
	}
	if blocked > 0 {
		return fmt.Sprintf("preferred runtime currently unavailable; %s is the next eligible runtime", top.Label)
	}
	return fmt.Sprintf("%s ranks first for role %s", top.Label, role.ID)
}
