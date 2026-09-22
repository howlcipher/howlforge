package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// Schema identifiers stamped on JSON output. HowlPlane branches on these, so
// they are part of the contract and change only with a version bump.
const (
	SchemaRoles        = "howlforge.roles/v1"
	SchemaRole         = "howlforge.role_detail/v1"
	SchemaRuntimes     = "howlforge.runtimes/v1"
	SchemaRuntime      = "howlforge.runtime_detail/v1"
	SchemaPersonas     = "howlforge.personas/v1"
	SchemaPersona      = "howlforge.persona_detail/v1"
	SchemaCapabilities = "howlforge.capability_vocabulary/v1"
	SchemaMatch        = "howlforge.match/v1"
	SchemaExplain      = "howlforge.explanation/v1"
	SchemaAvailability = "howlforge.availability_report/v1"
	SchemaHandoff      = "howlforge.handoff_report/v1"
	SchemaResume       = "howlforge.resume_report/v1"
	SchemaValidation   = "howlforge.validation/v1"
	SchemaDoctor       = "howlforge.doctor/v1"
)

// emitJSON writes a value as indented JSON with a trailing newline.
//
// Indentation is fixed and struct field order is fixed, so identical inputs
// produce byte identical output. The determinism test depends on that.
func emitJSON(w io.Writer, value interface{}) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	// HTML escaping would mangle model names and reasons for no benefit here.
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

// table builds an aligned column writer for human readable output.
func table(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

// section prints a heading followed by indented key and value lines.
func section(w io.Writer, heading string) {
	fmt.Fprintf(w, "%s:\n", heading)
}

// field prints one indented label and value.
func field(w io.Writer, label, format string, args ...interface{}) {
	fmt.Fprintf(w, "  %-22s %s\n", label, fmt.Sprintf(format, args...))
}

// list prints an indented bullet list, or a dash when empty.
func list(w io.Writer, items []string) {
	if len(items) == 0 {
		fmt.Fprintln(w, "  (none)")
		return
	}
	for _, item := range items {
		fmt.Fprintf(w, "  - %s\n", item)
	}
}

// joinSorted renders a string set in lexical order.
func joinSorted(items []string) string {
	out := append([]string(nil), items...)
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// dashIfEmpty renders an empty string as a dash so columns never collapse.
func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// yesNo renders a boolean the way the CLI presents it.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
