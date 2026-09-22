# HOWL-0042 handoff summary

## What this task is

Add the runtime registry to HowlForge: the package that loads runtime
definitions from configuration, refuses duplicates and malformed documents, and
answers selector queries from roles.

## What happened

The runtime registry is implemented and its unit tests pass. Work stopped
because the Cursor session allowance was exhausted at 14:01Z, not because
anything failed. The reset is expected at 18:00Z, but the remaining work does
not need to wait for it: any runtime that meets the implementer role's
requirements can continue.

## What the replacement needs to know

The registry is deliberately independent of the roles package. It takes
selector fields as plain strings in `MatchesRef` rather than importing
`roles.RuntimeRef`, which keeps the two packages independently testable and
avoids an import cycle. Keep that shape.

Runtime documents are strictly declarative. `TestRuntimeDefinitionsAreDeclarativeOnly`
asserts that a document carrying a `command` field is refused. That test is
protecting a security property, not a style preference, so do not relax it to
make a new field parse.

## State of the tree

Clean at the commit recorded in `checkpoint.json`. Nothing is half written.
