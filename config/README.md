# Example configuration

**Everything in this directory is an example.** It exists so HowlForge works
immediately after a clone and so the documentation has something real to
demonstrate against. It is meant to be replaced.

## What the capability levels mean

They are this operator's declared expectations. They are **not** benchmark
results, and they are **not** claims about model quality. Two runtimes declaring
different levels for the same model through different clients is normal and
correct: the client changes what the model can actually do.

Edit them. Nothing in HowlForge treats these numbers as authoritative beyond
using them for comparison.

## What the preferences encode

The `preferred_runtimes` and `fallback_runtimes` in `roles/` are the initial
HowlFutureWorks choices from the HowlForge specification:

| Role | Preference order |
| --- | --- |
| foreman | Sol, then Opus, then Gemini Flash |
| architect | Opus, then Sol, then Gemini Pro |
| implementer | Composer, then Gemini Flash, then Sol |
| researcher | Gemini Flash, then Opus, then Sol |
| reviewer | Opus, then Sol, then Gemini Pro |
| security | Opus, then Sol |
| qa | Gemini Flash, then Composer, then Sol |
| product, devops, sre, auditor | see the individual files |

These are preferences, not rankings of capability.

## Two details that are deliberate

**`grok-cli` ships disabled.** It demonstrates the difference between a runtime
that is not configured at all and one the operator has configured but not
permitted. `howlforge runtimes` shows it; `howlforge match` never offers it.

**`codex-cli-sol` and `cursor-sol` share a provider and model.** They differ
only in client, and they declare different `tool_use` levels. A foreman match
returns one and rejects the other, which is the four level identity model doing
something visible.

## Layout

```
config/
├── roles/          one file per role
├── runtimes/       one file per runtime
├── personas/       optional; delete it entirely and nothing breaks
└── capabilities.yaml   optional; extends the capability vocabulary
```

## Overriding

Point at your own directory:

```bash
howlforge --config /etc/howlforge validate
export HOWLFORGE_CONFIG=/etc/howlforge
```

Or edit these files in place. Run `howlforge validate` after either.
