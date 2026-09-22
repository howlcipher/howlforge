# The handoff contract

## What it is for

One sentence:

> The next worker must be able to continue safely without the previous worker's
> session transcript.

Every field in the contract exists because its absence would force the
replacement to guess. The session that produced the work is gone and is not
coming back; the bundle is all that survives.

## The bundle

```
.ai/handoffs/<TASK_ID>/
├── checkpoint.json      the machine readable core
├── summary.md           orientation for a human or an agent reading in
├── decisions.json       choices the replacement must not silently re-litigate
├── changed-files.txt    repository relative paths, one per line
├── remaining-work.md    what is left, in prose, with acceptance criteria
└── verification.json    what was proven, by whom, and whether independently
```

**All six are required.** A bundle missing one is not mostly fine: the
replacement would have to infer the missing part, and inference is exactly what
this contract removes. `howlforge handoff validate` refuses an incomplete
bundle.

The machine readable and human readable halves are both required on purpose.
`checkpoint.json` is what HowlPlane branches on. `summary.md` and
`remaining-work.md` are what the next worker actually reads to understand
intent, which no JSON field captures well.

## checkpoint.json

Schema: [`schemas/checkpoint.schema.json`](../schemas/checkpoint.schema.json),
`$id` `howlforge.checkpoint/v1`.

```json
{
  "schema_version": "howlforge.checkpoint/v1",
  "task_id": "HOWL-0042",
  "role": "implementer",
  "runtime": "cursor",
  "model": "composer",
  "status": "partial",
  "safe_to_resume": true,
  "handoff_reason": "SESSION_LIMIT",
  "last_verified_commit": "5549653fda4342250362e219a2d23813f9022459",
  "completed": ["implemented runtime registry", "added unit tests"],
  "remaining": ["integration tests", "documentation"]
}
```

Those ten fields are the required minimum. A complete bundle also carries
`worker`, `runtime_identity`, `repository`, `changed_files`, `decisions`,
`unresolved_questions`, `known_failures`, `verification`, `created_at` and
`notes`. See `examples/handoffs/HOWL-0042/checkpoint.json` for a full one.

### Fields that carry weight

**`status`** is `partial`, `complete`, `blocked` or `abandoned`. An `abandoned`
attempt must not be built on, and marking one `safe_to_resume` is a validation
error.

**`safe_to_resume`** is the departing worker's own verdict. It is believed.
HowlForge never overrides a worker's caution; a `false` here yields
`NEEDS_RECONCILIATION` regardless of how healthy everything else looks.

**`handoff_reason`** distinguishes a capacity condition from a fault, using the
same vocabulary as the availability model: `SESSION_LIMIT`, `QUOTA_LIMIT`,
`RATE_LIMIT`, `PROVIDER_UNAVAILABLE`, `WORKER_FAILURE`, `VOLUNTARY`,
`INTERRUPTED`, `COMPLETED`. A session limit means "rotate and continue". A
worker failure means "something went wrong, look before continuing".

**`last_verified_commit`** must be a bare hexadecimal object name, 7 to 64
characters. Without it the resume rules have nothing to check and every resume
is a guess. The format is also the injection guard: only strings that pass it
are ever handed to `git`, so a commit field containing shell metacharacters or a
leading dash is refused before it reaches an argument list.

**`repository.dirty`** records whether the working tree had uncommitted changes.
A dirty tree the checkpoint does not record forces reconciliation, because the
replacement would otherwise build on work it cannot attribute.

**`unresolved_questions`** being non empty forces `NEEDS_RECONCILIATION`. That
is not a criticism of the departing worker. Recording an open question is the
correct behavior; the point is that the next worker must resolve it rather than
pick an answer silently.

**`changed_files`** entries must be repository relative. Absolute paths and
parent traversal are refused: a path list is the obvious place to hide an escape
from the repository the checkpoint claims to describe.

## decisions.json

```json
{
  "schema_version": "howlforge.decisions/v1",
  "decisions": [
    {
      "id": "D1",
      "decision": "Runtime definitions carry no command or argument field",
      "rationale": "A configuration file must never be able to cause HowlForge to execute a provider.",
      "reversible": false
    }
  ]
}
```

