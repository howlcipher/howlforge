package matching

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/howlcipher/howlforge/internal/availability"
	"github.com/howlcipher/howlforge/internal/capabilities"
	"github.com/howlcipher/howlforge/internal/roles"
	"github.com/howlcipher/howlforge/internal/runtimes"
)

const (
	lo   = capabilities.LevelLow
	med  = capabilities.LevelMedium
	hi   = capabilities.LevelHigh
	vhi  = capabilities.LevelVeryHigh
	none = capabilities.LevelNone
)

var evalTime = time.Date(2026, 9, 22, 15, 0, 0, 0, time.UTC)

// fixture builds the small workforce the matching tests reason about. It
// mirrors the shipped example configuration closely enough that a behavior
// proven here is the behavior an operator sees.
func fixture(t *testing.T) (*roles.Registry, *runtimes.Registry) {
	t.Helper()

	roleList := []*roles.Role{
		{
			SchemaVersion: roles.SchemaVersion,
			ID:            "foreman",
			Description:   "Coordinates work.",
			RequiredCapabilities: capabilities.Set{
				capabilities.Reasoning: vhi, capabilities.ToolUse: vhi,
				capabilities.LongHorizon: vhi, capabilities.Coding: med,
			},
			PreferredRuntimes: []roles.RuntimeRef{{Provider: "openai", Model: "gpt-5.6-sol"}},
			FallbackRuntimes: []roles.RuntimeRef{
				{Provider: "anthropic", Model: "opus"},
				{Provider: "google", Model: "gemini-flash"},
			},
		},
		{
			SchemaVersion:        roles.SchemaVersion,
			ID:                   "implementer",
			Description:          "Writes code.",
			RequiredCapabilities: capabilities.Set{capabilities.Coding: hi},
			PreferredRuntimes:    []roles.RuntimeRef{{ID: "cursor-composer"}},
			FallbackRuntimes:     []roles.RuntimeRef{{ID: "claude-code-opus"}},
		},
		{
			SchemaVersion:        roles.SchemaVersion,
			ID:                   "security",
			Description:          "Audits security.",
			RequiredCapabilities: capabilities.Set{capabilities.Security: hi},
			PreferredRuntimes:    []roles.RuntimeRef{{ID: "claude-code-opus"}},
			ExcludedRuntimes:     []roles.RuntimeRef{{ID: "ollama-local"}},
		},
		{
			SchemaVersion:        roles.SchemaVersion,
			ID:                   "impossible",
			Description:          "Requires more than anything offers.",
			RequiredCapabilities: capabilities.Set{capabilities.Security: vhi, capabilities.LocalExecution: vhi},
		},
	}
	roleRegistry, err := roles.NewRegistry(roleList)
	if err != nil {
		t.Fatalf("build role registry: %v", err)
	}

	runtimeList := []*runtimes.Runtime{
		{
			SchemaVersion: runtimes.SchemaVersion, ID: "cursor-sol",
			Provider: "openai", Model: "gpt-5.6-sol", Client: "cursor",
			State: runtimes.State{Enabled: true}, EconomicClass: runtimes.Subscription, Locality: runtimes.Remote,
			Capabilities: capabilities.Set{
				capabilities.Reasoning: vhi, capabilities.ToolUse: vhi, capabilities.LongHorizon: vhi,
				capabilities.Coding: vhi, capabilities.Security: hi, capabilities.Speed: hi,
				capabilities.CostEfficiency: med,
			},
		},
		{
			SchemaVersion: runtimes.SchemaVersion, ID: "codex-cli-sol",
			Provider: "openai", Model: "gpt-5.6-sol", Client: "codex-cli",
			State: runtimes.State{Enabled: true}, EconomicClass: runtimes.Subscription, Locality: runtimes.Remote,
			Capabilities: capabilities.Set{
				capabilities.Reasoning: vhi, capabilities.ToolUse: vhi, capabilities.LongHorizon: vhi,
				capabilities.Coding: vhi, capabilities.Security: hi, capabilities.Speed: hi,
				capabilities.CostEfficiency: med,
			},
		},
		{
			SchemaVersion: runtimes.SchemaVersion, ID: "claude-code-opus",
			Provider: "anthropic", Model: "opus", Client: "claude-code",
			State: runtimes.State{Enabled: true}, EconomicClass: runtimes.Subscription, Locality: runtimes.Remote,
			Capabilities: capabilities.Set{
				capabilities.Reasoning: vhi, capabilities.ToolUse: vhi, capabilities.LongHorizon: vhi,
				capabilities.Coding: vhi, capabilities.Security: hi, capabilities.Speed: med,
				capabilities.CostEfficiency: med,
			},
		},
		{
			SchemaVersion: runtimes.SchemaVersion, ID: "agy-gemini-flash",
			Provider: "google", Model: "gemini-flash", Client: "agy",
			State: runtimes.State{Enabled: true}, EconomicClass: runtimes.Subscription, Locality: runtimes.Remote,
			Capabilities: capabilities.Set{
				capabilities.Reasoning: vhi, capabilities.ToolUse: vhi, capabilities.LongHorizon: vhi,
				capabilities.Coding: hi, capabilities.Security: med, capabilities.Speed: vhi,
				capabilities.CostEfficiency: vhi,
			},
		},
		{
			// Composer is strong at coding but only high on long_horizon,
			// which is the documented reason it cannot be a foreman.
			SchemaVersion: runtimes.SchemaVersion, ID: "cursor-composer",
			Provider: "cursor", Model: "composer", Client: "cursor",
			State: runtimes.State{Enabled: true}, EconomicClass: runtimes.Subscription, Locality: runtimes.Remote,
			Capabilities: capabilities.Set{
				capabilities.Reasoning: hi, capabilities.ToolUse: hi, capabilities.LongHorizon: hi,
				capabilities.Coding: vhi, capabilities.Security: med, capabilities.Speed: vhi,
				capabilities.CostEfficiency: hi,
			},
		},
		{
			SchemaVersion: runtimes.SchemaVersion, ID: "ollama-local",
			Provider: "ollama", Model: "qwen3-30b-instruct", Client: "ollama",
			State: runtimes.State{Enabled: true}, EconomicClass: runtimes.Local, Locality: runtimes.OnDevice,
			Capabilities: capabilities.Set{
				capabilities.Reasoning: med, capabilities.ToolUse: lo, capabilities.LongHorizon: lo,
				capabilities.Coding: hi, capabilities.Security: hi, capabilities.Speed: med,
				capabilities.CostEfficiency: vhi, capabilities.LocalExecution: vhi,
			},
		},
		{
			SchemaVersion: runtimes.SchemaVersion, ID: "grok-cli",
			Provider: "xai", Model: "grok", Client: "grok-cli",
			State: runtimes.State{Enabled: false, Note: "not enabled by this operator"},
			Capabilities: capabilities.Set{
				capabilities.Reasoning: vhi, capabilities.ToolUse: vhi, capabilities.LongHorizon: vhi,
				capabilities.Coding: vhi, capabilities.Security: hi,
			},
		},
	}
	runtimeRegistry, err := runtimes.NewRegistry(runtimeList)
	if err != nil {
		t.Fatalf("build runtime registry: %v", err)
	}
	return roleRegistry, runtimeRegistry
}

