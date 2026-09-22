# Examples

## `handoffs/HOWL-0042/`

A complete handoff bundle from a worker that hit a session limit. Every required
artifact is present and the checkpoint anchors to a real commit in this
repository's history, so the resume check genuinely passes rather than
demonstrating the happy path with an invented hash:

```bash
howlforge handoff validate examples/handoffs/HOWL-0042
howlforge handoff inspect examples/handoffs/HOWL-0042
howlforge resume check examples/handoffs/HOWL-0042 --repo .
```

The last command returns `SAFE` against a clean checkout with full history. Two
things will change that legitimately:

- a shallow clone (`--depth 1`) will not contain the anchor commit, so the
  verdict becomes `STALE`. That is the check working.
- an uncommitted change in your working tree makes the verdict
  `NEEDS_RECONCILIATION`, because the checkpoint records a clean tree.

## `state/availability.json`

An availability snapshot in which both Sol runtimes are session limited until
18:00Z and everything else is available. HowlForge only ever reads this file;
HowlPlane and HowlRelay write it.

Use it with `--now` to see the rotation and the recovery without waiting:

```bash
# During the limit: Opus takes over.
howlforge --state examples/state/availability.json --now 2026-09-22T15:00:00Z availability foreman

# After the reset: Sol reclaims first place, with no configuration change.
howlforge --state examples/state/availability.json --now 2026-09-22T19:00:00Z match foreman
```
