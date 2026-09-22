package matching

import (
	"fmt"
	"sort"
	"strings"

	"github.com/howlcipher/howlforge/internal/capabilities"
	"github.com/howlcipher/howlforge/internal/runtimes"
)

// Hint is a soft ranking preference. Hints never change who is eligible; they
// only change the order eligible candidates are offered in.
//
// They are explicitly not truth claims about models. "prefer speed" means
// "rank by the speed capability this configuration declares", not "this model
// is objectively fast".
type Hint string

const (
	// HintQuality favors runtimes strong at both reasoning and coding.
	HintQuality Hint = "quality"
	// HintSpeed favors runtimes declaring high speed.
	HintSpeed Hint = "speed"
	// HintCost favors runtimes declaring high cost efficiency.
	HintCost Hint = "cost"
	// HintLocal favors runtimes that execute on operator hardware.
	HintLocal Hint = "local"
	// HintPrivacy favors runtimes that keep work on operator hardware.
	HintPrivacy Hint = "privacy"
	// HintContext favors runtimes with a large context window.
	HintContext Hint = "context"
)

var allHints = []Hint{HintQuality, HintSpeed, HintCost, HintLocal, HintPrivacy, HintContext}

// Hints returns every defined hint in declaration order.
func Hints() []Hint {
	out := make([]Hint, len(allHints))
	copy(out, allHints)
	return out
}

// ParseHint validates a preference hint name.
func ParseHint(s string) (Hint, error) {
	normalized := Hint(strings.ToLower(strings.TrimSpace(s)))
	for _, candidate := range allHints {
		if candidate == normalized {
			return candidate, nil
		}
	}
	names := make([]string, 0, len(allHints))
	for _, candidate := range allHints {
		names = append(names, string(candidate))
	}
	return "", fmt.Errorf("unknown preference %q: expected one of %s", s, strings.Join(names, ", "))
}

// Explain describes what a hint ranks on, for CLI help and explanation output.
func (h Hint) Explain() string {
	switch h {
	case HintQuality:
		return "lowest of reasoning and coding"
	case HintSpeed:
		return "speed"
	case HintCost:
		return "cost_efficiency"
	case HintLocal:
		return "local_execution, then local locality"
	case HintPrivacy:
		return "local_execution, then local locality"
	case HintContext:
		return "context_window"
	default:
		return string(h)
	}
}

// score rates one runtime against one hint. Higher is better. The scale is the
// capability ordinal scale, so scores across hints are commensurable and the
// sum over several hints stays meaningful.
func (h Hint) score(rt *runtimes.Runtime) int {
	caps := rt.Capabilities
	switch h {
	case HintQuality:
		// The weaker of the two axes, so a runtime that reasons well but codes
		// badly does not win a quality preference outright.
		reasoning := int(caps.Get(capabilities.Reasoning))
		coding := int(caps.Get(capabilities.Coding))
		if coding < reasoning {
			return coding
		}
		return reasoning
	case HintSpeed:
		return int(caps.Get(capabilities.Speed))
	case HintCost:
		return int(caps.Get(capabilities.CostEfficiency))
	case HintLocal, HintPrivacy:
		score := int(caps.Get(capabilities.LocalExecution))
		// Declared locality corroborates the capability. It is added as a
		// smaller term so a runtime that merely claims local_execution cannot
		// outrank one that is actually local.
		if rt.Locality == runtimes.OnDevice {
			score += len(capabilities.LevelNames())
		}
		return score
	case HintContext:
		return int(caps.Get(capabilities.ContextWindow))
	default:
		return 0
	}
}

// ParseHints validates and de-duplicates a list of hint names, preserving the
// order the operator gave them.
func ParseHints(raw []string) ([]Hint, error) {
	var out []Hint
	seen := map[Hint]bool{}
	for _, item := range raw {
		hint, err := ParseHint(item)
		if err != nil {
			return nil, err
		}
		if seen[hint] {
			continue
		}
		seen[hint] = true
		out = append(out, hint)
	}
	return out, nil
}

// totalHintScore sums every hint's score for a runtime.
func totalHintScore(hints []Hint, rt *runtimes.Runtime) int {
	total := 0
	for _, hint := range hints {
		total += hint.score(rt)
	}
	return total
}

// HintNames renders hints as strings in the order given.
func HintNames(hints []Hint) []string {
	out := make([]string, 0, len(hints))
	for _, hint := range hints {
		out = append(out, string(hint))
	}
	return out
}

// sortedHintNames renders hints in lexical order, used where output must not
// depend on the order flags were typed.
func sortedHintNames(hints []Hint) []string {
	out := HintNames(hints)
	sort.Strings(out)
	return out
}