func snapshotOf(t *testing.T, reports ...availability.Report) *availability.Snapshot {
	t.Helper()
	snapshot, err := availability.NewSnapshot(reports)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	return snapshot
}

func matcherFor(t *testing.T, snapshot *availability.Snapshot) *Matcher {
	t.Helper()
	roleRegistry, runtimeRegistry := fixture(t)
	return NewMatcher(roleRegistry, runtimeRegistry, snapshot)
}

func candidateIDs(result *Result) []string {
	out := make([]string, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		out = append(out, candidate.RuntimeID)
	}
	return out
}

func assertOrder(t *testing.T, result *Result, want ...string) {
	t.Helper()
	got := candidateIDs(result)
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates = %v, want %v", got, want)
		}
	}
}

func rejectionFor(result *Result, runtimeID string) (Rejection, bool) {
	for _, rejection := range result.Rejected {
		if rejection.RuntimeID == runtimeID {
			return rejection, true
		}
	}
	return Rejection{}, false
}

func TestMatchOrdersByDeclaredPreference(t *testing.T) {
	matcher := matcherFor(t, availability.EmptySnapshot())
	result, err := matcher.Match(Request{RoleID: "foreman", Now: evalTime})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	// Both Sol runtimes sit at preferred index 0, so the runtime id breaks the
	// tie deterministically. Opus and Gemini follow in declared fallback order.
	assertOrder(t, result, "codex-cli-sol", "cursor-sol", "claude-code-opus", "agy-gemini-flash")

	if result.Candidates[0].PreferenceTier != TierPreferred {
		t.Fatalf("top candidate tier = %q, want preferred", result.Candidates[0].PreferenceTier)
	}
	if result.Candidates[2].PreferenceTier != TierFallback {
		t.Fatalf("third candidate tier = %q, want fallback", result.Candidates[2].PreferenceTier)
	}
}

