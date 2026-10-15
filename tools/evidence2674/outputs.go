package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func toolTexts(raw json.RawMessage) []string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []string{text}
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	out := []string{}
	for _, b := range blocks {
		switch b.Type {
		case "text", "input_text", "output_text":
			out = append(out, b.Text)
		}
	}
	return out
}

func enclosingState(texts []string) string {
	// Only the actual tool envelope's leading block can assert script completion.
	// Child commands and arbitrary stdout cannot supply this assertion.
	if len(texts) == 0 {
		return "unproven"
	}
	first := strings.TrimSpace(texts[0])
	if strings.HasPrefix(first, "Script running with cell ID ") {
		return "pending"
	}
	if first == "Script completed" || strings.HasPrefix(first, "Script completed\n") {
		return "enclosing_completed"
	}
	return "unproven"
}

func inspectOutputs(private string) {
	f, e := os.Open(filepath.Join(private, "source-witnesses.private.jsonl"))
	if e != nil {
		fail()
	}
	defer f.Close()
	f2, e := os.Open(filepath.Join(private, "source-large-witnesses.private.jsonl"))
	if e != nil {
		fail()
	}
	defer f2.Close()
	s := bufio.NewScanner(io.MultiReader(f, f2))
	s.Buffer(make([]byte, 65536), 32*1024*1024)
	counts := map[string]int{}
	out := newPrivate(filepath.Join(private, "output-final.json"))
	defer out.Close()
	for s.Scan() {
		var record struct {
			Raw json.RawMessage `json:"raw"`
		}
		if json.Unmarshal(s.Bytes(), &record) != nil {
			fail()
		}
		event, ok := unwrapEvent(record.Raw)
		if !ok || !strings.HasSuffix(event.Type, "_output") {
			continue
		}
		counts["outputs"]++
		texts := toolTexts(event.Output)
		counts[enclosingState(texts)]++
		for _, text := range texts {
			for _, marker := range []string{"Script completed", "Script running", "Process exited with code", "exit_code", "Exit code:", "Final output:", "Command failed", "Rejected"} {
				if strings.Contains(text, marker) {
					counts[marker]++
				}
			}
		}
	}
	if s.Err() != nil {
		fail()
	}
	if json.NewEncoder(out).Encode(counts) != nil {
		fail()
	}
	emit(counts)
}
