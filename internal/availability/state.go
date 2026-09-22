// Package availability models whether a declared runtime can actually be used
// right now.
//
// The central rule, inherited from HowlPlane's resource pool invariants: not
// all failures are equivalent. A session limit is a scheduling fact with a
// reset time. An engineering failure is not. Collapsing them into a single
// "unavailable" bit is what makes a workforce stall when it should rotate.
package availability

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// State enumerates every availability condition HowlForge distinguishes.
type State string

const (
	// Available means the runtime is usable now.
	Available State = "AVAILABLE"
	// Busy means the runtime is occupied by other work but not limited.
	Busy State = "BUSY"
	// Degraded means the runtime works with reduced quality or throughput.
	Degraded State = "DEGRADED"
	// SessionLimit means a per-session allowance is spent and will reset.
	SessionLimit State = "SESSION_LIMIT"
	// QuotaLimit means a longer-horizon allowance is spent.
	QuotaLimit State = "QUOTA_LIMIT"
	// RateLimit means requests are being throttled in the short term.
	RateLimit State = "RATE_LIMIT"
	// ProviderUnavailable means the provider itself is not reachable.
	ProviderUnavailable State = "PROVIDER_UNAVAILABLE"
	// ModelUnavailable means the specific model is not currently served.
	ModelUnavailable State = "MODEL_UNAVAILABLE"
	// AuthFailure means credentials are missing, expired or rejected.
	AuthFailure State = "AUTH_FAILURE"
	// WorkerFailure means the worker process failed for non-capacity reasons.
	WorkerFailure State = "WORKER_FAILURE"
	// Unknown means no state has been reported for this runtime.
	Unknown State = "UNKNOWN"
)

var allStates = []State{
	Available,
	Busy,
	Degraded,
	SessionLimit,
	QuotaLimit,
	RateLimit,
	ProviderUnavailable,
	ModelUnavailable,
	AuthFailure,
	WorkerFailure,
	Unknown,
}

// States returns every defined state in declaration order.
func States() []State {
	out := make([]State, len(allStates))
	copy(out, allStates)
	return out
}

// ParseState converts a reported state name into a State. Unrecognized input
// is an error rather than a silent downgrade to Unknown, because a state file
// HowlForge cannot read is a condition an operator needs to see.
func ParseState(s string) (State, error) {
	normalized := State(strings.ToUpper(strings.TrimSpace(s)))
	for _, candidate := range allStates {
		if candidate == normalized {
			return candidate, nil
		}
	}
	names := make([]string, 0, len(allStates))
	for _, candidate := range allStates {
		names = append(names, string(candidate))
	}
	return Unknown, fmt.Errorf("unknown availability state %q: expected one of %s",
		s, strings.Join(names, ", "))
}

// String renders the state name.
func (s State) String() string { return string(s) }

// Usable reports whether a runtime in this state may be dispatched work now.
//
// Unknown is usable: a runtime nobody has reported on is not thereby broken,
// and treating silence as failure would make an empty state file disable the
// entire workforce. It ranks below Available so a confirmed-healthy runtime
// always wins.
func (s State) Usable() bool {
	switch s {
	case Available, Degraded, Unknown:
		return true
	default:
		return false
	}
}

// CapacityLimited reports whether the state is an allowance condition rather
// than a fault. These are the states that are expected to clear on their own
// and that justify rotating to another runtime instead of failing the task.
func (s State) CapacityLimited() bool {
	switch s {
	case SessionLimit, QuotaLimit, RateLimit, Busy:
		return true
	default:
		return false
	}
}

// Transient reports whether the state is expected to clear without operator
// intervention. AuthFailure is deliberately excluded: a rejected credential
// does not fix itself, and retrying it wastes a rotation.
func (s State) Transient() bool {
	switch s {
	case SessionLimit, QuotaLimit, RateLimit, Busy, ProviderUnavailable, ModelUnavailable, Degraded:
		return true
	default:
		return false
	}
}

