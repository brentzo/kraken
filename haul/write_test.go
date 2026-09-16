package haul

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAtomic_RoundTripAndDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "haul.json")
	if err := WriteAtomic(path, base()); err != nil {
		t.Fatal(err)
	}
	r := Read(path)
	if r.State != StateValid {
		t.Fatalf("state = %q, err = %v", r.State, r.Err)
	}
	if r.Haul.Assignment.TaskID != base().Assignment.TaskID {
		t.Error("round trip lost the task id")
	}
	if !strings.HasPrefix(r.Digest, "sha256:") {
		t.Errorf("digest = %q, want a sha256 of the exact bytes read", r.Digest)
	}
	// Same bytes, same digest. KRK-002-R08.
	if Read(path).Digest != r.Digest {
		t.Error("digest must be stable for identical bytes")
	}
}

func TestWriteAtomic_LeavesNoTempBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "haul.json")
	if err := WriteAtomic(path, base()); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if IsTemp(e.Name()) {
			t.Errorf("temp file %q survived the write", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("want exactly the haul in the directory, got %d entries", len(entries))
	}
}

func TestWriteAtomic_RefusesInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "haul.json")
	bad := base().with(func(h *Haul) { h.Claims.Outcome = "made_up" })
	if err := WriteAtomic(path, bad); err == nil {
		t.Fatal("want a refusal to write an invalid haul")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a refused write must not leave a file behind")
	}
}

func TestWriteStub_ReadsBackAsStub(t *testing.T) {
	// The truthful default is on disk before the tentacle is spawned, so a
	// tentacle that never writes does not leave the outcome to a guess.
	path := filepath.Join(t.TempDir(), "haul.json")
	a := base().Assignment
	if err := WriteStub(path, a, "claude-code"); err != nil {
		t.Fatal(err)
	}
	r := Read(path)
	if r.State != StateStub {
		t.Errorf("state = %q, want stub", r.State)
	}
	if r.State.Admissible() {
		t.Error("a stub must never be admissible to the beak")
	}
}

func TestPreserveRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "haul.json")
	body := []byte(`{"kraken_haul":"nope"}`)
	if err := PreserveRejected(path, body); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path + ".rejected")
	if err != nil || string(got) != string(body) {
		t.Errorf("rejected bytes not preserved verbatim: %v %q", err, got)
	}
}