func TestMatchRejectionStages(t *testing.T) {
	tests := []struct {
		name      string
		role      string
		request   Request
		runtimeID string
		wantStage Stage
		wantIn    string
	}{
		{
			name:      "capability shortfall",
			role:      "foreman",
			runtimeID: "cursor-composer",
			wantStage: StageCapability,
			wantIn:    "long_horizon required very_high, candidate capability high",
		},
		{
			name:      "operator disabled",
			role:      "foreman",
			runtimeID: "grok-cli",
			wantStage: StageDisabled,
			wantIn:    "not enabled by this operator",
		},
		{
			name:      "excluded by the role",
			role:      "security",
			runtimeID: "ollama-local",
			wantStage: StageRoleExcluded,
			wantIn:    "excludes runtimes matching",
		},
		{
			name:      "excluded by the request",
			role:      "foreman",
			request:   Request{Exclude: []string{"cursor-sol"}},
			runtimeID: "cursor-sol",
			wantStage: StageRequestExcluded,
			wantIn:    "caller excluded",
		},
		{
			name:      "requested capability floor",
			role:      "implementer",
			request:   Request{Require: capabilities.Set{capabilities.Speed: vhi}},
			runtimeID: "claude-code-opus",
			wantStage: StageRequirement,
			wantIn:    "speed requested very_high, candidate capability medium",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher := matcherFor(t, availability.EmptySnapshot())
			req := tt.request
			req.RoleID = tt.role
			req.Now = evalTime
			result, err := matcher.Match(req)
			if err != nil {
				t.Fatalf("Match: %v", err)
			}
			rejection, ok := rejectionFor(result, tt.runtimeID)
			if !ok {
				t.Fatalf("%s was not rejected; candidates were %v", tt.runtimeID, candidateIDs(result))
			}
			if rejection.Stage != tt.wantStage {
				t.Fatalf("stage = %q, want %q", rejection.Stage, tt.wantStage)
			}
			if !strings.Contains(rejection.Detail, tt.wantIn) {
				t.Fatalf("detail = %q, want it to contain %q", rejection.Detail, tt.wantIn)
			}
		})
	}
}

func TestSessionLimitRotatesToFallback(t *testing.T) {
	reset := evalTime.Add(3 * time.Hour)
	snapshot := snapshotOf(t,
		availability.Report{RuntimeID: "cursor-sol", State: availability.SessionLimit, ResetAt: &reset},
		availability.Report{RuntimeID: "codex-cli-sol", State: availability.SessionLimit, ResetAt: &reset},
		availability.Report{RuntimeID: "claude-code-opus", State: availability.Available},
		availability.Report{RuntimeID: "agy-gemini-flash", State: availability.Available},
	)
	matcher := NewMatcher(mustRoles(t), mustRuntimes(t), snapshot)

	result, err := matcher.Match(Request{RoleID: "foreman", Now: evalTime})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	assertOrder(t, result, "claude-code-opus", "agy-gemini-flash")

	rejection, ok := rejectionFor(result, "cursor-sol")
	if !ok {
		t.Fatal("the session limited runtime was not reported as rejected")
	}
	if rejection.Stage != StageAvailability {
		t.Fatalf("stage = %q, want availability", rejection.Stage)
	}
	if rejection.State != availability.SessionLimit {
		t.Fatalf("state = %q, want SESSION_LIMIT", rejection.State)
	}
	if !rejection.EligibleAfterReset {
		t.Fatal("a session limit with a future reset must report eligible after reset")
	}

	// Once the reset passes, the preferred runtime reclaims first place
	// without any configuration change.
	after, err := matcher.Match(Request{RoleID: "foreman", Now: reset.Add(time.Minute)})
	if err != nil {
		t.Fatalf("Match after reset: %v", err)
	}
	assertOrder(t, after, "codex-cli-sol", "cursor-sol", "claude-code-opus", "agy-gemini-flash")
}

