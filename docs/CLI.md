# HowlForge CLI

Every command supports `--json`. Every command is read only: nothing in the CLI
writes a file, launches a worker or contacts a provider.

## Global flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--config <dir>` | `$HOWLFORGE_CONFIG`, then `./config`, then the user config directory | configuration root holding `roles/`, `runtimes/` and `personas/` |
| `--state <file>` | `$HOWLFORGE_STATE`, then `<user config>/howlforge/availability.json` | availability state file. A missing file is not an error. |
| `--json` | off | emit JSON instead of text |
| `--now <rfc3339>` | the wall clock | evaluate as of a fixed time, for reproducible output |
| `--no-color` | off | disable colored output |

Global flags work both before and after the command name, so
`howlforge --json match foreman` and `howlforge match foreman --json` are
identical. Flags may also follow positional arguments:
`howlforge match implementer --require coding=high` parses as expected.

## Exit codes

Branch on these rather than parsing output.

| Code | Name | Meaning |
| --- | --- | --- |
| 0 | success | the command answered |
| 1 | internal error | an unexpected failure |
| 2 | usage error | malformed command line, unknown role or unknown runtime |
| 3 | invalid configuration | configuration failed to load or failed `validate` |
| 4 | no eligible candidate | `match` or `availability` found no usable runtime |
| 5 | not safe to resume | `resume check` returned anything other than `SAFE` |
| 6 | malformed handoff bundle | the bundle violates the contract, or is unreadable |

Note that `explain` exits 0 even when the runtime is not eligible: explaining a
rejection is a successful explanation. Read the `eligible` field in `--json`
output to branch on the result.

## Workforce commands

### `howlforge roles`

Lists every role with its handoff and verification contract.

### `howlforge role show <role>`

Full detail: responsibilities, required capabilities, preferred and fallback
selectors, excluded runtimes, contracts and the source file.

### `howlforge runtimes [--enabled]`

Lists every runtime with its identity, whether the operator enabled it, and its
current availability. `--enabled` hides runtimes the operator has not permitted.

### `howlforge runtime show <runtime>`

Full detail plus the roles this runtime is currently eligible for, derived from
the same matcher the selection commands use.

### `howlforge personas` and `howlforge persona show <persona>`

Personas are optional. With none configured, `personas` says so and exits 0.

### `howlforge capabilities`

The ordinal scale and the admitted capability vocabulary, including any
extensions from `config/capabilities.yaml`.

## Selection commands

### `howlforge match <role> [flags]`

Eligible runtimes in preference order.

| Flag | Meaning |
| --- | --- |
| `--require <capability>=<level>` | add a capability floor. Repeatable, and comma splittable. |
| `--prefer <hint>` | rank by `quality`, `speed`, `cost`, `local`, `privacy` or `context`. Repeatable. |
| `--exclude <runtime>` | refuse a runtime for this match. Repeatable. This is how HowlPlane asks for a rematch that skips a worker it just lost. |
| `--persona <persona>` | resolve the role from a persona instead of naming it |
| `--rejected` | also show every rejected runtime and the reason |

Exits 4 when no runtime is eligible.

```console
$ howlforge match implementer --require coding=high --prefer cost
```

### `howlforge explain <role> <runtime>`

Accounts for one pairing: what the role required, what the candidate offers,
where it sits in the declared preference order, its current state, and either
its rank or the reason it was rejected.

The explanation is derived from the same `match` the CLI would run, so `explain`
and `match` can never tell different stories.

### `howlforge availability [role]`

With no role, the whole fleet. With a role, only runtimes that could fill it,
including ones that are currently limited, with the time they are expected back.

A runtime that is available but cannot meet the role's requirements is
deliberately **not** listed. Unavailable and unqualified are different answers.

Exits 4 when nothing usable is available.

## Handoff commands

### `howlforge handoff validate <dir>`

Checks a bundle against `howlforge.checkpoint/v1`. Reports every problem, not
just the first. Exits 6 on any error diagnostic.

### `howlforge handoff inspect <dir>`

Summarizes a bundle: task, worker, repository, completed and remaining work,
changed files, decisions with their reversibility, verification evidence,
unresolved questions and known failures.

### `howlforge resume check <dir> [--repo <dir>] [--no-git]`

Decides whether a replacement may safely continue. `--repo` defaults to the
current directory. `--no-git` skips repository inspection, which always yields
`NEEDS_RECONCILIATION` rather than `SAFE`.

Exits 5 for anything other than `SAFE`.

## Configuration commands

### `howlforge validate [--strict]`

Checks the whole configuration and reports every problem. `--strict` treats
warnings as errors. Exits 3 on failure.

Detects: unknown capability names (with a suggestion), selectors matching no
configured runtime, personas naming missing roles, a role naming itself as its
own verifier, a declared verifier that does not exist, roles no enabled runtime
can ever fill, roles with only one possible runtime and therefore no fallback,
and availability entries for runtimes that are no longer configured.

Defects that prevent loading at all (duplicate ids, circular inheritance,
unsupported schema versions, malformed capability levels) are reported here too.

### `howlforge doctor`

Reports the effective environment: version, Go toolchain, resolved configuration
root, resolved state path, whether configuration loads and validates, and
whether `git` is available for resume checking. Contacts nothing. Exits 3 if
anything fails.

## JSON output

Every JSON document carries `schema_version`. HowlPlane branches on it, so these
identifiers change only with a version bump.

| Command | `schema_version` |
| --- | --- |
| `roles` | `howlforge.roles/v1` |
| `role show` | `howlforge.role_detail/v1` |
| `runtimes` | `howlforge.runtimes/v1` |
| `runtime show` | `howlforge.runtime_detail/v1` |
| `personas` | `howlforge.personas/v1` |
| `persona show` | `howlforge.persona_detail/v1` |
| `capabilities` | `howlforge.capability_vocabulary/v1` |
| `match` | `howlforge.match/v1` |
| `explain` | `howlforge.explanation/v1` |
| `availability` | `howlforge.availability_report/v1` |
| `handoff validate` / `inspect` | `howlforge.handoff_report/v1` |
| `resume check` | `howlforge.resume_report/v1` |
| `validate` | `howlforge.validation/v1` |
| `doctor` | `howlforge.doctor/v1` |

JSON output is byte identical across runs for identical inputs. Pass `--now` to
remove the wall clock as a variable when you need reproducible output in a test
or a fixture.
