package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJournalResumesEachCommittedStageWithoutTrustingAnotherContainer(t *testing.T) {
	p, _ := normalProfileFor("burst-3122")
	root := t.TempDir()
	j := burstJournal{root: root, profile: p, containerID: strings.Repeat("a", 64)}
	state := burstDrainState{ID: 55}
	for _, advance := range []func(){
		func() {},
		func() { state.LabelsWithdrawn = true },
		func() { state.RegistrationRemoved = true },
		func() { state.ListenerStopped = true },
		func() { state.ProcessDrained = true },
	} {
		advance()
		if err := j.save(&state); err != nil {
			t.Fatal(err)
		}
		loaded, found, err := j.pending()
		if err != nil || !found || loaded != state {
			t.Fatalf("committed progress lost: %+v %v", loaded, err)
		}
	}
	other := j
	other.containerID = strings.Repeat("b", 64)
	if _, _, err := other.pending(); err == nil {
		t.Fatal("another container's receipt was adopted")
	}
	state.ConfigCleared = true
	if err := j.save(&state); err != nil {
		t.Fatal(err)
	}
	if _, found, err := j.pending(); err != nil || found {
		t.Fatal("completed cleanup remained pending")
	}
}

func TestJournalIgnoresUnpublishedTemporaryDataAndRejectsConflictingBinding(t *testing.T) {
	p, _ := normalProfileFor("burst-3122")
	j := burstJournal{root: t.TempDir(), profile: p, containerID: strings.Repeat("a", 64)}
	if err := os.WriteFile(filepath.Join(j.root, ".pending-interrupted"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	state := burstDrainState{ID: 55}
	if err := j.save(&state); err != nil {
		t.Fatal(err)
	}
	if err := j.save(&state); err != nil {
		t.Fatal("idempotent publication changed an existing receipt", err)
	}
	other := j
	other.containerID = strings.Repeat("b", 64)
	if err := other.save(&state); err == nil {
		t.Fatal("binding overwrite was accepted")
	}
}

func TestJournalBoundsCompletedHistoryAndNeverDeletesPendingEvidence(t *testing.T) {
	p, _ := normalProfileFor("burst-3122")
	j := burstJournal{root: t.TempDir(), profile: p, containerID: strings.Repeat("a", 64)}
	for id := int64(1); id <= 20; id++ {
		s := burstDrainState{ID: id, LabelsWithdrawn: true, RegistrationRemoved: true, ListenerStopped: true, ProcessDrained: true, ConfigCleared: true}
		if err := j.save(&s); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(j.root)
	if err != nil || len(entries) > 9 {
		t.Fatal("completed history was unbounded", err)
	}
	s := burstDrainState{ID: 21}
	if err := j.save(&s); err != nil {
		t.Fatal(err)
	}
	if _, found, err := j.pending(); err != nil || !found {
		t.Fatal("pending evidence was reclaimed")
	}
}

func TestJournalRetentionCrashDoesNotTurnFinishedEvidenceIntoPendingWork(t *testing.T) {
	p, _ := normalProfileFor("burst-3122")
	j := burstJournal{root: t.TempDir(), profile: p, containerID: strings.Repeat("a", 64)}
	s := burstDrainState{ID: 55, LabelsWithdrawn: true, RegistrationRemoved: true, ListenerStopped: true, ProcessDrained: true, ConfigCleared: true}
	if err := j.save(&s); err != nil {
		t.Fatal(err)
	}
	b, _ := j.binding(55)
	dir := filepath.Join(j.root, "runner-55")
	if err := publishJournal(filepath.Join(dir, "retired.json"), burstRetirement{b, s}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "binding.json")); err != nil {
		t.Fatal(err)
	}
	if _, pending, err := j.pending(); err != nil || pending {
		t.Fatal("retention crash blocked supply", err)
	}
	if err := j.prune(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("interrupted retention was not resumed")
	}
}

func TestJournalRejectsUnknownFilesAndKeepsTheirContents(t *testing.T) {
	p, _ := normalProfileFor("burst-3122")
	j := burstJournal{root: t.TempDir(), profile: p, containerID: strings.Repeat("a", 64)}
	s := burstDrainState{ID: 55}
	if err := j.save(&s); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(j.root, "runner-55", "unknown.private")
	if err := os.WriteFile(path, []byte("owned-fixture-retain"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := j.pending(); err == nil {
		t.Fatal("unknown evidence was ignored")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "owned-fixture-retain" {
		t.Fatal("unknown evidence was reclaimed")
	}
}
