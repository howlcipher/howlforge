package availability

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed
}

func TestParseState(t *testing.T) {
	for _, state := range States() {
		got, err := ParseState(string(state))
		if err != nil {
			t.Fatalf("ParseState(%q) returned %v", state, err)
		}
		if got != state {
			t.Fatalf("ParseState(%q) = %q", state, got)
		}
	}
	if _, err := ParseState("session_limit"); err != nil {
		t.Fatalf("ParseState should normalize case: %v", err)
	}
	if _, err := ParseState("EXHAUSTED"); err == nil {
		t.Fatal("ParseState accepted an undefined state")
	}
}

func TestStateClassification(t *testing.T) {
	tests := []struct {
		state           State
		usable          bool
		capacityLimited bool
		transient       bool
	}{
		{Available, true, false, false},
		{Unknown, true, false, false},
		{Degraded, true, false, true},
		{Busy, false, true, true},
		{SessionLimit, false, true, true},
		{QuotaLimit, false, true, true},
		{RateLimit, false, true, true},
		{ProviderUnavailable, false, false, true},
		{ModelUnavailable, false, false, true},
		// An auth failure does not fix itself. Treating it as transient would
		// waste a rotation retrying a credential that is still rejected.
		{AuthFailure, false, false, false},
		{WorkerFailure, false, false, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.Usable(); got != tt.usable {
				t.Errorf("Usable() = %v, want %v", got, tt.usable)
			}
			if got := tt.state.CapacityLimited(); got != tt.capacityLimited {
				t.Errorf("CapacityLimited() = %v, want %v", got, tt.capacityLimited)
			}
			if got := tt.state.Transient(); got != tt.transient {
				t.Errorf("Transient() = %v, want %v", got, tt.transient)
			}
		})
	}
}

func TestWorkerFailureIsNotACapacityLimit(t *testing.T) {
	// The invariant HowlPlane's resource pool insists on: an engineering
	// failure must never be recorded as quota exhaustion.
	if WorkerFailure.CapacityLimited() {
		t.Fatal("WORKER_FAILURE was classified as a capacity limit")
	}
	if !SessionLimit.CapacityLimited() {
		t.Fatal("SESSION_LIMIT was not classified as a capacity limit")
	}
}

func TestReportEffectiveState(t *testing.T) {
	reset := mustTime(t, "2026-09-22T18:00:00Z")
	report := Report{RuntimeID: "cursor-sol", State: SessionLimit, ResetAt: &reset}

	before := mustTime(t, "2026-09-22T15:00:00Z")
	if got := report.EffectiveState(before); got != SessionLimit {
		t.Fatalf("before the reset the state is %q, want SESSION_LIMIT", got)
	}
	if !report.EligibleAfterReset(before) {
		t.Fatal("a limited runtime with a future reset should report eligible after reset")
	}

	after := mustTime(t, "2026-09-22T19:00:00Z")
	// UNKNOWN, not AVAILABLE: the reset time is a claim about allowance, not
	// an observation of health, and HowlForge does not invent observations.
	if got := report.EffectiveState(after); got != Unknown {
		t.Fatalf("after the reset the state is %q, want UNKNOWN", got)
	}
	if report.EligibleAfterReset(after) {
		t.Fatal("a cleared reset should no longer report eligible after reset")
	}
}

func TestReportWithoutResetDoesNotClear(t *testing.T) {
	report := Report{RuntimeID: "x", State: QuotaLimit}
	far := mustTime(t, "2030-01-01T00:00:00Z")
	if got := report.EffectiveState(far); got != QuotaLimit {
		t.Fatalf("a limit with no reset time became %q, want QUOTA_LIMIT", got)
	}
	if report.EligibleAfterReset(far) {
		t.Fatal("a limit with no reset time must not claim it will become eligible")
	}
}

func TestNonCapacityStatesIgnoreReset(t *testing.T) {
	reset := mustTime(t, "2026-01-01T00:00:00Z")
	report := Report{RuntimeID: "x", State: AuthFailure, ResetAt: &reset}
	after := mustTime(t, "2026-06-01T00:00:00Z")
	if got := report.EffectiveState(after); got != AuthFailure {
		t.Fatalf("an auth failure cleared itself at a reset time, got %q", got)
	}
}

func TestEligibleAfterResetRequiresACapacityCondition(t *testing.T) {
	// Regression: a fault state carrying a reset_at used to report that it
	// would become eligible, which would have HowlPlane wait out a credential
	// failure instead of rotating away from it.
	reset := mustTime(t, "2026-09-22T18:00:00Z")
	now := mustTime(t, "2026-09-22T15:00:00Z")

	capacity := []State{SessionLimit, QuotaLimit, RateLimit, Busy}
	for _, state := range capacity {
		report := Report{RuntimeID: "x", State: state, ResetAt: &reset}
		if !report.EligibleAfterReset(now) {
			t.Errorf("%s with a future reset should report eligible after reset", state)
		}
	}

	faults := []State{AuthFailure, WorkerFailure, ProviderUnavailable, ModelUnavailable}
	for _, state := range faults {
		report := Report{RuntimeID: "x", State: state, ResetAt: &reset}
		if report.EligibleAfterReset(now) {
			t.Errorf("%s must not promise eligibility at a reset time", state)
		}
	}

	// A usable state never reports eligible after reset either.
	for _, state := range []State{Available, Degraded, Unknown} {
		report := Report{RuntimeID: "x", State: state, ResetAt: &reset}
		if report.EligibleAfterReset(now) {
			t.Errorf("usable state %s reported eligible after reset", state)
		}
	}
}

