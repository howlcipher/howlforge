package availability

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/howlcipher/howlforge/internal/config"
)

// SchemaVersion is the only availability document version this build reads.
const SchemaVersion = "howlforge.availability/v1"

// EnvStatePath is the environment variable naming the state file.
const EnvStatePath = "HOWLFORGE_STATE"

// document is the on-disk shape of an availability state file.
//
// HowlForge only ever reads this file. It is written by whoever observes the
// providers, which in HowlFutureWorks means HowlPlane and HowlRelay. That
// division is deliberate: HowlForge does not probe providers, so it can never
// be the component that trips a rate limit while checking for one.
type document struct {
	SchemaVersion string          `json:"schema_version"`
	GeneratedAt   *time.Time      `json:"generated_at,omitempty"`
	Runtimes      []documentEntry `json:"runtimes"`
}

type documentEntry struct {
	RuntimeID  string     `json:"runtime_id"`
	State      string     `json:"state"`
	Reason     string     `json:"reason,omitempty"`
	ResetAt    *time.Time `json:"reset_at,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	Source     string     `json:"source,omitempty"`
}

// MaxStateFileBytes caps the availability file. It is far larger than any
// plausible fleet and small enough to keep a hostile file harmless.
const MaxStateFileBytes = 4 << 20

// LoadFile reads an availability snapshot from path.
//
// A missing file is not an error: it yields the empty snapshot, under which
// every runtime reads as UNKNOWN and therefore remains usable. Refusing to
// operate without a state file would make a fresh install useless, while
// treating absence as unavailability would disable the whole workforce.
func LoadFile(path string) (*Snapshot, error) {
	if strings.TrimSpace(path) == "" {
		return EmptySnapshot(), nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve availability state path %q: %w", path, err)
	}
	if _, err := os.Stat(abs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			snapshot := EmptySnapshot()
			return snapshot, nil
		}
		return nil, fmt.Errorf("stat availability state %q: %w", path, err)
	}

	loader, err := config.NewLoader(filepath.Dir(abs), config.WithMaxFileBytes(MaxStateFileBytes))
	if err != nil {
		return nil, fmt.Errorf("availability state %q: %w", path, err)
	}
	data, err := loader.ReadFile(filepath.Base(abs))
	if err != nil {
		return nil, fmt.Errorf("availability state %q: %w", path, err)
	}

	var doc document
	if err := config.DecodeJSONStrict(data, &doc); err != nil {
		return nil, fmt.Errorf("availability state %q: %w", path, err)
	}
	if doc.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("availability state %q: schema_version must be %q, got %q",
			path, SchemaVersion, doc.SchemaVersion)
	}

	reports := make([]Report, 0, len(doc.Runtimes))
	for i, entry := range doc.Runtimes {
		state, err := ParseState(entry.State)
		if err != nil {
			return nil, fmt.Errorf("availability state %q: runtimes[%d] (%s): %w",
				path, i, entry.RuntimeID, err)
		}
		// A capacity limit with no reset_at is legitimate and is kept as is,
		// so the CLI reports "reset unknown" rather than inventing a time.
		reports = append(reports, Report{
			RuntimeID:  entry.RuntimeID,
			State:      state,
			Reason:     entry.Reason,
			ResetAt:    entry.ResetAt,
			ObservedAt: entry.ObservedAt,
			Source:     entry.Source,
		})
	}
	snapshot, err := NewSnapshot(reports)
	if err != nil {
		return nil, fmt.Errorf("availability state %q: %w", path, err)
	}
	snapshot.Path = abs
	return snapshot, nil
}

// DefaultStatePath resolves where the availability file lives, honoring an
// explicit flag first, then the environment, then the user config directory.
func DefaultStatePath(explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	if fromEnv := strings.TrimSpace(os.Getenv(EnvStatePath)); fromEnv != "" {
		return fromEnv
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "howlforge", "availability.json")
}
