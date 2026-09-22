// Package cli implements the howlforge command line interface.
//
// The CLI is a thin shell over pkg/howlforge. Every question it answers is
// answered by the library, so anything the terminal can tell an operator,
// HowlPlane can obtain through the Go API with identical results.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/howlcipher/howlforge/pkg/howlforge"
)

// Exit codes. They are part of the interface: automation branches on them, so
// they are documented in docs/CLI.md and asserted by test.
const (
	// ExitOK means the command succeeded.
	ExitOK = 0
	// ExitError means an unexpected internal failure.
	ExitError = 1
	// ExitUsage means the command line was malformed.
	ExitUsage = 2
	// ExitInvalidConfig means configuration failed to load or validate.
	ExitInvalidConfig = 3
	// ExitNoCandidate means no eligible runtime could fill the role.
	ExitNoCandidate = 4
	// ExitNotResumable means a resume check returned anything but SAFE.
	ExitNotResumable = 5
	// ExitBadHandoff means a handoff bundle violated the contract.
	ExitBadHandoff = 6
)

// usageError marks a failure that should exit with ExitUsage.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

func usagef(format string, args ...interface{}) error {
	return &usageError{err: fmt.Errorf(format, args...)}
}

// configError marks a failure that should exit with ExitInvalidConfig.
type configError struct{ err error }

func (e *configError) Error() string { return e.err.Error() }
func (e *configError) Unwrap() error { return e.err }

// env carries everything a command needs.
type env struct {
	stdout io.Writer
	stderr io.Writer
	global *globalFlags
}

// globalFlags are accepted both before and after the command name.
type globalFlags struct {
	ConfigRoot string
	StatePath  string
	JSON       bool
	NoColor    bool
	NowRaw     string

	now time.Time
}

// register adds the global flags to a command flag set, defaulting each to
// whatever was already parsed from the leading position. That is what lets
// `howlforge --json match foreman` and `howlforge match foreman --json` behave
// identically.
func (g *globalFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&g.ConfigRoot, "config", g.ConfigRoot, "configuration directory holding roles, runtimes and personas")
	fs.StringVar(&g.StatePath, "state", g.StatePath, "availability state file to read")
	fs.BoolVar(&g.JSON, "json", g.JSON, "emit JSON instead of human readable text")
	fs.BoolVar(&g.NoColor, "no-color", g.NoColor, "disable colored output")
	fs.StringVar(&g.NowRaw, "now", g.NowRaw, "evaluate as of this RFC3339 time instead of the wall clock")
}

// globalsWithValue are the leading global flags that consume a following
// argument.
var globalsWithValue = map[string]bool{"config": true, "state": true, "now": true}

// parseLeading consumes global flags that appear before the command name and
// returns the remaining arguments.
func (g *globalFlags) parseLeading(args []string) ([]string, error) {
	for len(args) > 0 {
		arg := args[0]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			return args, nil
		}
		if arg == "--" {
			return args[1:], nil
		}
		name := strings.TrimLeft(arg, "-")
		value := ""
		hasValue := false
		if idx := strings.Index(name, "="); idx >= 0 {
			name, value = name[:idx], name[idx+1:]
			hasValue = true
		}
		switch name {
		case "help", "h", "version", "v":
			// Left for the dispatcher to handle as a command.
			return args, nil
		case "json":
			g.JSON = true
			args = args[1:]
		case "no-color":
			g.NoColor = true
			args = args[1:]
		default:
			if !globalsWithValue[name] {
				return nil, usagef("unknown flag %q", arg)
			}
			if !hasValue {
				if len(args) < 2 {
					return nil, usagef("flag %q needs a value", arg)
				}
				value = args[1]
				args = args[2:]
			} else {
				args = args[1:]
			}
			switch name {
			case "config":
				g.ConfigRoot = value
			case "state":
				g.StatePath = value
			case "now":
				g.NowRaw = value
			}
		}
	}
	return args, nil
}

// resolveNow parses the --now override once, after command flags are parsed.
func (g *globalFlags) resolveNow() error {
	if strings.TrimSpace(g.NowRaw) == "" {
		g.now = time.Now().UTC()
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, g.NowRaw)
	if err != nil {
		return usagef("--now must be an RFC3339 timestamp: %v", err)
	}
	g.now = parsed.UTC()
	return nil
}

// Run executes one command line and returns the process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	global := &globalFlags{}
	rest, err := global.parseLeading(args)
	if err != nil {
		return report(stderr, err)
	}
	e := &env{stdout: stdout, stderr: stderr, global: global}

	if len(rest) == 0 {
		printUsage(stdout)
		return ExitOK
	}

	command := rest[0]
	commandArgs := rest[1:]

	switch command {
	case "help", "-h", "--help":
		printUsage(stdout)
		return ExitOK
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "howlforge %s\n", howlforge.Version)
		return ExitOK
	}

	handler, ok := commands[command]
	if !ok {
		return report(stderr, usagef("unknown command %q: run `howlforge help` for the command list", command))
	}
	if err := handler(ctx, e, command, commandArgs); err != nil {
		return report(stderr, err)
	}
	return ExitOK
}

// handler is one command implementation.
type handler func(ctx context.Context, e *env, name string, args []string) error