An irreversible decision does not block resumption. It is surfaced as an
advisory, because a replacement that reverses one without noticing causes real
damage, and the bundle is the only place that knowledge survives.

## verification.json

```json
{
  "schema_version": "howlforge.verification/v1",
  "independent": false,
  "checks": [
    {"name": "go test ./internal/runtimes/", "status": "passed", "result": "ok"}
  ]
}
```

Check status is `passed`, `failed`, `skipped` or `errored`. **Anything other
than an explicit pass counts as not passed**, so a malformed or absent status
can never read as success. This mirrors a real HowlPlane incident where a dead
reviewer's empty findings list was inferred to mean "clean".

`independent` is recorded truthfully. A worker that verified its own output says
so, and HowlProof decides what that is worth.

## Cross checks

Artifacts that contradict each other are refused outright. Two disagreeing
records of one fact are worse than one record, because the replacement has no
way to know which is true.

| Check | Result |
| --- | --- |
| `changed-files.txt` disagrees with `checkpoint.json`'s `changed_files` | error |
| `decisions.json` records a different number of decisions than the checkpoint | error |
| the two `independent` flags disagree | error |
| a check failed while `safe_to_resume` is true | warning |
| `safe_to_resume` is true with unresolved questions | warning |

Order does not matter for the changed file comparison; content does.

## Resume rules

`howlforge resume check <dir> --repo <dir>` evaluates nine rules and returns one
of five verdicts.

| Verdict | Meaning |
| --- | --- |
| `SAFE` | every rule passed; work may continue |
| `NEEDS_RECONCILIATION` | something could not be established; look before continuing |
| `STALE` | the repository no longer contains the state the checkpoint describes |
| `BLOCKED` | the checkpoint itself says work cannot continue |
| `INVALID` | the bundle does not satisfy the contract |

Severity is ordered `SAFE < NEEDS_RECONCILIATION < STALE < BLOCKED < INVALID`
and the worst finding wins. A single failing rule is never averaged away by
passing ones.

### The rules

| Rule | Passes when |
| --- | --- |
| `bundle_valid` | all six artifacts are present and well formed |
| `status` | the status is not `blocked` or `abandoned` |
| `safe_to_resume` | the departing worker set it true |
| `remaining_explicit` | a partial checkpoint lists remaining work |
| `unresolved_questions` | none are recorded |
| `decisions` | advisory only; surfaces irreversible decisions |
| `verification_evidence` | checks are recorded and none failed |
| `verification_reproduced` | advisory only; never attempted, HowlProof owns execution |
| `verification_independence` | advisory only; reports whether verification was independent |
| `commit_exists` | the recorded commit is present in the repository |
| `head_related` | HEAD is at, or descends from, the recorded commit |
| `worktree_understood` | the working tree's dirtiness matches the checkpoint |

### The governing rule

**Indeterminacy never becomes confidence.** A git failure, a missing git binary,
`--no-git`, and an unsupplied repository path all degrade the verdict to
`NEEDS_RECONCILIATION`. Nothing degrades it to `SAFE`.

HEAD having moved *forward* is safe: the recorded work is still in the history
the replacement will build on. HEAD having *diverged* is not.

### What HowlForge deliberately does not do

It does not re-run the recorded checks, and it says so in the report rather than
implying it:

```
verification_reproduced  advisory  not attempted: HowlProof owns verification
                                   execution, HowlForge only reads the evidence
```

"Tests passed" in a file is a claim, not an observation. Turning that claim into
an observation requires running them, which is HowlProof's job and needs
HowlPlane to arrange a worker.

## Writing a bundle

HowlForge validates bundles; it never writes one. The worker, or HowlRelay on
its behalf, writes them. Validate what you produce:

```bash
howlforge handoff validate .ai/handoffs/HOWL-0042
howlforge resume check .ai/handoffs/HOWL-0042 --repo .
```

A worked example that returns `SAFE` against this repository lives in
[`examples/handoffs/HOWL-0042`](../examples/handoffs/HOWL-0042).

## Relationship to HowlPlane's ai.handoff/v1

HowlPlane already defines `ai.handoff/v1`. The two contracts overlap and the
mapping is documented field by field in
[`SCHEMA_MAPPING.md`](SCHEMA_MAPPING.md).