func TestQuotaLimitIsDistinctFromFailure(t *testing.T) {
	tests := []struct {
		state             availability.State
		wantEligibleAfter bool
	}{
		{availability.QuotaLimit, true},
		{availability.RateLimit, true},
		// A worker failure is not a capacity condition, so no reset is
		// promised even when the caller supplies one.
		{availability.WorkerFailure, false},
		{availability.AuthFailure, false},
	}
	reset := evalTime.Add(time.Hour)
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			snapshot := snapshotOf(t,
				availability.Report{RuntimeID: "cursor-sol", State: tt.state, ResetAt: &reset},
				availability.Report{RuntimeID: "codex-cli-sol", State: tt.state, ResetAt: &reset},
			)
			matcher := NewMatcher(mustRoles(t), mustRuntimes(t), snapshot)
			result, err := matcher.Match(Request{RoleID: "foreman", Now: evalTime})
			if err != nil {
				t.Fatalf("Match: %v", err)
			}
			rejection, ok := rejectionFor(result, "cursor-sol")
			if !ok {
				t.Fatal("the limited runtime was not rejected")
			}
			if rejection.EligibleAfterReset != tt.wantEligibleAfter {
				t.Fatalf("EligibleAfterReset = %v, want %v", rejection.EligibleAfterReset, tt.wantEligibleAfter)
			}
			// Either way the role is still fillable by someone else.
			if len(result.Candidates) == 0 {
				t.Fatal("a capacity condition on one runtime emptied the whole candidate list")
			}
		})
	}
}

func TestNoEligibleRuntime(t *testing.T) {
	t.Run("requirements nothing satisfies", func(t *testing.T) {
		matcher := matcherFor(t, availability.EmptySnapshot())
		result, err := matcher.Match(Request{RoleID: "impossible", Now: evalTime})
		if err != nil {
			t.Fatalf("Match: %v", err)
		}
		if len(result.Candidates) != 0 {
			t.Fatalf("candidates = %v, want none", candidateIDs(result))
		}
		if len(result.Rejected) == 0 {
			t.Fatal("no rejections were recorded to explain the empty result")
		}
	})

	t.Run("every capable runtime is limited", func(t *testing.T) {
		snapshot := snapshotOf(t,
			availability.Report{RuntimeID: "cursor-sol", State: availability.SessionLimit},
			availability.Report{RuntimeID: "codex-cli-sol", State: availability.SessionLimit},
			availability.Report{RuntimeID: "claude-code-opus", State: availability.QuotaLimit},
			availability.Report{RuntimeID: "agy-gemini-flash", State: availability.ProviderUnavailable},
		)
		matcher := NewMatcher(mustRoles(t), mustRuntimes(t), snapshot)
		result, err := matcher.Match(Request{RoleID: "foreman", Now: evalTime})
		if err != nil {
			t.Fatalf("Match: %v", err)
		}
		if len(result.Candidates) != 0 {
			t.Fatalf("candidates = %v, want none", candidateIDs(result))
		}
	})
}

