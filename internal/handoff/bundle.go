package handoff

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/howlcipher/howlforge/internal/config"
)

// File names inside a handoff bundle.
const (
	FileCheckpoint    = "checkpoint.json"
	FileSummary       = "summary.md"
	FileDecisions     = "decisions.json"
	FileChangedFiles  = "changed-files.txt"
	FileRemainingWork = "remaining-work.md"
	FileVerification  = "verification.json"
)

// RequiredFiles is the full bundle contract. All six must exist. A bundle
// missing a file is not "mostly fine": the next worker would have to infer the
// missing part, and inference is exactly what this contract removes.
var RequiredFiles = []string{
	FileCheckpoint,
	FileSummary,
	FileDecisions,
	FileChangedFiles,
	FileRemainingWork,
	FileVerification,
}

// MaxBundleFileBytes caps each artifact in a bundle.
const MaxBundleFileBytes = 4 << 20

// Severity distinguishes a contract violation from an advisory.
type Severity string

const (
	// SeverityError means the bundle does not satisfy the contract.
	SeverityError Severity = "error"
	// SeverityWarning means the bundle is usable but something is worth noting.
	SeverityWarning Severity = "warning"
)

// Diagnostic is one finding about a bundle.
type Diagnostic struct {
	Severity Severity `json:"severity"`
	File     string   `json:"file,omitempty"`
	Field    string   `json:"field,omitempty"`
	Message  string   `json:"message"`
}

// String renders a diagnostic for terminal output.
func (d Diagnostic) String() string {
	location := d.File
	if d.Field != "" {
		if location != "" {
			location += ":" + d.Field
		} else {
			location = d.Field
		}
	}
	if location == "" {
		return fmt.Sprintf("%s: %s", d.Severity, d.Message)
	}
	return fmt.Sprintf("%s: %s: %s", d.Severity, location, d.Message)
}

// Bundle is a loaded handoff directory.
type Bundle struct {
	Path          string        `json:"path"`
	Checkpoint    *Checkpoint   `json:"checkpoint,omitempty"`
	Summary       string        `json:"-"`
	RemainingWork string        `json:"-"`
	ChangedFiles  []string      `json:"changed_files,omitempty"`
	Decisions     []Decision    `json:"decisions,omitempty"`
	Verification  *Verification `json:"verification,omitempty"`
	Present       []string      `json:"present_files"`
	Missing       []string      `json:"missing_files,omitempty"`
	Diagnostics   []Diagnostic  `json:"diagnostics"`
}

// Valid reports whether the bundle satisfies the contract.
func (b *Bundle) Valid() bool {
	for _, diagnostic := range b.Diagnostics {
		if diagnostic.Severity == SeverityError {
			return false
		}
	}
	return true
}

// Errors returns only the error-severity diagnostics.
func (b *Bundle) Errors() []Diagnostic {
	var out []Diagnostic
	for _, diagnostic := range b.Diagnostics {
		if diagnostic.Severity == SeverityError {
			out = append(out, diagnostic)
		}
	}
	return out
}

func (b *Bundle) errorf(file, field, format string, args ...interface{}) {
	b.Diagnostics = append(b.Diagnostics, Diagnostic{
		Severity: SeverityError, File: file, Field: field,
		Message: fmt.Sprintf(format, args...),
	})
}

func (b *Bundle) warnf(file, field, format string, args ...interface{}) {
	b.Diagnostics = append(b.Diagnostics, Diagnostic{
		Severity: SeverityWarning, File: file, Field: field,
		Message: fmt.Sprintf(format, args...),
	})
}

// Load reads and validates a handoff bundle.
//
// A malformed bundle is reported through diagnostics rather than a Go error:
// the caller wants the whole list of what is wrong, not just the first
// problem. An error return means the directory itself could not be read.
func Load(dir string) (*Bundle, error) {
	loader, err := config.NewLoader(dir, config.WithMaxFileBytes(MaxBundleFileBytes))
	if err != nil {
		return nil, err
	}
	bundle := &Bundle{Path: loader.Root(), Diagnostics: []Diagnostic{}}

	for _, name := range RequiredFiles {
		if loader.Exists(name) {
			bundle.Present = append(bundle.Present, name)
		} else {
			bundle.Missing = append(bundle.Missing, name)
			bundle.errorf(name, "", "required file is missing")
		}
	}

	bundle.loadCheckpoint(loader)
	bundle.loadSummary(loader)
	bundle.loadRemainingWork(loader)
	bundle.loadChangedFiles(loader)
	bundle.loadDecisions(loader)
	bundle.loadVerification(loader)
	bundle.crossCheck()

	return bundle, nil
}

func (b *Bundle) loadCheckpoint(loader *config.Loader) {
	data, err := loader.ReadFile(FileCheckpoint)
	if err != nil {
		if !errors.Is(err, config.ErrNotFound) {
			b.errorf(FileCheckpoint, "", "%v", err)
		}
		return
	}
	var checkpoint Checkpoint
	if err := config.DecodeJSONStrict(data, &checkpoint); err != nil {
		b.errorf(FileCheckpoint, "", "%v", err)
		return
	}
	b.Checkpoint = &checkpoint
	b.validateCheckpoint(&checkpoint)
}

