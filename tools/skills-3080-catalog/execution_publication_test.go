package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func executionDiskFixture(t *testing.T) (catalog, manifest, root, output string) {
	t.Helper()
	root = t.TempDir()
	input, m, _ := executionFixture()
	catalog, manifest, output = filepath.Join(root, "catalog.jsonl"), filepath.Join(root, "manifest.json"), filepath.Join(root, "queue.jsonl")
	ledger := []byte(`{"entries":[],"excluded_entries":[]}`)
	h := sha256.Sum256(ledger)
	m.Ledger = ExecutionArtifact{File: "skills-3080-ledger.json", SHA: hex.EncodeToString(h[:]), Rows: 0}
	for name, data := range map[string][]byte{catalog: []byte(input), filepath.Join(root, m.Ledger.File): ledger} {
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(manifest, data, 0600); err != nil {
		t.Fatal(err)
	}
	return
}

func TestExecutionPublicationDoesNotLeaveFinalNamesOnFailure(t *testing.T) {
	for _, name := range []string{"late_catalog_error", "summary_collision"} {
		t.Run(name, func(t *testing.T) {
			catalog, manifest, root, output := executionDiskFixture(t)
			if name == "late_catalog_error" {
				f, err := os.OpenFile(catalog, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := f.WriteString("not-json\n")
				closeErr := f.Close()
				if writeErr != nil || closeErr != nil {
					t.Fatalf("fixture write: %v / %v", writeErr, closeErr)
				}
			} else if err := os.WriteFile(output+".summary.json", []byte("existing summary"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := writeExecutionQueue(catalog, manifest, root, output); err == nil {
				t.Fatal("invalid publication succeeded")
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("failed publication left final queue: %v", err)
			}
			if name == "summary_collision" {
				got, err := os.ReadFile(output + ".summary.json")
				if err != nil || string(got) != "existing summary" {
					t.Fatal("existing summary changed")
				}
			} else if _, err := os.Lstat(output + ".summary.json"); !os.IsNotExist(err) {
				t.Fatalf("failed publication left a summary: %v", err)
			}
		})
	}
}

func TestExecutionPublicationRecoversOnlyTheExactQueueBeforeItsSummary(t *testing.T) {
	catalog, manifest, root, output := executionDiskFixture(t)
	if err := writeExecutionQueue(catalog, manifest, root, output); err != nil {
		t.Fatal(err)
	}
	queue, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	// A stop after publishing the complete queue but before the commit marker.
	if err = os.Remove(output + ".summary.json"); err != nil {
		t.Fatal(err)
	}
	if err = writeExecutionQueue(catalog, manifest, root, output); err != nil {
		t.Fatalf("exact incomplete pair cannot be resumed: %v", err)
	}
	var summary ExecutionSummary
	data, err := os.ReadFile(output + ".summary.json")
	if err != nil || json.Unmarshal(data, &summary) != nil {
		t.Fatal("completed summary unavailable")
	}
	h := sha256.Sum256(queue)
	if summary.State != "complete" || summary.QueueSHA != hex.EncodeToString(h[:]) {
		t.Fatal("completion marker is not bound to the complete queue")
	}
	if err = os.Remove(output + ".summary.json"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(output, []byte("different interrupted output"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = writeExecutionQueue(catalog, manifest, root, output); err == nil {
		t.Fatal("mismatching old queue replaced")
	}
	got, err := os.ReadFile(output)
	if err != nil || !strings.HasPrefix(string(got), "different interrupted output") {
		t.Fatal("mismatching existing output changed")
	}
	if _, err = os.Lstat(output + ".summary.json"); !os.IsNotExist(err) {
		t.Fatal("mismatching queue acquired a completion marker")
	}
}

func TestExecutionPublicationFailureRetainsACompleteQueueForRecovery(t *testing.T) {
	for _, reused := range []bool{false, true} {
		t.Run(map[bool]string{false: "new_queue", true: "existing_exact_queue"}[reused], func(t *testing.T) {
			root := t.TempDir()
			queue, summary, output := filepath.Join(root, "stage-queue"), filepath.Join(root, "stage-summary"), filepath.Join(root, "queue.jsonl")
			body := []byte("complete queue bytes\n")
			for name, data := range map[string][]byte{queue: body, summary: []byte("complete summary bytes\n")} {
				if err := os.WriteFile(name, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if reused {
				if err := os.WriteFile(output, body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			h := sha256.Sum256(body)
			calls := 0
			injected := errors.New("summary publication interrupted")
			err := publishExecutionPair(queue, summary, output, hex.EncodeToString(h[:]), func(from, to string) error {
				calls++
				if calls == 2 {
					return injected
				}
				return os.Link(from, to)
			})
			if !errors.Is(err, injected) || calls != 2 {
				t.Fatalf("failure was not exercised: %v calls=%d", err, calls)
			}
			got, err := os.ReadFile(output)
			if err != nil || string(got) != string(body) {
				t.Fatal("complete queue removed on later failure")
			}
			if _, err = os.Lstat(output + ".summary.json"); !os.IsNotExist(err) {
				t.Fatal("failed publication left completion marker")
			}
		})
	}
}

func TestExecutionPublicationConcurrentFailureCannotRemoveTheOtherWritersQueue(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "queue.jsonl")
	body := []byte("the same complete queue\n")
	h := sha256.Sum256(body)
	wantSHA := hex.EncodeToString(h[:])
	marker, err := json.Marshal(ExecutionSummary{State: "complete", QueueSHA: wantSHA})
	if err != nil {
		t.Fatal(err)
	}
	stages := make([][2]string, 2)
	for i, label := range []string{"first", "second"} {
		stages[i] = [2]string{filepath.Join(root, label+"-queue"), filepath.Join(root, label+"-summary")}
		for file, data := range map[string][]byte{stages[i][0]: body, stages[i][1]: marker} {
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	firstReady, secondReady := make(chan struct{}), make(chan struct{})
	failFirst, publishSecond := make(chan struct{}), make(chan struct{})
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	injected := errors.New("first writer marker publication failed")
	go func() {
		firstDone <- publishExecutionPair(stages[0][0], stages[0][1], output, wantSHA, func(from, to string) error {
			if to == output+".summary.json" {
				close(firstReady)
				<-failFirst
				return injected
			}
			return os.Link(from, to)
		})
	}()
	select {
	case <-firstReady:
	case err := <-firstDone:
		t.Fatalf("first writer did not publish its complete queue: %v", err)
	}
	go func() {
		secondDone <- publishExecutionPair(stages[1][0], stages[1][1], output, wantSHA, func(from, to string) error {
			if to == output+".summary.json" {
				close(secondReady) // Existing complete queue was already matched.
				<-publishSecond
			}
			return os.Link(from, to)
		})
	}()
	select {
	case <-secondReady:
	case err := <-secondDone:
		close(failFirst)
		<-firstDone
		t.Fatalf("second writer could not match the queue: %v", err)
	}
	// First fails while the second has validated the queue but has not yet
	// published its marker. Cleanup must not invalidate the second's result.
	close(failFirst)
	firstErr := <-firstDone
	close(publishSecond)
	secondErr := <-secondDone
	if !errors.Is(firstErr, injected) || secondErr != nil {
		t.Fatalf("incorrect writer results: first=%v second=%v", firstErr, secondErr)
	}
	got, err := os.ReadFile(output)
	if err != nil || string(got) != string(body) {
		t.Fatal("successful marker lost its complete queue")
	}
	got, err = os.ReadFile(output + ".summary.json")
	if err != nil || string(got) != string(marker) {
		t.Fatal("successful marker changed or missing")
	}
}