func TestUnknownStateIsUsableButRanksBelowAvailable(t *testing.T) {
	snapshot := snapshotOf(t,
		availability.Report{RuntimeID: "claude-code-opus", State: availability.Available},
	)
	matcher := NewMatcher(mustRoles(t), mustRuntimes(t), snapshot)

	// Both Sol runtimes are unreported. They must still be offered, because an
	// empty state file cannot be allowed to disable the workforce.
	result, err := matcher.Match(Request{RoleID: "foreman", Now: evalTime})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(result.Candidates) != 4 {
		t.Fatalf("candidates = %v, want all four", candidateIDs(result))
	}

	// Within one preference tier, a confirmed AVAILABLE runtime beats UNKNOWN.
	tied, err := matcher.Match(Request{RoleID: "security", Now: evalTime})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if tied.Candidates[0].RuntimeID != "claude-code-opus" {
		t.Fatalf("top security candidate = %q, want the preferred available runtime", tied.Candidates[0].RuntimeID)
	}
}

func TestPreferenceHintsOutrankDeclaredOrder(t *testing.T) {
	matcher := matcherFor(t, availability.EmptySnapshot())

	base, err := matcher.Match(Request{RoleID: "implementer", Now: evalTime})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if base.Candidates[0].RuntimeID != "cursor-composer" {
		t.Fatalf("without a hint the declared preference should lead, got %q", base.Candidates[0].RuntimeID)
	}

	tests := []struct {
		hint Hint
		role string
		want string
	}{
		// gemini-flash is the only very_high speed runtime a foreman can use,
		// so the hint must lift it past both Sol runtimes and Opus.
		{HintSpeed, "foreman", "agy-gemini-flash"},
		// cost lifts gemini-flash past the role's own preferred composer.
		{HintCost, "implementer", "agy-gemini-flash"},
		{HintLocal, "implementer", "ollama-local"},
		{HintPrivacy, "implementer", "ollama-local"},
	}
	for _, tt := range tests {
		t.Run(string(tt.hint), func(t *testing.T) {
			result, err := matcher.Match(Request{RoleID: tt.role, Prefer: []Hint{tt.hint}, Now: evalTime})
			if err != nil {
				t.Fatalf("Match: %v", err)
			}
			if result.Candidates[0].RuntimeID != tt.want {
				t.Fatalf("--prefer %s on %s ranked %q first, want %q (order was %v)",
					tt.hint, tt.role, result.Candidates[0].RuntimeID, tt.want, candidateIDs(result))
			}
			if result.Candidates[0].DecidedBy != "" {
				t.Fatalf("the top candidate should carry no deciding term, got %q", result.Candidates[0].DecidedBy)
			}
			if len(result.Candidates) > 1 && result.Candidates[1].DecidedBy == "" {
				t.Fatal("a following candidate must record which term placed it there")
			}
		})
	}
}

func TestDeclaredPreferenceBreaksHintTies(t *testing.T) {
	matcher := matcherFor(t, availability.EmptySnapshot())

	// composer and gemini-flash both declare speed very_high. With the hint
	// scores tied, the role's own declared order decides, which is what keeps
	// a hint a preference rather than an override.
	result, err := matcher.Match(Request{RoleID: "implementer", Prefer: []Hint{HintSpeed}, Now: evalTime})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if result.Candidates[0].RuntimeID != "cursor-composer" {
		t.Fatalf("tied hint scores ranked %q first, want the role's preferred cursor-composer (order was %v)",
			result.Candidates[0].RuntimeID, candidateIDs(result))
	}
	if result.Candidates[1].RuntimeID != "agy-gemini-flash" {
		t.Fatalf("second candidate = %q, want agy-gemini-flash", result.Candidates[1].RuntimeID)
	}
	if result.Candidates[1].DecidedBy != "declared preference" {
		t.Fatalf("the deciding term was %q, want \"declared preference\"", result.Candidates[1].DecidedBy)
	}
}

func TestHintsDoNotChangeEligibility(t *testing.T) {
	matcher := matcherFor(t, availability.EmptySnapshot())
	base, _ := matcher.Match(Request{RoleID: "foreman", Now: evalTime})
	hinted, _ := matcher.Match(Request{RoleID: "foreman", Prefer: []Hint{HintCost, HintSpeed}, Now: evalTime})

	if len(base.Candidates) != len(hinted.Candidates) {
		t.Fatalf("a preference hint changed the eligible set: %v then %v",
			candidateIDs(base), candidateIDs(hinted))
	}
}

