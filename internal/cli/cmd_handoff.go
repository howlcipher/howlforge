package cli

import (
	"context"
	"fmt"

	"github.com/howlcipher/howlforge/internal/handoff"
	"github.com/howlcipher/howlforge/internal/resume"
)

type handoffValidateOutput struct {
	SchemaVersion string               `json:"schema_version"`
	Path          string               `json:"path"`
	Valid         bool                 `json:"valid"`
	PresentFiles  []string             `json:"present_files"`
	MissingFiles  []string             `json:"missing_files,omitempty"`
	Diagnostics   []handoff.Diagnostic `json:"diagnostics"`
}

type handoffInspectOutput struct {
	SchemaVersion string                `json:"schema_version"`
	Path          string                `json:"path"`
	Valid         bool                  `json:"valid"`
	Checkpoint    *handoff.Checkpoint   `json:"checkpoint,omitempty"`
	ChangedFiles  []string              `json:"changed_files"`
	Decisions     []handoff.Decision    `json:"decisions"`
	Verification  *handoff.Verification `json:"verification,omitempty"`
	Diagnostics   []handoff.Diagnostic  `json:"diagnostics"`
}

func cmdHandoff(ctx context.Context, e *env, name string, args []string) error {
	fs := e.newFlagSet("handoff")
	pos, err := e.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return usagef("usage: howlforge handoff <validate|inspect> <directory>")
	}
	sub := pos[0]
	switch sub {
	case "validate", "inspect":
	default:
		return usagef("unknown subcommand `handoff %s`: expected validate or inspect", sub)
	}
	if len(pos) != 2 {
		return usagef("usage: howlforge handoff %s <directory>", sub)
	}
	bundle, err := handoff.Load(pos[1])
	if err != nil {
		return exitWith(ExitBadHandoff, "%v", err)
	}
	if sub == "validate" {
		return renderHandoffValidate(e, bundle)
	}
	return renderHandoffInspect(e, bundle)
}

func renderHandoffValidate(e *env, bundle *handoff.Bundle) error {
	if e.global.JSON {
		if err := emitJSON(e.stdout, handoffValidateOutput{
			SchemaVersion: SchemaHandoff,
			Path:          bundle.Path,
			Valid:         bundle.Valid(),
			PresentFiles:  bundle.Present,
			MissingFiles:  bundle.Missing,
			Diagnostics:   bundle.Diagnostics,
		}); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(e.stdout, "Handoff: %s\n\n", bundle.Path)
		tw := table(e.stdout)
		for _, required := range handoff.RequiredFiles {
			status := "missing"
			for _, present := range bundle.Present {
				if present == required {
					status = "present"
					break
				}
			}
			fmt.Fprintf(tw, "  %s\t%s\n", required, status)
		}
		_ = tw.Flush()
		fmt.Fprintln(e.stdout)
		if len(bundle.Diagnostics) == 0 {
			fmt.Fprintln(e.stdout, "VALID")
			fmt.Fprintln(e.stdout, "the bundle satisfies howlforge.checkpoint/v1")
			return nil
		}
		for _, diagnostic := range bundle.Diagnostics {
			fmt.Fprintf(e.stdout, "  %s\n", diagnostic)
		}
		fmt.Fprintln(e.stdout)
		if bundle.Valid() {
			fmt.Fprintf(e.stdout, "VALID with %d warning(s)\n", len(bundle.Diagnostics))
		} else {
			fmt.Fprintf(e.stdout, "INVALID: %d error(s)\n", len(bundle.Errors()))
		}
	}
	if !bundle.Valid() {
		return exitWith(ExitBadHandoff, "")
	}
	return nil
}

