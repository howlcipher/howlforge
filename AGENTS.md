# AGENTS.md

Rules for AI agents working in this repository: Claude Code, Codex CLI, Cursor
Agent, AGY, Gemini CLI, Devin, and whatever comes next.

Read this before your first edit.

## What HowlForge is

The AI workforce definition and runtime selection layer for HowlFutureWorks. It
defines which roles exist, what each requires, which runtimes can fill them, and
what evidence must exist before another worker resumes a task.

**Roles are durable. Models are replaceable. Sessions are disposable.**

## What you must never build here

HowlForge sits between HowlBoard and HowlPlane. These belong to other
repositories, and adding them here is the single most likely way to damage this
project:

| Do not add | It belongs to |
| --- | --- |
| launching, scheduling, retrying or routing work | HowlPlane |
| checkpoint transport or storage | HowlRelay |
| permission, approval or authority decisions | HowlFrame |
| running verification or tests on behalf of a worker | HowlProof |
| backlog or issue management | HowlBoard |

If a change would make HowlForge decide *what runs*, it is out of scope. If it
would make HowlForge decide *who is eligible and why*, it is in scope.

Specifically forbidden:

- any `exec.Command` outside `internal/resume`
- any `command`, `args`, `exec` or credential field on a runtime definition
- any network client, HTTP server or provider SDK
- any call to an LLM. HowlForge must never use a model to choose a model.
- a second dependency without an explicit decision. CI fails if `go.mod` gains
  one.

## Commands

```bash
make build        # compile to build/howlforge
make test         # go test ./...
make test-race    # go test -race ./...
make vet          # go vet ./...
make fmt          # gofmt -w .
make fmt-check    # fail if anything is unformatted
make validate     # check the shipped example configuration
make verify       # fmt-check, vet, test and validate. Run this before pushing.
make cover        # statement coverage per package
```

`make verify` is the gate. Do not report a change complete without running it
and reading the output.

## Layout

```
cmd/howlforge/          CLI entry point, thin
internal/capabilities/  ordinal scale and controlled vocabulary
internal/config/        the only file reading code; traversal and size guards
internal/roles/         role definitions, inheritance, registry
internal/runtimes/      runtime definitions, four level identity, registry
internal/personas/      optional persona to role resolution
internal/availability/  the eleven state model and the state file reader
internal/matching/      the deterministic matcher, explain and availability
internal/handoff/       checkpoint contract and bundle validation
internal/resume/        resume rules and the single git subprocess
internal/cli/           command dispatch, rendering, exit codes
pkg/howlforge/          the public library surface HowlPlane consumes
config/                 example roles, runtimes and personas
schemas/                JSON Schema for every artifact
examples/               a worked handoff bundle and an availability state file
```

## Conventions

**Comments explain why, not what.** `// Loop over the runtimes` is noise.
`// Capability is checked before availability so an availability rejection is
always a capability eligible runtime` is the reason a reader needs.

**Diagnostics name the file and the field.** An operator should never have to
search for what a message refers to. Compare:

```
bad:  unknown capability
good: role "foreman" (roles/foreman.yaml): unknown capability "resoning" (did you mean "reasoning"?)
```

**Errors are values with context.** Wrap with `%w` and add the source path.

**Nothing iterates a map without sorting.** Map iteration order is randomized in
Go, and determinism is a correctness property here, not a nicety. Use the
registries' `All()` and `IDs()`, which are id ordered.

**Exported identifiers are documented.** Start the comment with the identifier
name.

**Formatting.** `gofmt` is authoritative. Hyphens are fine in prose and in file
names where they read naturally; this repository follows the canonical
HowlFutureWorks rulebook on that point.

## Required tests

Any change to matching, availability, handoff or resume logic must keep these
properties provable, and each already has a test:

| Property | Test |
| --- | --- |
| identical inputs produce byte identical output | `internal/matching.TestDeterminism` |
| `explain` and `match` never disagree | `internal/matching.TestExplainAgreesWithMatch` |
| a hint reorders but never changes eligibility | `internal/matching.TestHintsDoNotChangeEligibility` |
| a new runtime needs no code change | `pkg/howlforge.TestAddingARuntimeNeedsNoCodeChange` |
| a capacity limit reports a reset, a fault does not | `internal/availability.TestEligibleAfterResetRequiresACapacityCondition` |
| indeterminacy never yields SAFE | `internal/resume.TestVerdictPrecedence` |
| config cannot escape its root | `internal/config.TestReadFileRefusesEscapes` and `TestReadFileRefusesSymlinkEscape` |
| a runtime file cannot carry a command | `internal/runtimes.TestRuntimeDefinitionsAreDeclarativeOnly` |
| a persona cannot name a model | `internal/personas.TestPersonaCannotNameARuntime` |
| commit arguments cannot reach git unvalidated | `internal/resume.TestCommitishGuardRefusesArguments` |
| CLI exit codes are stable | `internal/cli.TestExitCodes` |

Write behavior tests, not coverage filler. A test exists to protect a contract,
an invariant or a regression. Do not add one merely because a function exists,
and do not delete one without evidence that the behavior it protected is gone.

Use table driven tests where the cases are variations on one behavior. Name
subtests as sentences describing the expected behavior, not as input values.

## Determinism is a hard requirement

Core selection must be deterministic. Given the same role definitions, runtime
definitions, availability state and request, the result must be identical,
including ordering.

This is not a performance preference. HowlForge is consulted during incidents,
and a layer that answers differently on two consecutive calls cannot be trusted
to explain what happened. If you ever need non determinism, it goes in a
separate optional layer that consumes HowlForge's output.

Concretely, that means: no map iteration without sorting, no wall clock reads
inside the matcher (`Request.Now` is passed in), no randomness, and a total
order in the rank key so ties cannot resolve arbitrarily.

## When configuration semantics change

Changing what a configuration field means is a four part change. All four, in
the same commit:

1. the Go type and its validation in `internal/`
2. the JSON Schema in `schemas/`
3. the documentation in `README.md`, `ARCHITECTURE.md` or `docs/`
4. tests covering the new behavior and its rejection cases

A schema that has drifted from the code is worse than no schema, because it
tells a confident lie to whoever validated against it.

If the change is not backward compatible, bump the schema version (for example
`howlforge.role/v1` to `/v2`) and reject the old version with a diagnostic that
says what to do. Do not silently accept both.

## Treat all input as hostile

Configuration files, availability state and handoff bundles are untrusted.
`internal/config` is the only code that reads a file, and it enforces the
confinement, size and strict decoding rules. Do not read files elsewhere.

When you add a field, ask: can this be a path? If so, it needs validation. Can
it reach a subprocess argument? If so, it needs a format guard like
`handoff.IsCommitish`, applied before the value is put in an argument vector.

## Before you say you are done

1. `make verify` passes, and you read the output rather than assuming it
2. `make test-race` passes if you touched anything concurrent
3. the documentation matches what the code actually prints, checked by running
   it, not by memory
4. no orchestration, authority or verification logic leaked in
5. no credentials, tokens or provider secrets exist anywhere in the tree
6. if you changed configuration semantics, all four parts above are done

Report what you actually ran and what it actually said. If something fails, say
so with the output. A claim of success that the tests do not support is worse
than an honest failure, because the next worker builds on it.