// rank orders usable states for deterministic candidate ranking. Lower is
// better. Non-usable states share the highest rank because they never reach
// the ranking phase.
func (s State) rank() int {
	switch s {
	case Available:
		return 0
	case Unknown:
		return 1
	case Degraded:
		return 2
	default:
		return 3
	}
}

// Rank exposes the ordering used by the matcher.
func (s State) Rank() int { return s.rank() }

// Report is the availability of a single runtime at a point in time.
type Report struct {
	// RuntimeID identifies the runtime this report describes.
	RuntimeID string `json:"runtime_id"`
	// State is the reported condition.
	State State `json:"state"`
	// Reason is optional free text explaining the state.
	Reason string `json:"reason,omitempty"`
	// ResetAt, when set, is when a capacity-limited state is expected to clear.
	ResetAt *time.Time `json:"reset_at,omitempty"`
	// ObservedAt, when set, is when the state was recorded.
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	// Source names who reported the state, for auditability.
	Source string `json:"source,omitempty"`
}

// EffectiveState resolves the report against a clock. A capacity-limited state
// whose reset time has passed becomes Unknown rather than Available: the reset
// is a claim about allowance, not an observation of health, and HowlForge does
// not invent observations it has not been given.
func (r Report) EffectiveState(now time.Time) State {
	if r.ResetAt == nil || !r.State.CapacityLimited() {
		return r.State
	}
	if now.Before(*r.ResetAt) {
		return r.State
	}
	return Unknown
}

// EligibleAfterReset reports whether the runtime is currently unusable but is
// expected to become eligible at a known time.
//
// Only a capacity condition can promise this. An auth failure or a worker
// failure carrying a reset_at is not a runtime that will come back on its own,
// and reporting one as "eligible after reset" would have HowlPlane wait out a
// credential that is still going to be rejected.
func (r Report) EligibleAfterReset(now time.Time) bool {
	state := r.EffectiveState(now)
	if state.Usable() || !state.CapacityLimited() {
		return false
	}
	return r.ResetAt != nil && now.Before(*r.ResetAt)
}

// Snapshot is a set of availability reports keyed by runtime id.
type Snapshot struct {
	reports map[string]Report
	// Path records where the snapshot was loaded from, or empty when the
	// snapshot is the implicit empty one.
	Path string
}

// NewSnapshot builds a snapshot from reports. A duplicate runtime id is an
// error: silently keeping one of two conflicting reports would make selection
// depend on file ordering.
func NewSnapshot(reports []Report) (*Snapshot, error) {
	out := &Snapshot{reports: make(map[string]Report, len(reports))}
	for _, report := range reports {
		id := strings.TrimSpace(report.RuntimeID)
		if id == "" {
			return nil, fmt.Errorf("availability report is missing runtime_id")
		}
		if _, exists := out.reports[id]; exists {
			return nil, fmt.Errorf("duplicate availability report for runtime %q", id)
		}
		report.RuntimeID = id
		out.reports[id] = report
	}
	return out, nil
}

// EmptySnapshot returns a snapshot with no reports, under which every runtime
// reads as Unknown.
func EmptySnapshot() *Snapshot {
	return &Snapshot{reports: map[string]Report{}}
}

// Lookup returns the report for a runtime. When no report exists, it
// synthesizes an Unknown report so callers never have to special case absence.
func (s *Snapshot) Lookup(runtimeID string) Report {
	if s != nil {
		if report, ok := s.reports[runtimeID]; ok {
			return report
		}
	}
	return Report{RuntimeID: runtimeID, State: Unknown, Source: "unreported"}
}

// Has reports whether an explicit report exists for a runtime.
func (s *Snapshot) Has(runtimeID string) bool {
	if s == nil {
		return false
	}
	_, ok := s.reports[runtimeID]
	return ok
}

// RuntimeIDs returns every reported runtime id in lexical order.
func (s *Snapshot) RuntimeIDs() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.reports))
	for id := range s.reports {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Len returns the number of explicit reports.
func (s *Snapshot) Len() int {
	if s == nil {
		return 0
	}
	return len(s.reports)
}
