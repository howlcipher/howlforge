# Remaining work for HOWL-0042

## 1. Integration tests

The registry is covered by unit tests, but nothing yet exercises the path from
a configuration directory through the registry and into a match. Add a test
that writes a temporary configuration tree, loads it through
`pkg/howlforge.Load`, and asserts that a runtime defined only in configuration
becomes an eligible candidate.

Acceptance: a new runtime file makes a runtime selectable with no Go change.

## 2. Documentation

`docs/` does not yet describe how an operator adds a runtime. Write the
procedure: copy an example file, set the four identity fields, declare
capabilities, set `state.enabled`, run `howlforge validate`.

Acceptance: the procedure works when followed literally against a clean
checkout.

## Not in scope

Do not add a `command` or `exec` field to runtime definitions. See decision D1
in `decisions.json`. If a caller needs to know how to launch a runtime, that
belongs to HowlPlane.
