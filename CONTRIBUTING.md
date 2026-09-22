# Contributing to HowlForge

## Scope

HowlForge is the workforce definition and runtime selection layer. Before
proposing a change, check it against the boundaries in
[`ARCHITECTURE.md`](ARCHITECTURE.md): orchestration belongs to HowlPlane,
checkpoint transport to HowlRelay, authority to HowlFrame, and verification
execution to HowlProof.

A change that makes HowlForge decide *what runs* is out of scope. A change that
makes it better at deciding *who is eligible and why* is in scope.

## Getting set up

```bash
git clone https://github.com/howlcipher/howlforge
cd howlforge
make build
make verify
```

Go 1.24 or newer. Nothing else is required, and that is deliberate.

## Making a change

1. Branch from `main`.
2. Write the test first. Define the observable behavior, watch it fail, then
   implement it.
3. Run the targeted package tests while you work
   (`go test ./internal/matching/`), not the whole suite after every edit.
4. Run `make verify` before opening a pull request, and read the output.
5. If you changed configuration semantics, update the Go validation, the JSON
   Schema, the documentation and the tests in the same commit.

## Commit messages

Conventional Commits: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`,
`ci:`, with an optional scope such as `feat(matching):`.

Explain **why** in the body. The diff already shows what changed; what a future
reader needs is the reason a decision was made, especially when the obvious
alternative was rejected.

## What reviewers look for

- **Determinism.** Any map iteration without sorting, any wall clock read
  inside the matcher, any randomness in selection is a defect.
- **Boundaries.** Nothing that launches, schedules, authorizes or verifies.
- **Input safety.** New fields that can hold a path or reach a subprocess need
  validation.
- **Diagnostics.** Does the error say which file and which field?
- **Dependencies.** HowlForge carries exactly one. Adding another is an
  architectural decision that needs to be argued, and CI will fail until it is.
- **Tests that protect behavior**, not tests that exist to raise coverage.

## Adding a runtime, role or capability

Usually no Go change is needed:

- **A runtime**: add one file to `config/runtimes/`. If that is not enough, the
  abstraction has failed and the fix belongs in the data model.
- **A role**: add one file to `config/roles/`.
- **A capability**: add the name to `config/capabilities.yaml`.

Then run `howlforge validate`.

## Reporting a problem

Include the command you ran, the `--json` output, and the relevant configuration
files with any secrets removed. `howlforge doctor` output is usually the fastest
way to establish what your environment actually resolved to.
