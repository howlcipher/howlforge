// Package capabilities defines HowlForge's aptitude vocabulary and its ordinal
// scale.
//
// This vocabulary describes what a worker is GOOD AT. It is deliberately
// separate from the authority vocabulary (filesystem:repository, git:push and
// friends) that HowlFrame owns. A runtime declaring coding=very_high says
// nothing about whether it is permitted to write to a repository.
package capabilities

import (
	"fmt"
	"sort"
	"strings"
)

// Level is an ordinal capability rating. The zero value is LevelNone, which
// means an absent capability compares correctly without special casing.
type Level int

const (
	LevelNone Level = iota
	LevelLow
	LevelMedium
	LevelHigh
	LevelVeryHigh
)

var levelNames = [...]string{"none", "low", "medium", "high", "very_high"}

// String renders the canonical name of the level.
func (l Level) String() string {
	if l < LevelNone || int(l) >= len(levelNames) {
		return fmt.Sprintf("Level(%d)", int(l))
	}
	return levelNames[l]
}

// Valid reports whether the level is inside the defined scale.
func (l Level) Valid() bool {
	return l >= LevelNone && int(l) < len(levelNames)
}

// LevelNames returns the scale in ascending order.
func LevelNames() []string {
	out := make([]string, len(levelNames))
	copy(out, levelNames[:])
	return out
}

// ParseLevel converts a canonical level name into a Level. Parsing is strict:
// case and surrounding whitespace are normalized, but no synonyms are accepted,
// so configuration cannot drift into an undefined scale.
func ParseLevel(s string) (Level, error) {
	normalized := strings.ToLower(strings.TrimSpace(s))
	for i, name := range levelNames {
		if name == normalized {
			return Level(i), nil
		}
	}
	return LevelNone, fmt.Errorf("unknown capability level %q: expected one of %s",
		s, strings.Join(LevelNames(), ", "))
}

// Satisfies reports whether an offered level meets a required level.
func Satisfies(required, offered Level) bool {
	return offered >= required
}

// UnmarshalYAML decodes a level from its canonical name.
func (l *Level) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var raw string
	if err := unmarshal(&raw); err != nil {
		return fmt.Errorf("capability level must be a string: %w", err)
	}
	parsed, err := ParseLevel(raw)
	if err != nil {
		return err
	}
	*l = parsed
	return nil
}

// MarshalYAML encodes a level as its canonical name.
func (l Level) MarshalYAML() (interface{}, error) {
	return l.String(), nil
}

// MarshalJSON encodes a level as its canonical name so JSON output stays
// human-readable and stable across releases.
func (l Level) MarshalJSON() ([]byte, error) {
	if !l.Valid() {
		return nil, fmt.Errorf("cannot marshal invalid capability level %d", int(l))
	}
	return []byte(`"` + l.String() + `"`), nil
}

// UnmarshalJSON decodes a level from its canonical name.
func (l *Level) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(string(data), `"`)
	parsed, err := ParseLevel(raw)
	if err != nil {
		return err
	}
	*l = parsed
	return nil
}

// Set is a capability profile: a mapping from capability name to level.
type Set map[string]Level

// Get returns the level recorded for a capability. An undeclared capability
// reads as LevelNone rather than an error, which is what makes an omitted
// capability fail a requirement instead of silently passing it.
func (s Set) Get(name string) Level {
	if s == nil {
		return LevelNone
	}
	return s[name]
}

// Names returns the declared capability names in lexical order. Callers rely on
// this for deterministic rendering.
func (s Set) Names() []string {
	out := make([]string, 0, len(s))
	for name := range s {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Clone returns an independent copy of the set.
func (s Set) Clone() Set {
	if s == nil {
		return nil
	}
	out := make(Set, len(s))
	for name, level := range s {
		out[name] = level
	}
	return out
}

// Shortfall reports the first capability, in lexical order, where the offered
// set fails to meet required. It returns the capability name, the required
// level and the offered level. ok is false when every requirement is met.
func (s Set) Shortfall(required Set) (name string, want, have Level, ok bool) {
	for _, capName := range required.Names() {
		want := required[capName]
		have := s.Get(capName)
		if !Satisfies(want, have) {
			return capName, want, have, false
		}
	}
	return "", LevelNone, LevelNone, true
}

// Surplus totals how far the offered set exceeds the required set across every
// required capability. It is used only as a late, deterministic ranking
// tie-break, never as an eligibility test.
func (s Set) Surplus(required Set) int {
	total := 0
	for capName, want := range required {
		total += int(s.Get(capName)) - int(want)
	}
	return total
}
