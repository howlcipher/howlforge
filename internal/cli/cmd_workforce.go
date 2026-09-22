package cli

import (
	"context"
	"fmt"

	"github.com/howlcipher/howlforge/pkg/howlforge"
)

// roleSummary is the list row for a role.
type roleSummary struct {
	ID                   string            `json:"id"`
	Description          string            `json:"description"`
	RequiredCapabilities map[string]string `json:"required_capabilities"`
	PreferredCount       int               `json:"preferred_runtimes"`
	FallbackCount        int               `json:"fallback_runtimes"`
	HandoffRequired      bool              `json:"handoff_required"`
	VerificationRequired bool              `json:"verification_required"`
	VerifierRole         string            `json:"verifier_role,omitempty"`
}

type rolesOutput struct {
	SchemaVersion string        `json:"schema_version"`
	Root          string        `json:"root"`
	Roles         []roleSummary `json:"roles"`
}

func summarizeRole(role *howlforge.Role) roleSummary {
	required := map[string]string{}
	for _, name := range role.RequiredCapabilities.Names() {
		required[name] = role.RequiredCapabilities[name].String()
	}
	return roleSummary{
		ID:                   role.ID,
		Description:          role.Description,
		RequiredCapabilities: required,
		PreferredCount:       len(role.PreferredRuntimes),
		FallbackCount:        len(role.FallbackRuntimes),
		HandoffRequired:      role.HandoffRequired,
		VerificationRequired: role.RequiresVerification(),
		VerifierRole:         role.VerifierRole(),
	}
}

func cmdRoles(ctx context.Context, e *env, name string, args []string) error {
	fs := e.newFlagSet(name)
	pos, err := e.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("`howlforge roles` takes no arguments; did you mean `howlforge role show %s`?", pos[0])
	}
	forge, err := e.load()
	if err != nil {
		return err
	}
	all := forge.Roles()
	if e.global.JSON {
		out := rolesOutput{SchemaVersion: SchemaRoles, Root: forge.Root(), Roles: []roleSummary{}}
		for _, role := range all {
			out.Roles = append(out.Roles, summarizeRole(role))
		}
		return emitJSON(e.stdout, out)
	}
	if len(all) == 0 {
		fmt.Fprintf(e.stdout, "no roles are configured in %s\n", forge.Root())
		return nil
	}
	tw := table(e.stdout)
	fmt.Fprintln(tw, "ROLE\tHANDOFF\tVERIFY\tVERIFIER\tDESCRIPTION")
	for _, role := range all {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			role.ID, yesNo(role.HandoffRequired), yesNo(role.RequiresVerification()),
			dashIfEmpty(role.VerifierRole()), role.Description)
	}
	return tw.Flush()
}

type roleDetailOutput struct {
	SchemaVersion string          `json:"schema_version"`
	Role          *howlforge.Role `json:"role"`
}

