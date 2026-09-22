package cli

import (
	"context"
	"fmt"

	"github.com/howlcipher/howlforge/pkg/howlforge"
)

type matchOutput struct {
	SchemaVersion string `json:"schema_version"`
	*howlforge.MatchResult
}

func cmdMatch(ctx context.Context, e *env, name string, args []string) error {
	fs := e.newFlagSet(name)
	var selection selectionFlags
	selection.register(fs)
	showRejected := fs.Bool("rejected", false, "also show every rejected runtime and the reason")
	pos, err := e.parseArgs(fs, args)
	if err != nil {
		return err
	}
	role := ""
	switch len(pos) {
	case 0:
		if selection.persona == "" {
			return usagef("usage: howlforge match <role> [flags], or pass --persona <persona>")
		}
	case 1:
		role = pos[0]
	default:
		return usagef("usage: howlforge match <role> [flags]")
	}

	forge, err := e.load()
	if err != nil {
		return err
	}
	req, err := selection.request(e, role)
	if err != nil {
		return err
	}
	result, err := forge.MatchResult(ctx, req)
	if err != nil {
		return &usageError{err: err}
	}

	if e.global.JSON {
		if err := emitJSON(e.stdout, matchOutput{SchemaVersion: SchemaMatch, MatchResult: result}); err != nil {
			return err
		}
	} else {
		renderMatch(e, result, *showRejected)
	}
	// An empty candidate list is a real answer, not a crash, but automation
	// needs to branch on it without parsing output.
	if len(result.Candidates) == 0 {
		return exitWith(ExitNoCandidate, "")
	}
	return nil
}

func renderMatch(e *env, result *howlforge.MatchResult, showRejected bool) {
	fmt.Fprintf(e.stdout, "Role: %s\n", result.Role)
	if len(result.Hints) > 0 {
		fmt.Fprintf(e.stdout, "Preference: %s\n", joinSorted(result.Hints))
	}
	fmt.Fprintln(e.stdout)

	if len(result.Candidates) == 0 {
		fmt.Fprintln(e.stdout, "No eligible runtime can fill this role right now.")
	} else {
		tw := table(e.stdout)
		fmt.Fprintln(tw, "  #\tRUNTIME\tCANDIDATE\tSTATE\tPREFERENCE")
		for _, candidate := range result.Candidates {
			preference := string(candidate.PreferenceTier)
			if candidate.PreferenceTier != howlforge.TierEligible {
				preference = fmt.Sprintf("%s #%d", candidate.PreferenceTier, candidate.PreferenceIndex+1)
			}
			fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\n",
				candidate.Rank, candidate.RuntimeID, candidate.Label, candidate.State, preference)
		}
		_ = tw.Flush()
	}

	blocked := 0
	for _, rejection := range result.Rejected {
		if rejection.Stage == howlforge.StageAvailability {
			blocked++
		}
	}
	if blocked > 0 && !showRejected {
		fmt.Fprintf(e.stdout, "\n%d capability eligible runtime(s) are currently unavailable; run `howlforge availability %s` for detail.\n",
			blocked, result.Role)
	}
	if showRejected && len(result.Rejected) > 0 {
		fmt.Fprintln(e.stdout)
		section(e.stdout, "Rejected")
		tw := table(e.stdout)
		fmt.Fprintln(tw, "  RUNTIME\tSTAGE\tREASON")
		for _, rejection := range result.Rejected {
			fmt.Fprintf(tw, "  %s\t%s\t%s\n",
				rejection.RuntimeID, rejection.Stage, dashIfEmpty(rejection.Detail))
		}
		_ = tw.Flush()
	}
}

type explainOutput struct {
	SchemaVersion string `json:"schema_version"`
	*howlforge.Explanation
}

func cmdExplain(ctx context.Context, e *env, name string, args []string) error {
	fs := e.newFlagSet(name)
	var selection selectionFlags
	selection.register(fs)
	pos, err := e.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return usagef("usage: howlforge explain <role> <runtime>")
	}
	forge, err := e.load()
	if err != nil {
		return err
	}
	req, err := selection.request(e, pos[0])
	if err != nil {
		return err
	}
	explanation, err := forge.Explain(ctx, req, pos[1])
	if err != nil {
		return &usageError{err: err}
	}
	if e.global.JSON {
		return emitJSON(e.stdout, explainOutput{SchemaVersion: SchemaExplain, Explanation: explanation})
	}
	renderExplanation(e, explanation)
	return nil
}

