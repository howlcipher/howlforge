# Schemas

JSON Schema (draft 2020-12) definitions for every artifact HowlForge reads or
defines. They are the portable contract: other HowlFutureWorks components
validate against these without linking the Go packages.

| Schema | `$id` | Artifact |
| --- | --- | --- |
| `role.schema.json` | `howlforge.role/v1` | `config/roles/*.yaml` |
| `runtime.schema.json` | `howlforge.runtime/v1` | `config/runtimes/*.yaml` |
| `persona.schema.json` | `howlforge.persona/v1` | `config/personas/*.yaml` |
| `capabilities.schema.json` | `howlforge.capabilities/v1` | `config/capabilities.yaml` |
| `capability-level.schema.json` | `howlforge.capability_level/v1` | the ordinal scale |
| `runtime-ref.schema.json` | `howlforge.runtime_ref/v1` | a role's runtime selector |
| `availability.schema.json` | `howlforge.availability/v1` | the availability state file |
| `checkpoint.schema.json` | `howlforge.checkpoint/v1` | `.ai/handoffs/<TASK>/checkpoint.json` |
| `decisions.schema.json` | `howlforge.decisions/v1` | `.ai/handoffs/<TASK>/decisions.json` |
| `verification.schema.json` | `howlforge.verification/v1` | `.ai/handoffs/<TASK>/verification.json` |

## Validation is enforced in Go, not by a schema library

HowlForge validates configuration in code rather than by running a JSON Schema
validator at startup. That is a deliberate trade off:

- It keeps the dependency budget at one library, which matters for a tool whose
  whole purpose is to be the thing that still works when other things do not.
- It produces better diagnostics. A schema validator says `additionalProperties
  is not allowed`; HowlForge says which file, which field, and suggests the
  capability name you probably meant.
- The cost is that these files and the Go validators can drift. That is why
  changing configuration semantics requires updating both, and why `AGENTS.md`
  makes it a rule rather than a suggestion.

## CLI output schemas

The `--json` output of each command is also schema stamped, with identifiers
such as `howlforge.match/v1` and `howlforge.resume_report/v1`. Those shapes are
defined by the Go types in `internal/cli` and documented in `docs/CLI.md`.
HowlPlane branches on `schema_version`, so those identifiers change only with a
version bump.
