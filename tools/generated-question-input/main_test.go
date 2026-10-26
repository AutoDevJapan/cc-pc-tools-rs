package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testCLIArgs(text string) []string {
	return []string{"--thread-id", testThread, "--turn-id", testTurn, "--item-id", testItem, "--input-sha256", inputHash(text), "--image-name", testImage}
}

func TestInputCLIReportsOnlySourceMatchAndAlwaysDeniesAssetQualification(t *testing.T) {
	text := "Review " + testImage
	rpc := &fakeInputRPC{t: t, text: text}
	var output bytes.Buffer
	exit := runInputCLI(testCLIArgs(text), &output, func(context.Context) (inputRPC, error) { return rpc, nil }, func() time.Time { return time.Unix(200, 0) })
	want := "{\"domain\":\"generated-question-input-reference-v1\",\"source_matched\":true,\"stage\":\"complete\",\"code\":\"source_reference_matched\",\"asset_qualified\":false}\n"
	if exit != 0 || output.String() != want || !rpc.closed {
		t.Fatalf("CLI exit=%d output=%s closed=%v", exit, &output, rpc.closed)
	}
}

func TestInputCLIRejectsArgumentsBeforeStartingTheReader(t *testing.T) {
	valid := testCLIArgs("Review " + testImage)
	for _, args := range [][]string{nil, append(append([]string{}, valid...), "--thread-id", testThread), append(append([]string{}, valid...), "--path", "PRIVATE-MARKER"), append(append([]string{}, valid...), "--exec", "PRIVATE-MARKER"), {"--thread-id", "PRIVATE-MARKER"}} {
		called := false
		var out bytes.Buffer
		exit := runInputCLI(args, &out, func(context.Context) (inputRPC, error) { called = true; return nil, errors.New("PRIVATE-MARKER") }, time.Now)
		if exit != 1 || called || strings.Contains(out.String(), "PRIVATE-MARKER") || !strings.Contains(out.String(), `"code":"invalid_reference"`) {
			t.Fatalf("invalid args reached reader: called=%v exit=%d output=%s", called, exit, &out)
		}
	}
}

func TestInputCLIHidesReaderErrorsAndRequiresCleanClose(t *testing.T) {
	text := "Review " + testImage
	cases := []struct {
		name    string
		connect connectInputRPC
		code    string
	}{
		{"start_error", func(context.Context) (inputRPC, error) { return nil, errors.New("PRIVATE-MARKER") }, "source_unavailable"},
		{"read_error", func(context.Context) (inputRPC, error) {
			return &fakeInputRPC{t: t, text: text, failMethod: "thread/items/read"}, nil
		}, "source_unavailable"},
		{"close_error", func(context.Context) (inputRPC, error) {
			return &fakeInputRPC{t: t, text: text, closeErr: errors.New("PRIVATE-MARKER")}, nil
		}, "source_close_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if runInputCLI(testCLIArgs(text), &out, tc.connect, func() time.Time { return time.Unix(200, 0) }) != 1 {
				t.Fatal("failure returned success")
			}
			var fields map[string]any
			if json.Unmarshal(out.Bytes(), &fields) != nil || len(fields) != 5 || fields["code"] != tc.code || fields["source_matched"] != false || fields["asset_qualified"] != false {
				t.Fatalf("unexpected wire result %s", &out)
			}
			if strings.Contains(out.String(), "PRIVATE-MARKER") || strings.Contains(out.String(), testItem) {
				t.Fatalf("private input echoed: %s", &out)
			}
		})
	}
}

func TestInputCLIWithGeneralPastInstructionIsNotAnImageGrant(t *testing.T) {
	text := "Please keep the provided HTML structure and adjust the appearance."
	var out bytes.Buffer
	exit := runInputCLI(testCLIArgs(text), &out, func(context.Context) (inputRPC, error) { return &fakeInputRPC{t: t, text: text}, nil }, func() time.Time { return time.Unix(200, 0) })
	if exit != 1 || !strings.Contains(out.String(), `"code":"image_reference_missing"`) || strings.Contains(out.String(), text) {
		t.Fatalf("generic prior instruction became an image grant: %s", &out)
	}
}

func TestInputSourceEnvironmentRejectsCallerSelectedRecordStoreAndExecutablePath(t *testing.T) {
	paths := inputPaths{Profile: "D:/synthetic-profile", Windows: "C:/Windows"}
	env := inputSourceEnvironment(paths)
	values := map[string]string{}
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	if len(values) != 10 || values["CODEX_HOME"] != filepath.Join(paths.Profile, ".codex") || values["PATH"] != filepath.Join(paths.Windows, "System32")+string(filepath.ListSeparator)+paths.Windows || values["APPDATA"] != filepath.Join(paths.Profile, "AppData", "Roaming") {
		t.Fatal("canonical source environment is not fixed")
	}
	for key := range values {
		if strings.Contains(key, "TOKEN") || strings.Contains(key, "API_KEY") {
			t.Fatal("credentials entered source environment")
		}
	}
}
