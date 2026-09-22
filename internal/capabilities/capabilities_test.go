package capabilities

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Level
		wantErr bool
	}{
		{"none", "none", LevelNone, false},
		{"low", "low", LevelLow, false},
		{"medium", "medium", LevelMedium, false},
		{"high", "high", LevelHigh, false},
		{"very high", "very_high", LevelVeryHigh, false},
		{"upper case is normalized", "VERY_HIGH", LevelVeryHigh, false},
		{"surrounding space is trimmed", "  high  ", LevelHigh, false},
		{"synonyms are refused", "highest", LevelNone, true},
		{"numbers are refused", "4", LevelNone, true},
		{"empty is refused", "", LevelNone, true},
		{"hyphen form is refused", "very-high", LevelNone, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLevel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseLevel(%q) succeeded, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLevel(%q) returned %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestLevelIsOrdinal(t *testing.T) {
	// The whole matcher depends on this ordering, so it is asserted directly
	// rather than assumed from the constant declaration order.
	ascending := []Level{LevelNone, LevelLow, LevelMedium, LevelHigh, LevelVeryHigh}
	for i := 1; i < len(ascending); i++ {
		if !(ascending[i-1] < ascending[i]) {
			t.Fatalf("%v is not ordered below %v", ascending[i-1], ascending[i])
		}
	}
}

func TestSatisfies(t *testing.T) {
	tests := []struct {
		name     string
		required Level
		offered  Level
		want     bool
	}{
		{"exact match satisfies", LevelHigh, LevelHigh, true},
		{"exceeding satisfies", LevelHigh, LevelVeryHigh, true},
		{"falling short does not", LevelVeryHigh, LevelHigh, false},
		{"none is always satisfied", LevelNone, LevelNone, true},
		{"absent capability fails a real requirement", LevelLow, LevelNone, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Satisfies(tt.required, tt.offered); got != tt.want {
				t.Fatalf("Satisfies(%v, %v) = %v, want %v", tt.required, tt.offered, got, tt.want)
			}
		})
	}
}

func TestSetGetTreatsMissingAsNone(t *testing.T) {
	set := Set{Coding: LevelHigh}
	if got := set.Get(Reasoning); got != LevelNone {
		t.Fatalf("Get on an undeclared capability = %v, want none", got)
	}
	var nilSet Set
	if got := nilSet.Get(Coding); got != LevelNone {
		t.Fatalf("Get on a nil set = %v, want none", got)
	}
}

func TestShortfall(t *testing.T) {
	offered := Set{Reasoning: LevelHigh, Coding: LevelVeryHigh, LongHorizon: LevelHigh}

	t.Run("reports the first shortfall in lexical order", func(t *testing.T) {
		required := Set{Reasoning: LevelVeryHigh, LongHorizon: LevelVeryHigh}
		name, want, have, ok := offered.Shortfall(required)
		if ok {
			t.Fatal("Shortfall reported success, want a shortfall")
		}
		// long_horizon sorts before reasoning, so it must be reported first
		// for the diagnostic to be deterministic.
		if name != LongHorizon {
			t.Fatalf("Shortfall reported %q, want %q", name, LongHorizon)
		}
		if want != LevelVeryHigh || have != LevelHigh {
			t.Fatalf("Shortfall reported want=%v have=%v, want very_high and high", want, have)
		}
	})

	t.Run("succeeds when every requirement is met", func(t *testing.T) {
		required := Set{Reasoning: LevelHigh, Coding: LevelHigh}
		if _, _, _, ok := offered.Shortfall(required); !ok {
			t.Fatal("Shortfall reported a shortfall on a satisfied requirement")
		}
	})

	t.Run("an empty requirement is always satisfied", func(t *testing.T) {
		if _, _, _, ok := offered.Shortfall(nil); !ok {
			t.Fatal("Shortfall failed on an empty requirement set")
		}
	})

	t.Run("an undeclared capability fails the requirement", func(t *testing.T) {
		name, _, have, ok := offered.Shortfall(Set{Security: LevelLow})
		if ok {
			t.Fatal("an undeclared capability satisfied a requirement")
		}
		if name != Security || have != LevelNone {
			t.Fatalf("got name=%q have=%v, want security and none", name, have)
		}
	})
}

func TestSurplus(t *testing.T) {
	offered := Set{Reasoning: LevelVeryHigh, Coding: LevelHigh}
	required := Set{Reasoning: LevelHigh, Coding: LevelHigh}
	if got := offered.Surplus(required); got != 1 {
		t.Fatalf("Surplus = %d, want 1", got)
	}
	if got := offered.Surplus(nil); got != 0 {
		t.Fatalf("Surplus over an empty requirement = %d, want 0", got)
	}
}

func TestSetNamesIsSorted(t *testing.T) {
	set := Set{Testing: LevelHigh, Coding: LevelHigh, Reasoning: LevelHigh}
	got := set.Names()
	want := []string{Coding, Reasoning, Testing}
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names() = %v, want %v", got, want)
		}
	}
}

func TestLevelJSONRoundTrip(t *testing.T) {
	for _, level := range []Level{LevelNone, LevelLow, LevelMedium, LevelHigh, LevelVeryHigh} {
		data, err := json.Marshal(level)
		if err != nil {
			t.Fatalf("marshal %v: %v", level, err)
		}
		var decoded Level
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if decoded != level {
			t.Fatalf("round trip produced %v, want %v", decoded, level)
		}
		if !strings.Contains(string(data), level.String()) {
			t.Fatalf("marshal produced %s, want the level name", data)
		}
	}
}

func TestVocabularyKnowsBuiltins(t *testing.T) {
	vocab := DefaultVocabulary()
	for _, name := range BuiltinNames() {
		if !vocab.Known(name) {
			t.Fatalf("built-in capability %q is not admitted by the default vocabulary", name)
		}
	}
	if vocab.Known("teleportation") {
		t.Fatal("the default vocabulary admitted an undefined capability")
	}
	if len(BuiltinNames()) != 14 {
		t.Fatalf("built-in vocabulary has %d entries, want 14", len(BuiltinNames()))
	}
}

func TestVocabularyExtend(t *testing.T) {
	vocab := DefaultVocabulary()

	extended, err := vocab.Extend([]string{"negotiation"})
	if err != nil {
		t.Fatalf("Extend returned %v", err)
	}
	if !extended.Known("negotiation") {
		t.Fatal("the extension was not admitted")
	}
	if !extended.Known(Reasoning) {
		t.Fatal("extending dropped a built-in capability")
	}
	if vocab.Known("negotiation") {
		t.Fatal("Extend mutated the original vocabulary")
	}

	for _, bad := range []string{"", "Bad-Name", "has space", "UPPER"} {
		if _, err := vocab.Extend([]string{bad}); err == nil {
			t.Fatalf("Extend admitted the invalid name %q", bad)
		}
	}
}

func TestVocabularyValidateSuggests(t *testing.T) {
	vocab := DefaultVocabulary()

	err := vocab.Validate(Set{"resoning": LevelHigh})
	if err == nil {
		t.Fatal("Validate accepted a misspelled capability")
	}
	if !strings.Contains(err.Error(), "reasoning") {
		t.Fatalf("Validate error %q does not suggest the intended capability", err)
	}

	if err := vocab.Validate(Set{Reasoning: LevelHigh, Coding: LevelLow}); err != nil {
		t.Fatalf("Validate rejected a valid set: %v", err)
	}
	if err := vocab.Validate(nil); err != nil {
		t.Fatalf("Validate rejected an empty set: %v", err)
	}
}
