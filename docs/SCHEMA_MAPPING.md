# Schema mapping

HowlForge does not invent vocabulary where HowlFutureWorks already has one. This
document records where HowlForge's contracts line up with HowlPlane's existing
ones, and where they deliberately do not.

It exists because the alternative is silent divergence: two systems using the
same word for different things, discovered during an incident.

## Availability states

HowlForge's `howlforge.availability/v1` and HowlPlane's
`ProviderAvailabilityStatus` (`src/control_plane/synthesis/provider_pool.py`).

| HowlForge | HowlPlane | Notes |
| --- | --- | --- |
| `AVAILABLE` | `AVAILABLE` | direct |
| `BUSY` | `RESOURCE_CONSTRAINED` | closest match; HowlPlane's is broader |
| `DEGRADED` | `DEGRADED` | direct |
| `SESSION_LIMIT` | `SESSION_EXHAUSTED` | direct. HowlPlane's failure class is also named `SESSION_LIMIT`. |
| `QUOTA_LIMIT` | `QUOTA_EXHAUSTED` | direct |
| `RATE_LIMIT` | `RATE_LIMITED` | direct |
| `PROVIDER_UNAVAILABLE` | `UNREACHABLE` | direct |
| `MODEL_UNAVAILABLE` | (none) | HowlPlane's `ai.failure_class/v1` has `model_unavailable`, but the pool status enum does not distinguish it |
| `AUTH_FAILURE` | `AUTH_REQUIRED` | direct |
| `WORKER_FAILURE` | (none) | HowlPlane records engineering failures as failure classes rather than availability, which is the stricter treatment |
| `UNKNOWN` | `UNKNOWN` | direct |
| (none) | `MISSING_EXECUTABLE` | HowlForge is declarative and never probes for a binary, so it cannot observe this |
| (none) | `DISABLED` | HowlForge expresses this as `state.enabled: false` on the runtime, not as an availability state, because it is an operator decision rather than an observation |

### The invariant both systems share

An engineering failure must never be recorded as quota exhaustion. HowlForge
enforces it structurally: `WORKER_FAILURE.CapacityLimited()` is false, so it can
never report "eligible after reset" and can never be mistaken for a condition
that clears on its own.

### One divergence worth naming

HowlPlane's Go side (`internal/runtime/failure_class.go`) has
`RetriableViaFallback()` returning **false** for `usage_limit`, while its Python
pool treats `SESSION_LIMIT` and `QUOTA_EXHAUSTED` as exactly what drives
failover. HowlForge follows the Python behavior: a capacity limit is a reason to
rotate, not a reason to fail the task. If the Go side is still authoritative
anywhere, that contradiction should be resolved there.

## Capability vocabularies

These are **not** the same vocabulary and must never be merged.

| Vocabulary | Schema | Values | Question |
| --- | --- | --- | --- |
| HowlForge aptitude | `howlforge.capability_level/v1` | `reasoning`, `coding`, `tool_use`, `long_horizon`, `context_window`, `research`, `planning`, `review`, `security`, `testing`, `documentation`, `speed`, `cost_efficiency`, `local_execution` | what is this worker good at? |
| HowlPlane authority | `ai.capability/v1` | `filesystem:repository`, `network:allowlist`, `process:test_only`, `git:push`, `secrets:named_reference`, … | what is this worker permitted to do? |
| HowlPlane skill | `AgentProfile.capabilities` | `code_generation`, `file_editing`, `code_review`, `terminal_execution`, `git_operations`, `architectural_reasoning`, `deep_debugging`, `autonomous_workflow` | what can this worker do, as a feature list? |

HowlForge's aptitude vocabulary is ordinal, which the other two are not. That is
the substantive difference: `coding: high` and `coding: very_high` are
comparable, while `code_generation` is present or absent.

### Suggested mapping from HowlPlane skills

Approximate, for anyone wiring the two together. This is a starting point for a
migration, not a claim of equivalence.

| HowlPlane skill | HowlForge aptitude floor |
| --- | --- |
| `code_generation` | `coding >= medium` |
| `file_editing` | `coding >= medium` and `tool_use >= medium` |
| `code_review` | `review >= high` |
| `terminal_execution` | `tool_use >= high` |
| `git_operations` | `tool_use >= high` |
| `architectural_reasoning` | `reasoning >= very_high` and `planning >= high` |
| `deep_debugging` | `reasoning >= high` and `coding >= high` |
| `autonomous_workflow` | `long_horizon >= very_high` |