func cmdRole(ctx context.Context, e *env, name string, args []string) error {
	fs := e.newFlagSet("role show")
	pos, err := e.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return usagef("usage: howlforge role show <role>")
	}
	if pos[0] != "show" {
		return usagef("unknown subcommand `role %s`: expected `role show <role>`", pos[0])
	}
	if len(pos) != 2 {
		return usagef("usage: howlforge role show <role>")
	}
	forge, err := e.load()
	if err != nil {
		return err
	}
	role, err := forge.Role(pos[1])
	if err != nil {
		return &usageError{err: err}
	}
	if e.global.JSON {
		return emitJSON(e.stdout, roleDetailOutput{SchemaVersion: SchemaRole, Role: role})
	}

	fmt.Fprintf(e.stdout, "Role: %s\n", role.ID)
	fmt.Fprintf(e.stdout, "%s\n\n", role.Description)

	section(e.stdout, "Responsibilities")
	list(e.stdout, role.Responsibilities)
	fmt.Fprintln(e.stdout)

	section(e.stdout, "Required capabilities")
	if len(role.RequiredCapabilities) == 0 {
		fmt.Fprintln(e.stdout, "  (none)")
	} else {
		tw := table(e.stdout)
		for _, capName := range role.RequiredCapabilities.Names() {
			fmt.Fprintf(tw, "  %s\t%s\n", capName, role.RequiredCapabilities[capName])
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	fmt.Fprintln(e.stdout)

	section(e.stdout, "Preferred runtimes")
	list(e.stdout, refStrings(role.PreferredRuntimes))
	fmt.Fprintln(e.stdout)

	section(e.stdout, "Fallback runtimes")
	list(e.stdout, refStrings(role.FallbackRuntimes))
	if len(role.ExcludedRuntimes) > 0 {
		fmt.Fprintln(e.stdout)
		section(e.stdout, "Excluded runtimes")
		list(e.stdout, refStrings(role.ExcludedRuntimes))
	}
	fmt.Fprintln(e.stdout)

	section(e.stdout, "Contracts")
	field(e.stdout, "handoff required", "%s", yesNo(role.HandoffRequired))
	field(e.stdout, "verification required", "%s", yesNo(role.RequiresVerification()))
	if role.VerifierRole() != "" {
		field(e.stdout, "verifier role", "%s", role.VerifierRole())
	}
	if role.Verification != nil && role.Verification.IndependentRuntime != "" {
		field(e.stdout, "independent runtime", "%s", role.Verification.IndependentRuntime)
	}
	field(e.stdout, "source", "%s", role.SourcePath)
	return nil
}

func refStrings(refs []howlforge.RuntimeRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.String())
	}
	return out
}

type runtimeSummary struct {
	ID            string            `json:"id"`
	Provider      string            `json:"provider"`
	Model         string            `json:"model"`
	Runtime       string            `json:"runtime"`
	Enabled       bool              `json:"enabled"`
	State         string            `json:"state"`
	EconomicClass string            `json:"economic_class,omitempty"`
	Locality      string            `json:"locality,omitempty"`
	Capabilities  map[string]string `json:"capabilities"`
}

type runtimesOutput struct {
	SchemaVersion string           `json:"schema_version"`
	Root          string           `json:"root"`
	StatePath     string           `json:"state_path,omitempty"`
	Runtimes      []runtimeSummary `json:"runtimes"`
}

func summarizeRuntime(rt *howlforge.Runtime) runtimeSummary {
	caps := map[string]string{}
	for _, capName := range rt.Capabilities.Names() {
		caps[capName] = rt.Capabilities[capName].String()
	}
	return runtimeSummary{
		ID:            rt.ID,
		Provider:      rt.Provider,
		Model:         rt.Model,
		Runtime:       rt.Client,
		Enabled:       rt.State.Enabled,
		EconomicClass: string(rt.EconomicClass),
		Locality:      string(rt.Locality),
		Capabilities:  caps,
	}
}

func cmdRuntimes(ctx context.Context, e *env, name string, args []string) error {
	fs := e.newFlagSet(name)
	enabledOnly := fs.Bool("enabled", false, "list only runtimes the operator has enabled")
	pos, err := e.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("`howlforge runtimes` takes no arguments; did you mean `howlforge runtime show %s`?", pos[0])
	}
	forge, err := e.load()
	if err != nil {
		return err
	}
	all := forge.Runtimes()
	out := runtimesOutput{
		SchemaVersion: SchemaRuntimes,
		Root:          forge.Root(),
		StatePath:     forge.StatePath(),
		Runtimes:      []runtimeSummary{},
	}
	var rows []*howlforge.Runtime
	for _, rt := range all {
		if *enabledOnly && !rt.State.Enabled {
			continue
		}
		rows = append(rows, rt)
	}
	if e.global.JSON {
		for _, rt := range rows {
			summary := summarizeRuntime(rt)
			summary.State = string(forge.AvailabilityOf(rt.ID).EffectiveState(e.global.now))
			out.Runtimes = append(out.Runtimes, summary)
		}
		return emitJSON(e.stdout, out)
	}
	if len(rows) == 0 {
		fmt.Fprintf(e.stdout, "no runtimes are configured in %s\n", forge.Root())
		return nil
	}
	tw := table(e.stdout)
	fmt.Fprintln(tw, "RUNTIME\tPROVIDER\tMODEL\tCLIENT\tENABLED\tSTATE")
	for _, rt := range rows {
		state := forge.AvailabilityOf(rt.ID).EffectiveState(e.global.now)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			rt.ID, rt.Provider, rt.Model, rt.Client, yesNo(rt.State.Enabled), state)
	}
	return tw.Flush()
}

