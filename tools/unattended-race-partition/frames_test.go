package main

import (
	"bytes"
	"reflect"
	"testing"
)

func TestFrameScannerRecordsTopLevelStartsAcrossChunks(t *testing.T) {
	input := "\x16=== RUN   TestA\n" +
		"\x16=== RUN   TestA/sub_one\n" +
		"output that mentions === RUN   TestFake\n" +
		"\x16--- PASS: TestA (0.00s)\n" +
		"\x16=== NAME  \n" +
		"partial output\x16=== RUN   TestB\n" +
		"\x16=== NAME  TestB\n" +
		"text with === NAME inside\n" +
		"\x16=== RUN   ExampleC\r\n" +
		"tail without newline"
	var out bytes.Buffer
	scanner := newFrameScanner(&out)
	for i := 0; i < len(input); i += 3 {
		chunk := input[i:min(i+3, len(input))]
		if n, err := scanner.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Fatalf("Write(%q) = %d, %v", chunk, n, err)
		}
	}
	scanner.Flush()
	if want := []string{"TestA", "TestB", "ExampleC"}; !reflect.DeepEqual(scanner.started, want) {
		t.Fatalf("started = %q, want %q", scanner.started, want)
	}
	want := "=== RUN   TestA\n" +
		"=== RUN   TestA/sub_one\n" +
		"output that mentions === RUN   TestFake\n" +
		"--- PASS: TestA (0.00s)\n" +
		"partial output=== RUN   TestB\n" +
		"text with === NAME inside\n" +
		"=== RUN   ExampleC\r\n" +
		"tail without newline"
	if out.String() != want {
		t.Fatalf("echo = %q, want %q", out.String(), want)
	}
}