func (b *Bundle) validateCheckpoint(c *Checkpoint) {
	if c.SchemaVersion != CheckpointSchemaVersion {
		b.errorf(FileCheckpoint, "schema_version",
			"must be %q, got %q", CheckpointSchemaVersion, c.SchemaVersion)
		// Continue validating: an operator fixing a version bump benefits from
		// seeing the rest of the problems in the same pass.
	}
	if strings.TrimSpace(c.TaskID) == "" {
		b.errorf(FileCheckpoint, "task_id", "must not be empty")
	}
	if strings.TrimSpace(c.EffectiveRole()) == "" {
		b.errorf(FileCheckpoint, "role", "must not be empty")
	}
	if strings.TrimSpace(c.EffectiveModel()) == "" {
		b.errorf(FileCheckpoint, "model", "must not be empty")
	}
	if strings.TrimSpace(c.EffectiveClient()) == "" {
		b.errorf(FileCheckpoint, "runtime", "must not be empty")
	}
	if _, err := ParseStatus(string(c.Status)); err != nil {
		b.errorf(FileCheckpoint, "status", "%v", err)
	}
	if c.Reason != "" {
		if _, err := ParseReason(string(c.Reason)); err != nil {
			b.errorf(FileCheckpoint, "handoff_reason", "%v", err)
		}
	}

	commit := c.EffectiveCommit()
	if commit == "" {
		b.errorf(FileCheckpoint, "last_verified_commit",
			"must not be empty: a checkpoint with no commit cannot be resume checked")
	} else if !IsCommitish(commit) {
		b.errorf(FileCheckpoint, "last_verified_commit",
			"%q is not a hexadecimal commit hash", commit)
	}
	if c.Repo != nil && c.Repo.HeadCommit != "" && !IsCommitish(c.Repo.HeadCommit) {
		b.errorf(FileCheckpoint, "repository.head_commit",
			"%q is not a hexadecimal commit hash", c.Repo.HeadCommit)
	}

	if c.Status == StatusPartial && len(c.Remaining) == 0 {
		b.errorf(FileCheckpoint, "remaining",
			"a partial checkpoint must list remaining work explicitly")
	}
	if c.SafeToResume && c.Status == StatusAbandoned {
		b.errorf(FileCheckpoint, "safe_to_resume",
			"an abandoned checkpoint must not be marked safe to resume")
	}
	if c.SafeToResume && len(c.UnresolvedQuestions) > 0 {
		b.warnf(FileCheckpoint, "safe_to_resume",
			"marked safe to resume while %d unresolved question(s) remain", len(c.UnresolvedQuestions))
	}
	for i, path := range c.ChangedFiles {
		if err := validateRelPath(path); err != nil {
			b.errorf(FileCheckpoint, fmt.Sprintf("changed_files[%d]", i), "%v", err)
		}
	}
}

func (b *Bundle) loadSummary(loader *config.Loader) {
	data, err := loader.ReadFile(FileSummary)
	if err != nil {
		if !errors.Is(err, config.ErrNotFound) {
			b.errorf(FileSummary, "", "%v", err)
		}
		return
	}
	b.Summary = string(data)
	if strings.TrimSpace(b.Summary) == "" {
		b.errorf(FileSummary, "", "must not be empty: the replacement worker reads this first")
	}
}

func (b *Bundle) loadRemainingWork(loader *config.Loader) {
	data, err := loader.ReadFile(FileRemainingWork)
	if err != nil {
		if !errors.Is(err, config.ErrNotFound) {
			b.errorf(FileRemainingWork, "", "%v", err)
		}
		return
	}
	b.RemainingWork = string(data)
	if strings.TrimSpace(b.RemainingWork) == "" && b.Checkpoint != nil && b.Checkpoint.Status == StatusPartial {
		b.errorf(FileRemainingWork, "", "must not be empty for a partial checkpoint")
	}
}

func (b *Bundle) loadChangedFiles(loader *config.Loader) {
	data, err := loader.ReadFile(FileChangedFiles)
	if err != nil {
		if !errors.Is(err, config.ErrNotFound) {
			b.errorf(FileChangedFiles, "", "%v", err)
		}
		return
	}
	// An empty file is legitimate: a worker may have changed nothing yet.
	b.ChangedFiles = []string{}
	for i, line := range strings.Split(string(data), "\n") {
		entry := strings.TrimSpace(line)
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}
		if err := validateRelPath(entry); err != nil {
			b.errorf(FileChangedFiles, fmt.Sprintf("line %d", i+1), "%v", err)
			continue
		}
		b.ChangedFiles = append(b.ChangedFiles, entry)
	}
}