func TestSnapshotLookupSynthesizesUnknown(t *testing.T) {
	snapshot := EmptySnapshot()
	report := snapshot.Lookup("absent")
	if report.State != Unknown {
		t.Fatalf("an unreported runtime is %q, want UNKNOWN", report.State)
	}
	if snapshot.Has("absent") {
		t.Fatal("Has reported true for an unreported runtime")
	}
}

func TestNewSnapshotRejectsDuplicates(t *testing.T) {
	_, err := NewSnapshot([]Report{
		{RuntimeID: "a", State: Available},
		{RuntimeID: "a", State: SessionLimit},
	})
	if err == nil {
		t.Fatal("NewSnapshot accepted two reports for one runtime")
	}
	if _, err := NewSnapshot([]Report{{RuntimeID: "  ", State: Available}}); err == nil {
		t.Fatal("NewSnapshot accepted a report with no runtime id")
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()

	t.Run("a missing file yields the empty snapshot", func(t *testing.T) {
		snapshot, err := LoadFile(filepath.Join(dir, "absent.json"))
		if err != nil {
			t.Fatalf("LoadFile on a missing file returned %v, want the empty snapshot", err)
		}
		if snapshot.Len() != 0 {
			t.Fatalf("empty snapshot has %d reports", snapshot.Len())
		}
	})

	t.Run("an empty path yields the empty snapshot", func(t *testing.T) {
		snapshot, err := LoadFile("")
		if err != nil || snapshot.Len() != 0 {
			t.Fatalf("LoadFile(\"\") = %v, %v", snapshot, err)
		}
	})

	valid := `{
	  "schema_version": "howlforge.availability/v1",
	  "runtimes": [
	    {"runtime_id": "a", "state": "SESSION_LIMIT", "reset_at": "2026-09-22T18:00:00Z"},
	    {"runtime_id": "b", "state": "AVAILABLE"}
	  ]
	}`
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Run("a valid file loads", func(t *testing.T) {
		snapshot, err := LoadFile(path)
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		if snapshot.Len() != 2 {
			t.Fatalf("loaded %d reports, want 2", snapshot.Len())
		}
		if got := snapshot.Lookup("a").State; got != SessionLimit {
			t.Fatalf("runtime a is %q, want SESSION_LIMIT", got)
		}
		if snapshot.Lookup("a").ResetAt == nil {
			t.Fatal("the reset time was dropped")
		}
		if snapshot.Path == "" {
			t.Fatal("the snapshot did not record where it was loaded from")
		}
	})

	bad := []struct {
		name    string
		content string
		wantErr string
	}{
		{"wrong schema version", `{"schema_version":"howlforge.availability/v2","runtimes":[]}`, "schema_version"},
		{"unknown state", `{"schema_version":"howlforge.availability/v1","runtimes":[{"runtime_id":"a","state":"NAPPING"}]}`, "unknown availability state"},
		{"unknown field", `{"schema_version":"howlforge.availability/v1","runtimes":[],"extra":1}`, "unknown field"},
		{"duplicate runtime", `{"schema_version":"howlforge.availability/v1","runtimes":[{"runtime_id":"a","state":"AVAILABLE"},{"runtime_id":"a","state":"BUSY"}]}`, "duplicate"},
		{"not json", `not json at all`, ""},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(dir, "bad.json")
			if err := os.WriteFile(p, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			_, loadErr := LoadFile(p)
			if loadErr == nil {
				t.Fatalf("LoadFile accepted %s", tt.name)
			}
			if tt.wantErr != "" && !strings.Contains(loadErr.Error(), tt.wantErr) {
				t.Fatalf("LoadFile returned %q, want it to mention %q", loadErr, tt.wantErr)
			}
		})
	}
}

func TestDefaultStatePath(t *testing.T) {
	if got := DefaultStatePath("/explicit/path.json"); got != "/explicit/path.json" {
		t.Fatalf("an explicit path was not honored, got %q", got)
	}
	t.Setenv(EnvStatePath, "/from/env.json")
	if got := DefaultStatePath(""); got != "/from/env.json" {
		t.Fatalf("the environment was not honored, got %q", got)
	}
	if got := DefaultStatePath("/explicit/path.json"); got != "/explicit/path.json" {
		t.Fatalf("the environment overrode an explicit path, got %q", got)
	}
}
