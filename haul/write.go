package haul

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TempSuffix marks a transient write in progress. Readers ignore these.
// KRK-002-R34.
const TempSuffix = ".tmp"

// WriteAtomic writes h to path via a temp file in the same directory followed
// by rename(2), which is atomic within a filesystem.
//
// The protocol, per KRK-002-R32 and R33:
//   - the temp file is created in the SAME directory, so the rename does not
//     cross a filesystem boundary and therefore cannot degrade to a copy
//   - it is opened O_CREAT|O_EXCL and carries the writer pid, so two writers
//     cannot collide on it
//   - it is fsynced before the rename, and the directory is fsynced after, so
//     a crash cannot leave a rename that points at unflushed bytes
//
// A haul is immutable once written. A corrected report is a new dive
// directory, never an edit. KRK-002-R37.
func WriteAtomic(path string, h *Haul) (err error) {
	if err := h.Validate(); err != nil {
		return fmt.Errorf("refusing to write an invalid haul: %w", err)
	}
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if len(b) > MaxBytes {
		return fmt.Errorf("haul is %d bytes, over the %d byte cap", len(b), MaxBytes)
	}

	dir := filepath.Dir(path)
	tmp := fmt.Sprintf("%s.%d%s", path, os.Getpid(), TempSuffix)

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating temp haul: %w", err)
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()

	if _, err = f.Write(b); err != nil {
		return fmt.Errorf("writing temp haul: %w", err)
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("fsyncing temp haul: %w", err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("closing temp haul: %w", err)
	}
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("renaming haul into place: %w", err)
	}
	return syncDir(dir)
}

// syncDir flushes the directory entry so the rename survives a crash.
// A failure here is not fatal on filesystems that do not support it.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer d.Close()
	_ = d.Sync()
	return nil
}

// IsTemp reports whether name is a transient write that readers must ignore
// rather than promote. KRK-002-R34.
func IsTemp(name string) bool { return strings.HasSuffix(name, TempSuffix) }

// WriteStub pre-writes the truthful default before the tentacle is spawned.
// If the tentacle never writes, the document already on disk says so rather
// than the directory being empty and the outcome being guessed. KRK-002-R30.
func WriteStub(path string, a Assignment, agentName string) error {
	h := &Haul{
		KrakenHaul:    Magic,
		SchemaVersion: SchemaVersion,
		Assignment:    a,
		Agent:         Agent{Name: agentName},
		Claims: Claims{
			Outcome: OutcomeCrashed,
			Summary: "pre-spawn stub; the tentacle had not written when this was created",
			OutcomeDetail: &OutcomeDetail{
				ReasonCode: ReasonProcessDied,
				Message:    "no haul was written by the tentacle",
			},
			Changes: Changes{Files: []FileChange{}},
			Checks:  []Check{},
			NotDone: []NotDoneItem{},
		},
	}
	return WriteAtomic(path, h)
}

// PreserveRejected writes the original bytes of an unusable document beside
// the haul path, so a validation failure is diagnosable rather than discarded.
// KRK-002-R42.
func PreserveRejected(haulPath string, b []byte) error {
	return os.WriteFile(haulPath+".rejected", b, 0o600)
}