func renderHandoffInspect(e *env, bundle *handoff.Bundle) error {
	if e.global.JSON {
		out := handoffInspectOutput{
			SchemaVersion: SchemaHandoff,
			Path:          bundle.Path,
			Valid:         bundle.Valid(),
			Checkpoint:    bundle.Checkpoint,
			ChangedFiles:  bundle.ChangedFiles,
			Decisions:     bundle.Decisions,
			Verification:  bundle.Verification,
			Diagnostics:   bundle.Diagnostics,
		}
		if out.ChangedFiles == nil {
			out.ChangedFiles = []string{}
		}
		if out.Decisions == nil {
			out.Decisions = []handoff.Decision{}
		}
		if err := emitJSON(e.stdout, out); err != nil {
			return err
		}
		if !bundle.Valid() {
			return exitWith(ExitBadHandoff, "")
		}
		return nil
	}

	checkpoint := bundle.Checkpoint
	if checkpoint == nil {
		fmt.Fprintf(e.stdout, "Handoff: %s\n\nno readable checkpoint.json\n", bundle.Path)
		return exitWith(ExitBadHandoff, "")
	}

	fmt.Fprintf(e.stdout, "Handoff: %s\n", bundle.Path)
	fmt.Fprintln(e.stdout)
	section(e.stdout, "Task")
	field(e.stdout, "task id", "%s", checkpoint.TaskID)
	field(e.stdout, "status", "%s", checkpoint.Status)
	field(e.stdout, "handoff reason", "%s", dashIfEmpty(string(checkpoint.Reason)))
	field(e.stdout, "safe to resume", "%s", yesNo(checkpoint.SafeToResume))
	fmt.Fprintln(e.stdout)

	section(e.stdout, "Worker")
	field(e.stdout, "role", "%s", checkpoint.EffectiveRole())
	field(e.stdout, "runtime", "%s", checkpoint.EffectiveClient())
	field(e.stdout, "model", "%s", checkpoint.EffectiveModel())
	if checkpoint.Worker != nil && checkpoint.Worker.PersonaID != "" {
		field(e.stdout, "persona", "%s", checkpoint.Worker.PersonaID)
	}
	fmt.Fprintln(e.stdout)

	section(e.stdout, "Repository")
	field(e.stdout, "last verified commit", "%s", checkpoint.EffectiveCommit())
	if checkpoint.Repo != nil {
		field(e.stdout, "branch", "%s", dashIfEmpty(checkpoint.Repo.Branch))
		field(e.stdout, "working tree", "%s", map[bool]string{true: "dirty", false: "clean"}[checkpoint.Repo.Dirty])
	}
	fmt.Fprintln(e.stdout)

	section(e.stdout, "Completed")
	list(e.stdout, checkpoint.Completed)
	fmt.Fprintln(e.stdout)

	section(e.stdout, "Remaining")
	list(e.stdout, checkpoint.Remaining)
	fmt.Fprintln(e.stdout)

	section(e.stdout, "Changed files")
	list(e.stdout, bundle.ChangedFiles)
	fmt.Fprintln(e.stdout)

	section(e.stdout, "Decisions")
	if len(bundle.Decisions) == 0 {
		fmt.Fprintln(e.stdout, "  (none)")
	} else {
		for _, decision := range bundle.Decisions {
			reversible := "reversible"
			if !decision.Reversible {
				reversible = "IRREVERSIBLE"
			}
			fmt.Fprintf(e.stdout, "  - [%s] %s (%s)\n", decision.ID, decision.Decision, reversible)
			if decision.Rationale != "" {
				fmt.Fprintf(e.stdout, "      %s\n", decision.Rationale)
			}
		}
	}
	fmt.Fprintln(e.stdout)

	section(e.stdout, "Verification")
	if bundle.Verification == nil || len(bundle.Verification.Checks) == 0 {
		fmt.Fprintln(e.stdout, "  (no checks recorded)")
	} else {
		field(e.stdout, "independent", "%s", yesNo(bundle.Verification.Independent))
		tw := table(e.stdout)
		for _, check := range bundle.Verification.Checks {
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", check.Name, check.Status, dashIfEmpty(check.Result))
		}
		_ = tw.Flush()
	}

	if len(checkpoint.UnresolvedQuestions) > 0 {
		fmt.Fprintln(e.stdout)
		section(e.stdout, "Unresolved questions")
		list(e.stdout, checkpoint.UnresolvedQuestions)
	}
	if len(checkpoint.KnownFailures) > 0 {
		fmt.Fprintln(e.stdout)
		section(e.stdout, "Known failures")
		list(e.stdout, checkpoint.KnownFailures)
	}
	if !bundle.Valid() {
		fmt.Fprintf(e.stdout, "\nthis bundle has %d contract error(s); run `howlforge handoff validate`\n", len(bundle.Errors()))
		return exitWith(ExitBadHandoff, "")
	}
	return nil
}

type resumeOutput struct {
	SchemaVersion string `json:"schema_version"`
	*resume.Report
}

func cmdResume(ctx context.Context, e *env, name string, args []string) error {
	fs := e.newFlagSet("resume check")
	repoDir := fs.String("repo", ".", "repository to verify the checkpoint against")
	noGit := fs.Bool("no-git", false, "skip repository inspection entirely")
	pos, err := e.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 || pos[0] != "check" {
		return usagef("usage: howlforge resume check <directory> [--repo <dir>]")
	}

	bundle, err := handoff.Load(pos[1])
	if err != nil {
		return exitWith(ExitBadHandoff, "%v", err)
	}

	opts := resume.Options{RepoDir: *repoDir, SkipGit: *noGit, Now: e.global.now}
	if !*noGit {
		inspector, err := resume.NewInspector()
		if err != nil {
			return err
		}
		// A nil inspector means git is unavailable. That degrades the verdict
		// to NEEDS_RECONCILIATION rather than failing the command, so the tool
		// stays usable on a machine without git.
		opts.Inspector = inspector
	}
	report := resume.Evaluate(ctx, bundle, opts)

	if e.global.JSON {
		if err := emitJSON(e.stdout, resumeOutput{SchemaVersion: SchemaResume, Report: report}); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(e.stdout, "Handoff: %s\n", report.Path)
		if report.TaskID != "" {
			fmt.Fprintf(e.stdout, "Task: %s  role %s  %s / %s\n",
				report.TaskID, report.Role, dashIfEmpty(report.Runtime), dashIfEmpty(report.Model))
		}
		fmt.Fprintln(e.stdout)
		tw := table(e.stdout)
		fmt.Fprintln(tw, "  CHECK\tSTATUS\tDETAIL")
		for _, check := range report.Checks {
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", check.ID, check.Status, check.Detail)
		}
		_ = tw.Flush()
		fmt.Fprintln(e.stdout)
		fmt.Fprintf(e.stdout, "%s\n", report.State)
		fmt.Fprintf(e.stdout, "%s\n", report.Summary)
	}
	if !report.Safe() {
		return exitWith(ExitNotResumable, "")
	}
	return nil
}