func renderExplanation(e *env, x *howlforge.Explanation) {
	fmt.Fprintf(e.stdout, "Role: %s\n", x.Role)

	section(e.stdout, "Required")
	tw := table(e.stdout)
	for _, check := range x.Checks {
		suffix := ""
		if check.Source == "request" {
			suffix = "  (requested)"
		}
		fmt.Fprintf(tw, "  %s\t%s%s\n", check.Capability, check.Required, suffix)
	}
	_ = tw.Flush()

	fmt.Fprintf(e.stdout, "Candidate:\n  %s  (%s)\n", x.RuntimeID, x.Label)

	section(e.stdout, "Matched")
	tw = table(e.stdout)
	for _, check := range x.Checks {
		mark := "ok"
		if !check.Satisfied {
			mark = "SHORT"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", check.Capability, check.Offered, mark)
	}
	_ = tw.Flush()

	section(e.stdout, "Preference")
	fmt.Fprintf(e.stdout, "  %s\n", x.PreferenceLabel)

	section(e.stdout, "State")
	fmt.Fprintf(e.stdout, "  %s", x.State)
	if x.StateReason != "" {
		fmt.Fprintf(e.stdout, "  (%s)", x.StateReason)
	}
	fmt.Fprintln(e.stdout)

	fmt.Fprintln(e.stdout)
	if x.Eligible {
		fmt.Fprintf(e.stdout, "Eligible: yes\n")
		fmt.Fprintf(e.stdout, "Rank: %d\n", x.Rank)
		return
	}
	fmt.Fprintf(e.stdout, "Eligible: no\n")
	if x.Rejection != nil {
		fmt.Fprintf(e.stdout, "Rejected: %s\n", x.RuntimeID)
		fmt.Fprintf(e.stdout, "Reason:\n  %s\n", x.Rejection.Detail)
		fmt.Fprintf(e.stdout, "  stage %s, code %s\n", x.Rejection.Stage, x.Rejection.Reason)
	}
}

type availabilityOutput struct {
	SchemaVersion string `json:"schema_version"`
	*howlforge.AvailabilityResult
}

type fleetAvailabilityOutput struct {
	SchemaVersion string                 `json:"schema_version"`
	StatePath     string                 `json:"state_path,omitempty"`
	Runtimes      []fleetAvailabilityRow `json:"runtimes"`
}

type fleetAvailabilityRow struct {
	RuntimeID string `json:"runtime_id"`
	Label     string `json:"label"`
	Enabled   bool   `json:"enabled"`
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	Usable    bool   `json:"usable"`
	ResetAt   string `json:"reset_at,omitempty"`
}

func cmdAvailability(ctx context.Context, e *env, name string, args []string) error {
	fs := e.newFlagSet(name)
	var selection selectionFlags
	selection.register(fs)
	pos, err := e.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return usagef("usage: howlforge availability [role]")
	}
	forge, err := e.load()
	if err != nil {
		return err
	}

	// With no role, report the whole fleet. With a role, report only the
	// runtimes that could actually fill it.
	if len(pos) == 0 && selection.persona == "" {
		return renderFleetAvailability(e, forge)
	}
	role := ""
	if len(pos) == 1 {
		role = pos[0]
	}

	req, err := selection.request(e, role)
	if err != nil {
		return err
	}
	result, err := forge.Availability(ctx, req)
	if err != nil {
		return &usageError{err: err}
	}
	if e.global.JSON {
		if err := emitJSON(e.stdout, availabilityOutput{
			SchemaVersion: SchemaAvailability, AvailabilityResult: result,
		}); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(e.stdout, "Role: %s\n\n", result.Role)
		tw := table(e.stdout)
		fmt.Fprintln(tw, "  RUNTIME\tCANDIDATE\tSTATE\tNOTE")
		for _, entry := range result.Entries {
			note := ""
			switch {
			case entry.EligibleAfterReset && entry.ResetAt != nil:
				note = "eligible after reset at " + entry.ResetAt.UTC().Format("2006-01-02T15:04:05Z")
			case entry.EligibleAfterReset:
				note = "eligible after reset"
			case !entry.Usable:
				note = "not usable"
			case entry.Rank > 0:
				note = fmt.Sprintf("candidate #%d", entry.Rank)
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", entry.RuntimeID, entry.Label, entry.State, note)
		}
		_ = tw.Flush()
		fmt.Fprintln(e.stdout)
		if result.Recommended != "" {
			fmt.Fprintf(e.stdout, "recommended runtime: %s\n", result.Recommended)
		}
		fmt.Fprintf(e.stdout, "reason: %s\n", result.Reason)
	}
	if result.Recommended == "" {
		return exitWith(ExitNoCandidate, "")
	}
	return nil
}

func renderFleetAvailability(e *env, forge *howlforge.Forge) error {
	out := fleetAvailabilityOutput{
		SchemaVersion: SchemaAvailability,
		StatePath:     forge.StatePath(),
		Runtimes:      []fleetAvailabilityRow{},
	}
	usable := 0
	for _, rt := range forge.Runtimes() {
		report := forge.AvailabilityOf(rt.ID)
		state := report.EffectiveState(e.global.now)
		row := fleetAvailabilityRow{
			RuntimeID: rt.ID,
			Label:     rt.Label(),
			Enabled:   rt.State.Enabled,
			State:     string(state),
			Reason:    report.Reason,
			Usable:    rt.State.Enabled && state.Usable(),
		}
		if report.ResetAt != nil {
			row.ResetAt = report.ResetAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		if row.Usable {
			usable++
		}
		out.Runtimes = append(out.Runtimes, row)
	}
	if e.global.JSON {
		if err := emitJSON(e.stdout, out); err != nil {
			return err
		}
	} else {
		if len(out.Runtimes) == 0 {
			fmt.Fprintln(e.stdout, "no runtimes are configured")
		} else {
			tw := table(e.stdout)
			fmt.Fprintln(tw, "RUNTIME\tCANDIDATE\tENABLED\tSTATE\tRESET AT")
			for _, row := range out.Runtimes {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					row.RuntimeID, row.Label, yesNo(row.Enabled), row.State, dashIfEmpty(row.ResetAt))
			}
			_ = tw.Flush()
		}
		if forge.StatePath() == "" {
			fmt.Fprintf(e.stdout, "\nNo availability state file was read, so every runtime reads as UNKNOWN.\n")
		}
	}
	if usable == 0 {
		return exitWith(ExitNoCandidate, "")
	}
	return nil
}