func (b *Bundle) loadDecisions(loader *config.Loader) {
	data, err := loader.ReadFile(FileDecisions)
	if err != nil {
		if !errors.Is(err, config.ErrNotFound) {
			b.errorf(FileDecisions, "", "%v", err)
		}
		return
	}
	var doc DecisionsDocument
	if err := config.DecodeJSONStrict(data, &doc); err != nil {
		b.errorf(FileDecisions, "", "%v", err)
		return
	}
	if doc.SchemaVersion != DecisionsSchemaVersion {
		b.errorf(FileDecisions, "schema_version",
			"must be %q, got %q", DecisionsSchemaVersion, doc.SchemaVersion)
	}
	seen := map[string]bool{}
	for i, decision := range doc.Decisions {
		if strings.TrimSpace(decision.ID) == "" {
			b.errorf(FileDecisions, fmt.Sprintf("decisions[%d].id", i), "must not be empty")
			continue
		}
		if seen[decision.ID] {
			b.errorf(FileDecisions, fmt.Sprintf("decisions[%d].id", i),
				"duplicate decision id %q", decision.ID)
		}
		seen[decision.ID] = true
		if strings.TrimSpace(decision.Decision) == "" {
			b.errorf(FileDecisions, fmt.Sprintf("decisions[%d].decision", i), "must not be empty")
		}
	}
	b.Decisions = doc.Decisions
}

func (b *Bundle) loadVerification(loader *config.Loader) {
	data, err := loader.ReadFile(FileVerification)
	if err != nil {
		if !errors.Is(err, config.ErrNotFound) {
			b.errorf(FileVerification, "", "%v", err)
		}
		return
	}
	var doc VerificationDocument
	if err := config.DecodeJSONStrict(data, &doc); err != nil {
		b.errorf(FileVerification, "", "%v", err)
		return
	}
	if doc.SchemaVersion != VerificationSchemaVersion {
		b.errorf(FileVerification, "schema_version",
			"must be %q, got %q", VerificationSchemaVersion, doc.SchemaVersion)
	}
	for i, check := range doc.Checks {
		if strings.TrimSpace(check.Name) == "" {
			b.errorf(FileVerification, fmt.Sprintf("checks[%d].name", i), "must not be empty")
		}
		switch strings.ToLower(strings.TrimSpace(check.Status)) {
		case "passed", "failed", "skipped", "errored":
		default:
			b.errorf(FileVerification, fmt.Sprintf("checks[%d].status", i),
				"must be passed, failed, skipped or errored, got %q", check.Status)
		}
	}
	b.Verification = &Verification{
		SchemaVersion:   doc.SchemaVersion,
		Checks:          doc.Checks,
		Independent:     doc.Independent,
		VerifierRole:    doc.VerifierRole,
		VerifierRuntime: doc.VerifierRuntime,
	}
}

// crossCheck refuses bundles whose artifacts contradict each other. Two
// disagreeing records of the same fact are worse than one record, because the
// replacement worker has no way to know which is true.
func (b *Bundle) crossCheck() {
	if b.Checkpoint == nil {
		return
	}
	if len(b.Checkpoint.ChangedFiles) > 0 && b.ChangedFiles != nil {
		if !sameStringSet(b.Checkpoint.ChangedFiles, b.ChangedFiles) {
			b.errorf(FileChangedFiles, "",
				"disagrees with checkpoint.json changed_files; the two records must match")
		}
	}
	if len(b.Checkpoint.Decisions) > 0 && b.Decisions != nil {
		if len(b.Checkpoint.Decisions) != len(b.Decisions) {
			b.errorf(FileDecisions, "",
				"records %d decision(s) but checkpoint.json records %d",
				len(b.Decisions), len(b.Checkpoint.Decisions))
		}
	}
	if b.Checkpoint.Verification != nil && b.Verification != nil {
		if b.Checkpoint.Verification.Independent != b.Verification.Independent {
			b.errorf(FileVerification, "independent",
				"disagrees with checkpoint.json verification.independent")
		}
	}
	if b.Verification != nil && b.Checkpoint.SafeToResume {
		for _, check := range b.Verification.Checks {
			if strings.EqualFold(strings.TrimSpace(check.Status), "failed") {
				b.warnf(FileVerification, "checks",
					"check %q failed while the checkpoint claims it is safe to resume", check.Name)
			}
		}
	}
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left := append([]string(nil), a...)
	right := append([]string(nil), b...)
	sort.Strings(left)
	sort.Strings(right)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// validateRelPath refuses absolute paths and parent traversal in recorded file
// lists. A checkpoint is untrusted input, and a path list is the obvious place
// to hide an escape from the repository it claims to describe.
func validateRelPath(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return errors.New("path must not be empty")
	}
	if len(trimmed) > 4096 {
		return errors.New("path is longer than 4096 characters")
	}
	if strings.ContainsRune(trimmed, 0) {
		return errors.New("path contains a null byte")
	}
	if filepath.IsAbs(trimmed) || strings.HasPrefix(trimmed, "/") {
		return fmt.Errorf("path %q must be repository relative, not absolute", trimmed)
	}
	cleaned := filepath.ToSlash(filepath.Clean(trimmed))
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("path %q escapes the repository root", trimmed)
	}
	return nil
}

// IsCommitish reports whether a string is a plausible git object name.
//
// This is also the injection guard for the resume checker: only strings that
// pass here are ever handed to git, so a commit field containing shell
// metacharacters or a leading dash is refused before it reaches an argument
// list.
func IsCommitish(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}
