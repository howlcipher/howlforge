package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/howlcipher/howlforge/pkg/howlforge"
)

// requireFlag collects repeatable --require capability=level pairs.
type requireFlag struct {
	set howlforge.CapabilitySet
}

func (r *requireFlag) String() string {
	if r == nil || len(r.set) == 0 {
		return ""
	}
	parts := make([]string, 0, len(r.set))
	for _, name := range r.set.Names() {
		parts = append(parts, fmt.Sprintf("%s=%s", name, r.set[name]))
	}
	return strings.Join(parts, ",")
}

func (r *requireFlag) Set(value string) error {
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, levelText, found := strings.Cut(item, "=")
		if !found {
			return fmt.Errorf("expected capability=level, got %q", item)
		}
		level, err := howlforge.ParseLevel(levelText)
		if err != nil {
			return err
		}
		if r.set == nil {
			r.set = howlforge.CapabilitySet{}
		}
		r.set[strings.TrimSpace(name)] = level
	}
	return nil
}

// stringsFlag collects a repeatable, comma splittable string flag.
type stringsFlag struct {
	values []string
}

func (s *stringsFlag) String() string {
	if s == nil {
		return ""
	}
	return strings.Join(s.values, ",")
}

func (s *stringsFlag) Set(value string) error {
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		s.values = append(s.values, item)
	}
	return nil
}

// selectionFlags are the flags shared by match, explain and availability.
type selectionFlags struct {
	require requireFlag
	prefer  stringsFlag
	exclude stringsFlag
	persona string
}

func (s *selectionFlags) register(fs *flag.FlagSet) {
	fs.Var(&s.require, "require", "capability floor as capability=level, repeatable")
	fs.Var(&s.prefer, "prefer", "ranking preference: quality, speed, cost, local, privacy or context, repeatable")
	fs.Var(&s.exclude, "exclude", "runtime id to refuse for this match, repeatable")
	fs.StringVar(&s.persona, "persona", "", "resolve the role from this persona")
}

// request builds a library request from the parsed flags.
func (s *selectionFlags) request(e *env, role string) (howlforge.MatchRequest, error) {
	hints, err := howlforge.ParseHints(s.prefer.values)
	if err != nil {
		return howlforge.MatchRequest{}, &usageError{err: err}
	}
	return howlforge.MatchRequest{
		Role:    role,
		Persona: s.persona,
		Require: s.require.set,
		Prefer:  hints,
		Exclude: s.exclude.values,
		Now:     e.global.now,
	}, nil
}
