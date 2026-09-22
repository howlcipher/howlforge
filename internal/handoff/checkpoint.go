// Package handoff reads and validates the structured state a departing worker
// leaves behind.
//
// The contract this package enforces has one purpose: the next worker must be
// able to continue safely without the previous worker's chat transcript. Every
// field exists because its absence would force the replacement to guess.
package handoff

import (
	"fmt"
	"strings"
	"time"
)

// Schema versions for the artifacts in a handoff bundle.
const (
	CheckpointSchemaVersion   = "howlforge.checkpoint/v1"
	DecisionsSchemaVersion    = "howlforge.decisions/v1"
	VerificationSchemaVersion = "howlforge.verification/v1"
)

// Status is how far the departing worker got.
type Status string

const (
	// StatusPartial means work is incomplete and expected to be resumed.
	StatusPartial Status = "partial"
	// StatusComplete means the task is finished and needs no resumption.
	StatusComplete Status = "complete"
	// StatusBlocked means work cannot continue without a decision or access.
	StatusBlocked Status = "blocked"
	// StatusAbandoned means the attempt was discarded and should not be built on.
	StatusAbandoned Status = "abandoned"
)

var allStatuses = []Status{StatusPartial, StatusComplete, StatusBlocked, StatusAbandoned}

// ParseStatus validates a checkpoint status.
func ParseStatus(s string) (Status, error) {
	normalized := Status(strings.ToLower(strings.TrimSpace(s)))
	for _, candidate := range allStatuses {
		if candidate == normalized {
			return candidate, nil
		}
	}
	names := make([]string, 0, len(allStatuses))
	for _, candidate := range allStatuses {
		names = append(names, string(candidate))
	}
	return "", fmt.Errorf("unknown status %q: expected one of %s", s, strings.Join(names, ", "))
}

// Reason classifies why the worker handed off. It reuses the availability
// vocabulary for capacity conditions so a session limit reads the same word
// everywhere in HowlFutureWorks.
type Reason string

const (
	// ReasonSessionLimit means a per-session allowance ended the work.
	ReasonSessionLimit Reason = "SESSION_LIMIT"
	// ReasonQuotaLimit means a longer-horizon allowance ended the work.
	ReasonQuotaLimit Reason = "QUOTA_LIMIT"
	// ReasonRateLimit means throttling ended the work.
	ReasonRateLimit Reason = "RATE_LIMIT"
	// ReasonProviderUnavailable means the provider became unreachable.
	ReasonProviderUnavailable Reason = "PROVIDER_UNAVAILABLE"
	// ReasonWorkerFailure means the worker failed for non-capacity reasons.
	ReasonWorkerFailure Reason = "WORKER_FAILURE"
	// ReasonVoluntary means the worker chose to hand off, for example to a
	// role better suited to the remaining work.
	ReasonVoluntary Reason = "VOLUNTARY"
	// ReasonInterrupted means an operator or supervisor stopped the work.
	ReasonInterrupted Reason = "INTERRUPTED"
	// ReasonCompleted means the worker finished and is recording final state.
	ReasonCompleted Reason = "COMPLETED"
)

var allReasons = []Reason{
	ReasonSessionLimit,
	ReasonQuotaLimit,
	ReasonRateLimit,
	ReasonProviderUnavailable,
	ReasonWorkerFailure,
	ReasonVoluntary,
	ReasonInterrupted,
	ReasonCompleted,
}

// ParseReason validates a handoff reason.
func ParseReason(s string) (Reason, error) {
	normalized := Reason(strings.ToUpper(strings.TrimSpace(s)))
	for _, candidate := range allReasons {
		if candidate == normalized {
			return candidate, nil
		}
	}
	names := make([]string, 0, len(allReasons))
	for _, candidate := range allReasons {
		names = append(names, string(candidate))
	}
	return "", fmt.Errorf("unknown handoff_reason %q: expected one of %s", s, strings.Join(names, ", "))
}

// Worker identifies who did the work. It is deliberately separate from Runtime
// so a session id can be recorded without implying the session can be reopened.
type Worker struct {
	// RoleID is the workforce role the worker filled. Required.
	RoleID string `json:"role"`
	// PersonaID is the optional organizational identity.
	PersonaID string `json:"persona,omitempty"`
	// SessionID is the disposable session reference, for audit only. Nothing
	// in HowlForge attempts to reattach to it.
	SessionID string `json:"session_id,omitempty"`
}

// Runtime identifies what executed the work.
type Runtime struct {
	// RuntimeID is the HowlForge runtime registry id, when one applies.
	RuntimeID string `json:"runtime_id,omitempty"`
	// Provider is the organization serving the model.
	Provider string `json:"provider,omitempty"`
	// Model is the model that did the work. Required.
	Model string `json:"model"`
	// Client is the tool the model was reached through. Required.
	Client string `json:"runtime"`
}