var commands map[string]handler

func init() {
	// Assigned in init to avoid an initialization cycle between the map and
	// the handlers that reference it for help output.
	commands = map[string]handler{
		"roles":        cmdRoles,
		"role":         cmdRole,
		"runtimes":     cmdRuntimes,
		"runtime":      cmdRuntime,
		"personas":     cmdPersonas,
		"persona":      cmdPersona,
		"capabilities": cmdCapabilities,
		"match":        cmdMatch,
		"explain":      cmdExplain,
		"availability": cmdAvailability,
		"handoff":      cmdHandoff,
		"resume":       cmdResume,
		"validate":     cmdValidate,
		"doctor":       cmdDoctor,
	}
}

// report renders an error and maps it to an exit code.
func report(stderr io.Writer, err error) int {
	if err == nil {
		return ExitOK
	}
	var exit *exitError
	if errors.As(err, &exit) {
		if exit.message != "" {
			fmt.Fprintf(stderr, "howlforge: %s\n", exit.message)
		}
		return exit.code
	}
	var usage *usageError
	if errors.As(err, &usage) {
		fmt.Fprintf(stderr, "howlforge: %v\n", err)
		fmt.Fprintf(stderr, "run `howlforge help` for usage\n")
		return ExitUsage
	}
	var cfg *configError
	if errors.As(err, &cfg) {
		fmt.Fprintf(stderr, "howlforge: %v\n", err)
		return ExitInvalidConfig
	}
	fmt.Fprintf(stderr, "howlforge: %v\n", err)
	return ExitError
}

// exitError carries an explicit exit code for outcomes that are not failures
// of the program, such as "no candidate is eligible".
type exitError struct {
	code    int
	message string
}

func (e *exitError) Error() string {
	if e.message == "" {
		return fmt.Sprintf("exit %d", e.code)
	}
	return e.message
}

func exitWith(code int, format string, args ...interface{}) error {
	return &exitError{code: code, message: fmt.Sprintf(format, args...)}
}

// newFlagSet builds a flag set that writes usage to stderr and carries the
// global flags.
func (e *env) newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	e.global.register(fs)
	return fs
}

// load opens the configured workforce definition.
func (e *env) load() (*howlforge.Forge, error) {
	forge, err := howlforge.Load(howlforge.Options{
		Root:      e.global.ConfigRoot,
		StatePath: e.global.StatePath,
	})
	if err != nil {
		return nil, &configError{err: err}
	}
	return forge, nil
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `howlforge - AI workforce definition and runtime selection for HowlFutureWorks

Roles are durable. Models are replaceable. Sessions are disposable.

Usage:
  howlforge [global flags] <command> [arguments]

Workforce:
  roles                          list every defined role
  role show <role>               show one role in full
  runtimes                       list every configured runtime
  runtime show <runtime>         show one runtime in full
  personas                       list every configured persona
  persona show <persona>         show one persona and the role it resolves to
  capabilities                   list the admitted capability vocabulary

Selection:
  match <role>                   list eligible runtimes in preference order
  explain <role> <runtime>       account for one role and runtime pairing
  availability [role]            show runtime availability, optionally for a role

Handoff:
  handoff validate <dir>         check a handoff bundle against the contract
  handoff inspect <dir>          summarize a handoff bundle
  resume check <dir>             decide whether the bundle is safe to resume

Configuration:
  validate                       check the whole configuration for coherence
  doctor                         report the effective environment and readiness
  version                        print the howlforge version

Global flags:
  --config <dir>                 configuration directory (default: $HOWLFORGE_CONFIG or ./config)
  --state <file>                 availability state file (default: $HOWLFORGE_STATE)
  --json                         emit JSON instead of text
  --now <rfc3339>                evaluate as of a fixed time, for reproducible output
  --no-color                     disable colored output

Selection flags:
  --require <capability>=<level> add a capability floor, repeatable
  --prefer <hint>                rank by quality, speed, cost, local, privacy or context, repeatable
  --exclude <runtime>            refuse a runtime for this match, repeatable
  --persona <persona>            resolve the role from a persona

Exit codes:
  0 success   1 internal error   2 usage error   3 invalid configuration
  4 no eligible candidate        5 not safe to resume                6 malformed handoff bundle
`)
}

// parseArgs parses a flag set that allows flags and positional arguments to be
// interleaved, and returns the positional arguments.
//
// Go's flag package stops at the first non-flag argument, which would make
// `howlforge match implementer --require coding=high` a usage error. Since
// that is the form the documentation uses and the form operators type, the
// parser is run repeatedly, peeling off one positional argument each pass.
func (e *env) parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	// An explicit terminator means everything after it is positional, so the
	// peeling loop must not treat those tokens as flags.
	var tail []string
	for i, arg := range args {
		if arg == "--" {
			args, tail = args[:i], args[i+1:]
			break
		}
	}

	var positional []string
	remaining := args
	for {
		if err := fs.Parse(remaining); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, exitWith(ExitOK, "")
			}
			return nil, &usageError{err: err}
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		remaining = rest[1:]
	}
	positional = append(positional, tail...)

	if err := e.global.resolveNow(); err != nil {
		return nil, err
	}
	return positional, nil
}