type runtimeDetailOutput struct {
	SchemaVersion string             `json:"schema_version"`
	Runtime       *howlforge.Runtime `json:"runtime"`
	State         string             `json:"state"`
	StateReason   string             `json:"state_reason,omitempty"`
	FillsRoles    []string           `json:"fills_roles"`
}

func cmdRuntime(ctx context.Context, e *env, name string, args []string) error {
	fs := e.newFlagSet("runtime show")
	pos, err := e.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return usagef("usage: howlforge runtime show <runtime>")
	}
	if pos[0] != "show" {
		return usagef("unknown subcommand `runtime %s`: expected `runtime show <runtime>`", pos[0])
	}
	if len(pos) != 2 {
		return usagef("usage: howlforge runtime show <runtime>")
	}
	forge, err := e.load()
	if err != nil {
		return err
	}
	rt, err := forge.Runtime(pos[1])
	if err != nil {
		return &usageError{err: err}
	}
	report := forge.AvailabilityOf(rt.ID)
	state := report.EffectiveState(e.global.now)

	// Which roles this runtime can actually fill is the question an operator
	// asks about a runtime, and it is derived from the same matcher the
	// selection commands use.
	var fills []string
	for _, role := range forge.Roles() {
		candidates, err := forge.Match(ctx, howlforge.MatchRequest{Role: role.ID, Now: e.global.now})
		if err != nil {
			continue
		}
		for _, candidate := range candidates {
			if candidate.RuntimeID == rt.ID {
				fills = append(fills, role.ID)
				break
			}
		}
	}

	if e.global.JSON {
		return emitJSON(e.stdout, runtimeDetailOutput{
			SchemaVersion: SchemaRuntime,
			Runtime:       rt,
			State:         string(state),
			StateReason:   report.Reason,
			FillsRoles:    fills,
		})
	}

	fmt.Fprintf(e.stdout, "Runtime: %s\n", rt.ID)
	if rt.Description != "" {
		fmt.Fprintf(e.stdout, "%s\n", rt.Description)
	}
	fmt.Fprintln(e.stdout)
	section(e.stdout, "Identity")
	field(e.stdout, "provider", "%s", rt.Provider)
	field(e.stdout, "model", "%s", rt.Model)
	field(e.stdout, "client", "%s", rt.Client)
	field(e.stdout, "economic class", "%s", dashIfEmpty(string(rt.EconomicClass)))
	field(e.stdout, "locality", "%s", dashIfEmpty(string(rt.Locality)))
	fmt.Fprintln(e.stdout)

	section(e.stdout, "Capabilities")
	if len(rt.Capabilities) == 0 {
		fmt.Fprintln(e.stdout, "  (none declared)")
	} else {
		tw := table(e.stdout)
		for _, capName := range rt.Capabilities.Names() {
			fmt.Fprintf(tw, "  %s\t%s\n", capName, rt.Capabilities[capName])
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	fmt.Fprintln(e.stdout)

	section(e.stdout, "State")
	field(e.stdout, "enabled", "%s", yesNo(rt.State.Enabled))
	field(e.stdout, "availability", "%s", state)
	if report.Reason != "" {
		field(e.stdout, "reason", "%s", report.Reason)
	}
	if report.ResetAt != nil {
		field(e.stdout, "reset at", "%s", report.ResetAt.UTC().Format("2006-01-02T15:04:05Z"))
	}
	fmt.Fprintln(e.stdout)

	section(e.stdout, "Currently eligible for")
	list(e.stdout, fills)
	fmt.Fprintf(e.stdout, "\n  source: %s\n", rt.SourcePath)
	return nil
}

type personasOutput struct {
	SchemaVersion string               `json:"schema_version"`
	Personas      []*howlforge.Persona `json:"personas"`
}

func cmdPersonas(ctx context.Context, e *env, name string, args []string) error {
	fs := e.newFlagSet(name)
	if _, err := e.parseArgs(fs, args); err != nil {
		return err
	}
	forge, err := e.load()
	if err != nil {
		return err
	}
	all := forge.Personas()
	if e.global.JSON {
		if all == nil {
			all = []*howlforge.Persona{}
		}
		return emitJSON(e.stdout, personasOutput{SchemaVersion: SchemaPersonas, Personas: all})
	}
	if len(all) == 0 {
		fmt.Fprintln(e.stdout, "no personas are configured (personas are optional)")
		return nil
	}
	tw := table(e.stdout)
	fmt.Fprintln(tw, "PERSONA\tROLE\tDISPLAY NAME")
	for _, persona := range all {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", persona.ID, persona.Role, dashIfEmpty(persona.DisplayName))
	}
	return tw.Flush()
}

type personaDetailOutput struct {
	SchemaVersion string             `json:"schema_version"`
	Persona       *howlforge.Persona `json:"persona"`
	ResolvedRole  *howlforge.Role    `json:"resolved_role"`
}

func cmdPersona(ctx context.Context, e *env, name string, args []string) error {
	fs := e.newFlagSet("persona show")
	pos, err := e.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 || pos[0] != "show" {
		return usagef("usage: howlforge persona show <persona>")
	}
	forge, err := e.load()
	if err != nil {
		return err
	}
	persona, err := forge.Persona(pos[1])
	if err != nil {
		return &usageError{err: err}
	}
	role, err := forge.Role(persona.Role)
	if err != nil {
		return &configError{err: err}
	}
	if e.global.JSON {
		return emitJSON(e.stdout, personaDetailOutput{
			SchemaVersion: SchemaPersona, Persona: persona, ResolvedRole: role,
		})
	}
	fmt.Fprintf(e.stdout, "Persona: %s\n", persona.ID)
	if persona.DisplayName != "" {
		field(e.stdout, "display name", "%s", persona.DisplayName)
	}
	field(e.stdout, "role", "%s", role.ID)
	if persona.Description != "" {
		fmt.Fprintf(e.stdout, "\n%s\n", persona.Description)
	}
	if len(persona.StyleNotes) > 0 {
		fmt.Fprintln(e.stdout)
		section(e.stdout, "Style notes")
		list(e.stdout, persona.StyleNotes)
	}
	fmt.Fprintf(e.stdout, "\nA persona resolves to a role and stops there. It never selects a runtime.\n")
	return nil
}

type capabilitiesOutput struct {
	SchemaVersion string   `json:"schema_version"`
	Levels        []string `json:"levels"`
	Capabilities  []string `json:"capabilities"`
}

func cmdCapabilities(ctx context.Context, e *env, name string, args []string) error {
	fs := e.newFlagSet(name)
	if _, err := e.parseArgs(fs, args); err != nil {
		return err
	}
	forge, err := e.load()
	if err != nil {
		return err
	}
	if e.global.JSON {
		return emitJSON(e.stdout, capabilitiesOutput{
			SchemaVersion: SchemaCapabilities,
			Levels:        howlforge.LevelNames(),
			Capabilities:  forge.Capabilities(),
		})
	}
	section(e.stdout, "Levels, ascending")
	list(e.stdout, howlforge.LevelNames())
	fmt.Fprintln(e.stdout)
	section(e.stdout, "Capability vocabulary")
	list(e.stdout, forge.Capabilities())
	return nil
}