// Repository binds the checkpoint to a point in version control. Without this
// the resume rules have nothing to check and every resume is a guess.
type Repository struct {
	// Root is the repository-relative or absolute path the work happened in.
	Root string `json:"root,omitempty"`
	// Branch is the branch the work happened on.
	Branch string `json:"branch,omitempty"`
	// LastVerifiedCommit is the commit at which the recorded verification
	// results were observed. Required.
	LastVerifiedCommit string `json:"last_verified_commit"`
	// HeadCommit is the commit at the moment of handoff, when it differs from
	// the last verified commit.
	HeadCommit string `json:"head_commit,omitempty"`
	// Dirty records whether the working tree had uncommitted changes.
	Dirty bool `json:"dirty"`
}

// Decision is a choice the departing worker made that the replacement must not
// silently re-litigate.
type Decision struct {
	ID         string `json:"id"`
	Decision   string `json:"decision"`
	Rationale  string `json:"rationale,omitempty"`
	Reversible bool   `json:"reversible"`
}

// Check is one verification the worker performed.
type Check struct {
	Name       string     `json:"name"`
	Command    string     `json:"command,omitempty"`
	Status     string     `json:"status"`
	Result     string     `json:"result,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
}

// Passed reports whether the check was observed to succeed. Anything other
// than an explicit pass counts as not passed, so a malformed or missing status
// can never be read as success.
func (c Check) Passed() bool {
	return strings.EqualFold(strings.TrimSpace(c.Status), "passed")
}

// Verification is the evidence block. Independence is recorded truthfully: a
// worker that verified its own output says so, and HowlProof decides what that
// is worth.
type Verification struct {
	SchemaVersion   string  `json:"schema_version,omitempty"`
	Checks          []Check `json:"checks,omitempty"`
	Independent     bool    `json:"independent"`
	VerifierRole    string  `json:"verifier_role,omitempty"`
	VerifierRuntime string  `json:"verifier_runtime,omitempty"`
}

// Checkpoint is the machine-readable core of a handoff bundle.
type Checkpoint struct {
	SchemaVersion string `json:"schema_version"`
	TaskID        string `json:"task_id"`

	// Role, Client and Model are the flat fields the HowlFutureWorks spec
	// writes. Worker and Runtime carry the richer identity when it is known.
	Role   string `json:"role"`
	Client string `json:"runtime"`
	Model  string `json:"model"`

	Worker  *Worker  `json:"worker,omitempty"`
	Machine *Runtime `json:"runtime_identity,omitempty"`

	Status       Status `json:"status"`
	SafeToResume bool   `json:"safe_to_resume"`
	Reason       Reason `json:"handoff_reason,omitempty"`

	LastVerifiedCommit string      `json:"last_verified_commit"`
	Repo               *Repository `json:"repository,omitempty"`

	Completed           []string   `json:"completed"`
	Remaining           []string   `json:"remaining"`
	ChangedFiles        []string   `json:"changed_files,omitempty"`
	Decisions           []Decision `json:"decisions,omitempty"`
	UnresolvedQuestions []string   `json:"unresolved_questions,omitempty"`
	KnownFailures       []string   `json:"known_failures,omitempty"`

	Verification *Verification `json:"verification,omitempty"`

	CreatedAt *time.Time `json:"created_at,omitempty"`
	Notes     string     `json:"notes,omitempty"`
}

// EffectiveCommit returns the commit the checkpoint should be verified
// against, preferring the repository block when present.
func (c *Checkpoint) EffectiveCommit() string {
	if c.Repo != nil && strings.TrimSpace(c.Repo.LastVerifiedCommit) != "" {
		return strings.TrimSpace(c.Repo.LastVerifiedCommit)
	}
	return strings.TrimSpace(c.LastVerifiedCommit)
}

// EffectiveRole returns the role id from whichever block carries it.
func (c *Checkpoint) EffectiveRole() string {
	if c.Worker != nil && c.Worker.RoleID != "" {
		return c.Worker.RoleID
	}
	return c.Role
}

// EffectiveModel returns the model from whichever block carries it.
func (c *Checkpoint) EffectiveModel() string {
	if c.Machine != nil && c.Machine.Model != "" {
		return c.Machine.Model
	}
	return c.Model
}

// EffectiveClient returns the client runtime from whichever block carries it.
func (c *Checkpoint) EffectiveClient() string {
	if c.Machine != nil && c.Machine.Client != "" {
		return c.Machine.Client
	}
	return c.Client
}

// EffectiveDirty reports whether the working tree was recorded as dirty.
func (c *Checkpoint) EffectiveDirty() bool {
	return c.Repo != nil && c.Repo.Dirty
}

// DecisionsDocument is the on-disk shape of decisions.json.
type DecisionsDocument struct {
	SchemaVersion string     `json:"schema_version"`
	Decisions     []Decision `json:"decisions"`
}

// VerificationDocument is the on-disk shape of verification.json.
type VerificationDocument struct {
	SchemaVersion   string  `json:"schema_version"`
	Checks          []Check `json:"checks"`
	Independent     bool    `json:"independent"`
	VerifierRole    string  `json:"verifier_role,omitempty"`
	VerifierRuntime string  `json:"verifier_runtime,omitempty"`
}