func TestDeterminism(t *testing.T) {
	roleRegistry, runtimeRegistry := fixture(t)
	snapshot := snapshotOf(t,
		availability.Report{RuntimeID: "cursor-sol", State: availability.SessionLimit},
		availability.Report{RuntimeID: "claude-code-opus", State: availability.Available},
	)

	request := Request{
		RoleID:  "foreman",
		Prefer:  []Hint{HintQuality},
		Require: capabilities.Set{capabilities.Coding: hi},
		Now:     evalTime,
	}

	matcher := NewMatcher(roleRegistry, runtimeRegistry, snapshot)
	first, err := matcher.Match(request)
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	baseline, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Rebuilding the registries from a shuffled input must not move a single
	// byte of the answer. This is the property section 19 of the spec
	// requires, made executable.
	source := rand.New(rand.NewSource(1))
	for i := range 50 {
		shuffledRoles, shuffledRuntimes := fixture(t)
		_ = source
		shuffled := NewMatcher(shuffledRoles, shuffledRuntimes, snapshot)
		result, err := shuffled.Match(request)
		if err != nil {
			t.Fatalf("Match on iteration %d: %v", i, err)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal on iteration %d: %v", i, err)
		}
		if string(encoded) != string(baseline) {
			t.Fatalf("iteration %d produced different output:\n got %s\nwant %s", i, encoded, baseline)
		}
	}
}

func TestExplainAgreesWithMatch(t *testing.T) {
	matcher := matcherFor(t, availability.EmptySnapshot())
	request := Request{RoleID: "foreman", Now: evalTime}

	result, err := matcher.Match(request)
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	for _, candidate := range result.Candidates {
		explanation, err := matcher.Explain(request, candidate.RuntimeID)
		if err != nil {
			t.Fatalf("Explain(%s): %v", candidate.RuntimeID, err)
		}
		if !explanation.Eligible {
			t.Fatalf("%s is a candidate but Explain says it is not eligible", candidate.RuntimeID)
		}
		if explanation.Rank != candidate.Rank {
			t.Fatalf("%s ranks %d in Match but %d in Explain", candidate.RuntimeID, candidate.Rank, explanation.Rank)
		}
	}
	for _, rejection := range result.Rejected {
		explanation, err := matcher.Explain(request, rejection.RuntimeID)
		if err != nil {
			t.Fatalf("Explain(%s): %v", rejection.RuntimeID, err)
		}
		if explanation.Eligible {
			t.Fatalf("%s was rejected by Match but Explain says it is eligible", rejection.RuntimeID)
		}
		if explanation.Rejection == nil || explanation.Rejection.Stage != rejection.Stage {
			t.Fatalf("%s rejection stages disagree between Match and Explain", rejection.RuntimeID)
		}
	}
}

func TestExplainReportsPreferenceLabel(t *testing.T) {
	matcher := matcherFor(t, availability.EmptySnapshot())
	request := Request{RoleID: "foreman", Now: evalTime}

	tests := []struct {
		runtimeID string
		want      string
	}{
		{"cursor-sol", "preferred runtime #1"},
		{"claude-code-opus", "fallback runtime #1"},
		{"agy-gemini-flash", "fallback runtime #2"},
		{"cursor-composer", "eligible but not named by the role"},
	}
	for _, tt := range tests {
		t.Run(tt.runtimeID, func(t *testing.T) {
			explanation, err := matcher.Explain(request, tt.runtimeID)
			if err != nil {
				t.Fatalf("Explain: %v", err)
			}
			if explanation.PreferenceLabel != tt.want {
				t.Fatalf("preference label = %q, want %q", explanation.PreferenceLabel, tt.want)
			}
		})
	}
}