The authority vocabulary has no mapping and should not be given one. It belongs
to HowlFrame.

## Roles

HowlPlane has three role vocabularies today and none of them is HowlForge's.

| Source | Values |
| --- | --- |
| `AgentProfile.roles` | `planning`, `implementation`, `remediation`, `review`, `synthesis` |
| `REVIEWER_ROLES` | `correctness-reviewer`, `regression-reviewer`, `security-reviewer`, `test-falsifier`, `architecture-reviewer`, `simplicity-reviewer` |
| `role_binding.py` | domain scoped, for example `humanizer`, `writer`, `editor` |
| HowlForge | `foreman`, `architect`, `product`, `researcher`, `implementer`, `qa`, `security`, `devops`, `sre`, `auditor`, `reviewer` |

Suggested mapping for the pool roles:

| HowlPlane pool role | HowlForge role |
| --- | --- |
| `planning` | `architect`, or `product` for scope work |
| `implementation` | `implementer` |
| `remediation` | `implementer` |
| `review` | `reviewer` |
| `synthesis` | `foreman` |

The reviewer roles map to specializations of HowlForge's `reviewer` and
`security` roles. If HowlPlane adopts HowlForge definitions, those six are best
expressed as roles that `extends: reviewer` with different
`required_capabilities`, which is what the `extends` mechanism is for.

## Handoff contracts

HowlForge's `howlforge.checkpoint/v1` and HowlPlane's `ai.handoff/v1`
(`schemas/handoff.schema.json`).

| `ai.handoff/v1` | `howlforge.checkpoint/v1` | Notes |
| --- | --- | --- |
| `schema` | `schema_version` | renamed for consistency with the other HowlForge documents |
| `run_id` | `task_id` | HowlForge keys on the task, not the run. A task outlives the runs attempting it, which is the whole point. |
| `original_task` | `summary.md` | prose belongs in the prose file |
| `project_root` | `repository.root` | |
| `starting_commit` | `last_verified_commit` | **semantic difference.** HowlPlane records where the run started; HowlForge records where verification was last observed. The second is what a resume check can actually test. |
| `context_reviewed` | (none) | HowlForge does not model context provenance |
| `work_completed` | `completed[]` | prose becomes a list, so it can be counted and compared |
| `files_modified` | `changed_files[]` and `changed-files.txt` | duplicated deliberately, and cross checked |
| `commands_run[{command, result}]` | `verification.checks[{name, command, status, result}]` | HowlForge adds an explicit `status`, because a `result` string cannot be branched on |
| `failure_or_interruption_reason` | `handoff_reason` | HowlForge uses a controlled enum rather than free text |
| `remaining_work` | `remaining[]` and `remaining-work.md` | |
| `security_constraints` | (none) | **intentional gap.** Authority constraints belong to HowlFrame. HowlForge records aptitude and evidence, not permissions. |
| `recommended_next{provider, workflow}` | (none) | **intentional gap.** Recommending the next worker is a selection question, answered live by `howlforge match` against current availability. A recommendation frozen into a file is stale the moment a session limit changes. |
| (none) | `safe_to_resume` | the departing worker's own verdict, which `ai.handoff/v1` has no field for |
| (none) | `status` | `partial`, `complete`, `blocked`, `abandoned` |
| (none) | `decisions[]` | choices the replacement must not re-litigate |
| (none) | `unresolved_questions[]` | forces reconciliation rather than a silent guess |
| (none) | `repository.dirty` | lets a resume check detect unattributed working tree changes |
| (none) | `verification.independent` | whether the verifier was a different worker |

### Why a new schema rather than an extension

Two fields decided it.

`starting_commit` versus `last_verified_commit` is a real semantic difference,
not a rename. A resume check needs the commit at which the recorded evidence was
observed; the commit the run began at cannot answer "were these tests passing
against this tree".

`recommended_next` bakes a selection decision into a file. HowlForge's position
is that the recommendation must be computed against current availability, which
is exactly what `howlforge match` and `howlforge availability` do. A frozen
recommendation is stale the moment a session limit changes, which is the
scenario this whole system exists for.

Translation in either direction is mechanical for every other field, and the
gaps above are documented rather than lost.

## CLI output schemas

HowlForge stamps every JSON document with a `schema_version` in the
`howlforge.*` namespace, matching HowlPlane's convention of `ai.*` for portable
framework contracts and `howlplane.*` for internals. `howlforge.*` is a third
namespace for this layer's contracts; the full list is in [`CLI.md`](CLI.md).
