package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/howlcipher/howlforge/internal/availability"
	"github.com/howlcipher/howlforge/pkg/howlforge"
)

type validateOutput struct {
	SchemaVersion string `json:"schema_version"`
	*howlforge.ValidationReport
}

func cmdValidate(ctx context.Context, e *env, name string, args []string) error {
	fs := e.newFlagSet(name)
	strict := fs.Bool("strict", false, "treat warnings as errors")
	pos, err := e.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("`howlforge validate` takes no arguments")
	}
	forge, err := e.load()
	if err != nil {
		// A configuration that cannot even load is the most common failure, so
		// it is reported through the same exit code as a failed validation.
		return err
	}
	report, err := forge.Validate(ctx)
	if err != nil {
		return err
	}
	failed := !report.OK || (*strict && len(report.Warnings()) > 0)

	if e.global.JSON {
		if err := emitJSON(e.stdout, validateOutput{
			SchemaVersion: SchemaValidation, ValidationReport: report,
		}); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(e.stdout, "Configuration: %s\n", report.Root)
		tw := table(e.stdout)
		fmt.Fprintf(tw, "  roles\t%d\n", report.Roles)
		fmt.Fprintf(tw, "  runtimes\t%d (%d enabled)\n", report.Runtimes, report.EnabledRuntimes)
		fmt.Fprintf(tw, "  personas\t%d\n", report.Personas)
		fmt.Fprintf(tw, "  capabilities\t%d\n", report.Capabilities)
		_ = tw.Flush()
		fmt.Fprintln(e.stdout)
		if len(report.Problems) == 0 {
			fmt.Fprintln(e.stdout, "OK")
			return nil
		}
		for _, problem := range report.Problems {
			fmt.Fprintf(e.stdout, "  %s\n", problem)
		}
		fmt.Fprintln(e.stdout)
		fmt.Fprintf(e.stdout, "%d error(s), %d warning(s)\n", len(report.Errors()), len(report.Warnings()))
	}
	if failed {
		return exitWith(ExitInvalidConfig, "")
	}
	return nil
}

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type doctorOutput struct {
	SchemaVersion string        `json:"schema_version"`
	Version       string        `json:"version"`
	Go            string        `json:"go"`
	OS            string        `json:"os"`
	ConfigRoot    string        `json:"config_root"`
	StatePath     string        `json:"state_path,omitempty"`
	Checks        []doctorCheck `json:"checks"`
	OK            bool          `json:"ok"`
}

// cmdDoctor reports the effective environment. It never changes anything and
// never contacts a provider: everything it reports is read from disk.
func cmdDoctor(ctx context.Context, e *env, name string, args []string) error {
	fs := e.newFlagSet(name)
	if _, err := e.parseArgs(fs, args); err != nil {
		return err
	}
	out := doctorOutput{
		SchemaVersion: SchemaDoctor,
		Version:       howlforge.Version,
		Go:            runtime.Version(),
		OS:            runtime.GOOS + "/" + runtime.GOARCH,
		ConfigRoot:    howlforge.DefaultRoot(e.global.ConfigRoot),
		StatePath:     availability.DefaultStatePath(e.global.StatePath),
		OK:            true,
	}
	add := func(name, status, format string, args ...interface{}) {
		if status == "fail" {
			out.OK = false
		}
		out.Checks = append(out.Checks, doctorCheck{Name: name, Status: status, Detail: fmt.Sprintf(format, args...)})
	}

	if info, err := os.Stat(out.ConfigRoot); err != nil {
		add("config_root", "fail", "%s is not readable: %v", out.ConfigRoot, err)
	} else if !info.IsDir() {
		add("config_root", "fail", "%s is not a directory", out.ConfigRoot)
	} else {
		add("config_root", "ok", "%s", out.ConfigRoot)
	}

	if out.StatePath == "" {
		add("availability_state", "warn", "no state file path could be resolved; every runtime reads as UNKNOWN")
	} else if _, err := os.Stat(out.StatePath); err != nil {
		add("availability_state", "warn",
			"%s does not exist; every runtime reads as UNKNOWN until HowlPlane writes it", out.StatePath)
	} else {
		add("availability_state", "ok", "%s", out.StatePath)
	}

	forge, loadErr := e.load()
	if loadErr != nil {
		add("configuration", "fail", "%v", loadErr)
	} else {
		add("configuration", "ok", "%d role(s), %d runtime(s), %d persona(s)",
			len(forge.Roles()), len(forge.Runtimes()), len(forge.Personas()))
		report, err := forge.Validate(ctx)
		if err != nil {
			add("validation", "fail", "%v", err)
		} else if report.OK {
			add("validation", "ok", "%d warning(s)", len(report.Warnings()))
		} else {
			add("validation", "fail", "%d error(s); run `howlforge validate`", len(report.Errors()))
		}
	}

	// git is optional. Its absence only limits resume checking, so it is never
	// a failure.
	if inspector, err := resumeInspectorAvailable(); err != nil {
		add("git", "warn", "%v", err)
	} else if !inspector {
		add("git", "warn", "git was not found on PATH; `resume check` will report NEEDS_RECONCILIATION")
	} else {
		add("git", "ok", "available for resume checking")
	}

	if e.global.JSON {
		if err := emitJSON(e.stdout, out); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(e.stdout, "howlforge %s  (%s, %s)\n\n", out.Version, out.Go, out.OS)
		tw := table(e.stdout)
		fmt.Fprintln(tw, "  CHECK\tSTATUS\tDETAIL")
		for _, check := range out.Checks {
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", check.Name, check.Status, check.Detail)
		}
		_ = tw.Flush()
		fmt.Fprintln(e.stdout)
		if out.OK {
			fmt.Fprintln(e.stdout, "OK")
		} else {
			fmt.Fprintln(e.stdout, "PROBLEMS FOUND")
		}
	}
	if !out.OK {
		return exitWith(ExitInvalidConfig, "")
	}
	return nil
}