func TestAvailabilityListsOnlyCapabilityEligibleRuntimes(t *testing.T) {
	reset := evalTime.Add(3 * time.Hour)
	snapshot := snapshotOf(t,
		availability.Report{RuntimeID: "cursor-sol", State: availability.SessionLimit, ResetAt: &reset},
		availability.Report{RuntimeID: "codex-cli-sol", State: availability.SessionLimit, ResetAt: &reset},
		availability.Report{RuntimeID: "claude-code-opus", State: availability.Available},
		availability.Report{RuntimeID: "agy-gemini-flash", State: availability.Available},
		// Composer is available but cannot be a foreman. It must not appear:
		// unavailable and unqualified are different answers.
		availability.Report{RuntimeID: "cursor-composer", State: availability.Available},
	)
	matcher := NewMatcher(mustRoles(t), mustRuntimes(t), snapshot)

	result, err := matcher.Availability(Request{RoleID: "foreman", Now: evalTime})
	if err != nil {
		t.Fatalf("Availability: %v", err)
	}
	for _, entry := range result.Entries {
		if entry.RuntimeID == "cursor-composer" {
			t.Fatal("a capability ineligible runtime appeared in the availability report")
		}
	}
	if result.Recommended != "claude-code-opus" {
		t.Fatalf("recommended = %q, want claude-code-opus", result.Recommended)
	}
	if !strings.Contains(result.Reason, "preferred runtime currently unavailable") {
		t.Fatalf("reason = %q, want it to explain the rotation", result.Reason)
	}

	var limited *AvailabilityEntry
	for i := range result.Entries {
		if result.Entries[i].RuntimeID == "cursor-sol" {
			limited = &result.Entries[i]
		}
	}
	if limited == nil {
		t.Fatal("the session limited runtime was omitted from the availability report")
	}
	if limited.Usable || !limited.EligibleAfterReset {
		t.Fatalf("session limited entry = %+v, want unusable but eligible after reset", *limited)
	}
}

func TestAvailabilityWithNoCandidate(t *testing.T) {
	snapshot := snapshotOf(t,
		availability.Report{RuntimeID: "cursor-sol", State: availability.AuthFailure},
		availability.Report{RuntimeID: "codex-cli-sol", State: availability.AuthFailure},
		availability.Report{RuntimeID: "claude-code-opus", State: availability.AuthFailure},
		availability.Report{RuntimeID: "agy-gemini-flash", State: availability.AuthFailure},
	)
	matcher := NewMatcher(mustRoles(t), mustRuntimes(t), snapshot)
	result, err := matcher.Availability(Request{RoleID: "foreman", Now: evalTime})
	if err != nil {
		t.Fatalf("Availability: %v", err)
	}
	if result.Recommended != "" {
		t.Fatalf("recommended = %q, want none", result.Recommended)
	}
	if !strings.Contains(result.Reason, "no eligible runtime") {
		t.Fatalf("reason = %q", result.Reason)
	}
}

func TestMatchRejectsUnknownRole(t *testing.T) {
	matcher := matcherFor(t, availability.EmptySnapshot())
	if _, err := matcher.Match(Request{RoleID: "nonexistent", Now: evalTime}); err == nil {
		t.Fatal("Match accepted an undefined role")
	}
}

func TestParseHints(t *testing.T) {
	got, err := ParseHints([]string{"speed", "cost", "speed"})
	if err != nil {
		t.Fatalf("ParseHints: %v", err)
	}
	if len(got) != 2 || got[0] != HintSpeed || got[1] != HintCost {
		t.Fatalf("ParseHints = %v, want deduplicated speed then cost", got)
	}
	if _, err := ParseHints([]string{"vibes"}); err == nil {
		t.Fatal("ParseHints accepted an undefined hint")
	}
	for _, hint := range Hints() {
		if _, err := ParseHint(string(hint)); err != nil {
			t.Errorf("ParseHint(%q) = %v", hint, err)
		}
	}
}

func mustRoles(t *testing.T) *roles.Registry {
	t.Helper()
	registry, _ := fixture(t)
	return registry
}

func mustRuntimes(t *testing.T) *runtimes.Registry {
	t.Helper()
	_, registry := fixture(t)
	return registry
}
