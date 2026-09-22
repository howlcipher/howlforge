package capabilities

import (
	"fmt"
	"sort"
	"strings"
)

// Built-in capability names. Keeping these as constants means a typo in Go code
// is a compile error rather than a silently unmatched string.
const (
	Reasoning      = "reasoning"
	Coding         = "coding"
	ToolUse        = "tool_use"
	LongHorizon    = "long_horizon"
	ContextWindow  = "context_window"
	Research       = "research"
	Planning       = "planning"
	Review         = "review"
	Security       = "security"
	Testing        = "testing"
	Documentation  = "documentation"
	Speed          = "speed"
	CostEfficiency = "cost_efficiency"
	LocalExecution = "local_execution"
)

var builtinNames = []string{
	Reasoning,
	Coding,
	ToolUse,
	LongHorizon,
	ContextWindow,
	Research,
	Planning,
	Review,
	Security,
	Testing,
	Documentation,
	Speed,
	CostEfficiency,
	LocalExecution,
}

// Vocabulary is the set of capability names configuration may use. It exists so
// capabilities cannot become arbitrary strings scattered through config, while
// still allowing an operator to extend the list deliberately.
type Vocabulary struct {
	names map[string]bool
}

// DefaultVocabulary returns the built-in controlled vocabulary.
func DefaultVocabulary() *Vocabulary {
	v := &Vocabulary{names: make(map[string]bool, len(builtinNames))}
	for _, name := range builtinNames {
		v.names[name] = true
	}
	return v
}

// Extend returns a vocabulary with additional names admitted. Extension is
// additive only: a built-in name can never be removed, so a config file cannot
// quietly redefine the shared vocabulary out from under another repository.
func (v *Vocabulary) Extend(extra []string) (*Vocabulary, error) {
	out := &Vocabulary{names: make(map[string]bool, len(v.names)+len(extra))}
	for name := range v.names {
		out.names[name] = true
	}
	for _, name := range extra {
		normalized := strings.TrimSpace(name)
		if normalized == "" {
			return nil, fmt.Errorf("capability vocabulary contains an empty name")
		}
		if err := validateName(normalized); err != nil {
			return nil, err
		}
		out.names[normalized] = true
	}
	return out, nil
}

// validateName keeps extension names in the same lower_snake_case shape as the
// built-ins, so rendering and sorting stay predictable.
func validateName(name string) error {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return fmt.Errorf("invalid capability name %q: use lower_snake_case (a-z, 0-9, underscore)", name)
		}
	}
	return nil
}

// Known reports whether a capability name is admitted by this vocabulary.
func (v *Vocabulary) Known(name string) bool {
	if v == nil {
		return false
	}
	return v.names[name]
}

// Names returns every admitted capability name in lexical order.
func (v *Vocabulary) Names() []string {
	out := make([]string, 0, len(v.names))
	for name := range v.names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// BuiltinNames returns the built-in vocabulary in declaration order, which is
// the order documentation and CLI help present it in.
func BuiltinNames() []string {
	out := make([]string, len(builtinNames))
	copy(out, builtinNames)
	return out
}

// Validate checks every capability name in a set against the vocabulary and
// returns a diagnostic that suggests the closest known name on a miss.
func (v *Vocabulary) Validate(set Set) error {
	for _, name := range set.Names() {
		if v.Known(name) {
			continue
		}
		if suggestion, ok := v.suggest(name); ok {
			return fmt.Errorf("unknown capability %q (did you mean %q?)", name, suggestion)
		}
		return fmt.Errorf("unknown capability %q: known capabilities are %s",
			name, strings.Join(v.Names(), ", "))
	}
	return nil
}

// suggest finds the closest admitted name within a small edit distance. It is a
// diagnostic aid only and never affects matching.
func (v *Vocabulary) suggest(name string) (string, bool) {
	best := ""
	bestDistance := 0
	for _, candidate := range v.Names() {
		distance := editDistance(name, candidate)
		if distance > 2 {
			continue
		}
		if best == "" || distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return best, best != ""
}

// editDistance is the standard Levenshtein distance over runes.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
