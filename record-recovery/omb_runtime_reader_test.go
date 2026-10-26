package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

type omb3255SyntheticRuntimeStream struct {
	*strings.Reader
	closes   int
	changes  int
	badAt    int
	closeErr error
}

func (s *omb3255SyntheticRuntimeStream) Unchanged() bool {
	s.changes++
	return s.badAt == 0 || s.changes != s.badAt
}
func (s *omb3255SyntheticRuntimeStream) Close() error { s.closes++; return s.closeErr }

func omb3255ReaderFixture() (omb3255RuntimeRef, string) {
	ref := omb3255RuntimeRef{"synthetic-authorized-case", 1792848636233, 1792848928582, "synthetic-native-thread", "synthetic-native-turn", strings.Repeat("a", 64)}
	data := `{"type":"session_meta","payload":{"id":"synthetic-native-thread","cwd":"synthetic-private-directory"}}` + "\n" +
		`{"type":"turn_context","timestamp":"2026-10-24T13:31:00Z","payload":{"turn_id":"synthetic-native-turn","model":"gpt-6-luna","effort":"low","private":"synthetic-private-body"}}` + "\n"
	return ref, data
}

func TestOMB3255RuntimeReaderQualifiedSource(t *testing.T) {
	ref := omb3255RuntimeRef{AssignmentID: "synthetic-authorized-case", StartedAt: 1792848636233, FinishedAt: 1792848928582, ThreadID: "synthetic-native-thread", TurnID: "synthetic-native-turn", ResultSHA: strings.Repeat("a", 64)}
	data := `{"type":"session_meta","payload":{"id":"synthetic-native-thread"}}` + "\n" +
		`{"type":"turn_context","timestamp":"2026-10-24T13:31:00Z","payload":{"turn_id":"synthetic-native-turn","model":"gpt-6-luna","effort":"low"}}` + "\n"
	s := &omb3255SyntheticRuntimeStream{Reader: strings.NewReader(data)}
	opens := 0
	got := omb3255InspectRuntime(omb3255RuntimeDeps{
		Prepare: func() ([]omb3255RuntimeRef, error) { return []omb3255RuntimeRef{ref}, nil },
		Open:    func() (omb3255RuntimeStream, error) { opens++; return s, nil },
		Recheck: func([]omb3255RuntimeRef) bool { return true },
	})
	if len(got) != 1 || got[0].Observation.Model == nil || *got[0].Observation.Model != "gpt-6-luna" || got[0].Observation.Effort == nil || *got[0].Observation.Effort != "low" || !got[0].Observation.ExecutionVerified || got[0].Observation.AccessGranted || opens != 1 || s.closes != 1 {
		t.Fatalf("qualified original source unavailable: records=%+v opens=%d closes=%d", got, opens, s.closes)
	}
}

func TestOMB3255RuntimeReaderFailsClosedAndCloses(t *testing.T) {
	for _, name := range []string{"prepare", "empty refs", "bad refs", "open", "nil stream", "before change", "after change", "recheck", "close", "read", "trailing malformed"} {
		t.Run(name, func(t *testing.T) {
			ref, data := omb3255ReaderFixture()
			opens, rechecks := 0, 0
			s := &omb3255SyntheticRuntimeStream{Reader: strings.NewReader(data)}
			deps := omb3255RuntimeDeps{Prepare: func() ([]omb3255RuntimeRef, error) { return []omb3255RuntimeRef{ref}, nil }, Open: func() (omb3255RuntimeStream, error) { opens++; return s, nil }, Recheck: func([]omb3255RuntimeRef) bool { rechecks++; return true }}
			switch name {
			case "prepare":
				deps.Prepare = func() ([]omb3255RuntimeRef, error) { return nil, errors.New("private source error") }
			case "empty refs":
				deps.Prepare = func() ([]omb3255RuntimeRef, error) { return nil, nil }
			case "bad refs":
				ref.ResultSHA = "bad"
			case "open":
				deps.Open = func() (omb3255RuntimeStream, error) { opens++; return s, errors.New("private open error") }
			case "nil stream":
				deps.Open = func() (omb3255RuntimeStream, error) { opens++; return nil, nil }
			case "before change":
				s.badAt = 1
			case "after change":
				s.badAt = 2
			case "recheck":
				deps.Recheck = func([]omb3255RuntimeRef) bool { rechecks++; return false }
			case "close":
				s.closeErr = errors.New("private close error")
			case "read":
				deps.Open = func() (omb3255RuntimeStream, error) {
					opens++
					return &omb3255RuntimeReadError{omb3255SyntheticRuntimeStream: s, reader: io.MultiReader(strings.NewReader(data), omb3255TransportErrorReader{})}, nil
				}
			case "trailing malformed":
				s.Reader = strings.NewReader(data + "{broken}\n")
			}
			got := omb3255InspectRuntime(deps)
			for _, row := range got {
				if row.Observation != omb3255ContextUnavailable() {
					t.Fatalf("partial observation escaped: %+v", row)
				}
			}
			beforeOpen := name == "prepare" || name == "empty refs" || name == "bad refs"
			if beforeOpen && opens != 0 {
				t.Fatal("invalid qualification opened source")
			}
			if !beforeOpen && opens != 1 {
				t.Fatal("source open count differs")
			}
			wantClose := 1
			if beforeOpen || name == "nil stream" {
				wantClose = 0
			}
			if s.closes != wantClose {
				t.Fatalf("close count=%d want=%d", s.closes, wantClose)
			}
			if name == "before change" && rechecks != 0 {
				t.Fatal("changed source reached post-admission check")
			}
		})
	}
}

type omb3255RuntimeReadError struct {
	*omb3255SyntheticRuntimeStream
	reader io.Reader
}

func (s *omb3255RuntimeReadError) Read(b []byte) (int, error) { return s.reader.Read(b) }

func TestOMB3255NativeMetadataActualFieldsAndBounds(t *testing.T) {
	ref, good := omb3255ReaderFixture()
	large := strings.Replace(good, `"private":"synthetic-private-body"`, `"private":"`+strings.Repeat("x", 128<<10)+`"`, 1)
	obs, ok := omb3255NativeMetadata([]byte(large), []omb3255RuntimeRef{ref})
	if !ok || len(obs) != 1 || obs[0].Model == nil || *obs[0].Model != "gpt-6-luna" || *obs[0].Effort != "low" {
		t.Fatal("actual fields lost behind unrelated native fields")
	}
	encoded, _ := json.Marshal(obs)
	if bytes.Contains(encoded, []byte("synthetic-private")) || bytes.Contains(encoded, []byte("xxxxx")) {
		t.Fatal("native private field escaped")
	}
	for name, data := range map[string]string{
		"wrong header":          strings.Replace(good, "synthetic-native-thread", "different-thread", 1),
		"wrong later header":    good + `{"type":"session_meta","payload":{"id":"different-thread"}}` + "\n",
		"conflicting context":   good + strings.Replace(strings.Split(good, "\n")[1], "gpt-6-luna", "gpt-6.1-sol", 1) + "\n",
		"malformed tail":        good + "{broken}\n",
		"duplicate unknown key": good + `{"type":"unused","x":1,"x":2}` + "\n",
		"missing header":        strings.Split(good, "\n")[1] + "\n",
		"truncated":             strings.TrimSuffix(good, "\n"),
		"line bound":            good + strings.Repeat(" ", (4<<20)+1) + "\n",
		"depth bound":           good + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65) + "\n",
		"bad utf8":              good + string([]byte{0xff}) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := omb3255NativeMetadata([]byte(data), []omb3255RuntimeRef{ref}); ok {
				t.Fatal("invalid native source accepted")
			}
		})
	}
	if _, ok := omb3255NativeMetadata([]byte(good+strings.Split(good, "\n")[0]+"\n"), []omb3255RuntimeRef{ref}); !ok {
		t.Fatal("same native thread header rejected")
	}
	missing := strings.Replace(good, `,"model":"gpt-6-luna"`, "", 1)
	missing = strings.Replace(missing, `"effort":"low"`, `"effort":null`, 1)
	obs, ok = omb3255NativeMetadata([]byte(missing), []omb3255RuntimeRef{ref})
	if !ok || obs[0].ModelState != "missing" || obs[0].EffortState != "null" || obs[0].Model != nil || obs[0].Effort != nil {
		t.Fatal("missing/null became requested settings")
	}
	unknown := strings.Replace(good, "gpt-6-luna", "synthetic-private-model", 1)
	obs, ok = omb3255NativeMetadata([]byte(unknown), []omb3255RuntimeRef{ref})
	encoded, _ = json.Marshal(obs)
	if !ok || obs[0].ModelState != "unrecognized" || bytes.Contains(encoded, []byte("synthetic-private-model")) {
		t.Fatal("unknown native model disclosed")
	}
}

func TestOMB3255RuntimeCLIRefusesArgumentsAndKeepsSchema(t *testing.T) {
	calls := 0
	acquire := func() omb3255RuntimeAcquisition { calls++; return omb3255RuntimeAcquisition{} }
	for _, args := range [][]string{{"--path"}, {"4559"}, {"--help"}, {"--output", "outside"}} {
		var out bytes.Buffer
		if code := runOMB3255RuntimeCLI(args, &out, acquire); code != 2 || out.String() != "{\"code\":\"omb_runtime_metadata_invalid_arguments\",\"ok\":false}\n" {
			t.Fatalf("argument refusal=%d %s", code, out.String())
		}
	}
	if calls != 0 {
		t.Fatal("arguments reached acquisition")
	}
	var out bytes.Buffer
	if runOMB3255RuntimeCLI(nil, &out, acquire) != 0 || calls != 1 {
		t.Fatal("acquisition positive control not reached")
	}
	var got map[string]any
	if json.Unmarshal(out.Bytes(), &got) != nil {
		t.Fatal("bad result JSON")
	}
	want := map[string]any{"kind": "omb-review-runtime-metadata-3255", "status": "unavailable", "acquisition_state": "unavailable", "records": nil, "general_guard_changed": false, "data_access_granted": false}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unknown/schema changed: %s", out.String())
	}
}

func TestOMB3255RuntimeMainArguments(t *testing.T) {
	if os.Getenv("CC_3255_RUNTIME_MAIN_CHILD") == "1" {
		os.Args = []string{"recovery", "omb-runtime-metadata", "--path"}
		main()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOMB3255RuntimeMainArguments$")
	hideCommand(cmd)
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "CC_3255_RUNTIME_MAIN_CHILD=1")
	output, err := cmd.Output()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 2 || string(output) != "{\"code\":\"omb_runtime_metadata_invalid_arguments\",\"ok\":false}\n" {
		t.Fatalf("main dispatch did not refuse: %s %v", output, err)
	}
}

func TestOMB3255RuntimeDiagnosticsAreBoundedAndDoNotGrantProof(t *testing.T) {
	ref, data := omb3255ReaderFixture()
	contextLine := strings.Split(data, "\n")[1] + "\n"
	for _, tc := range []struct {
		name, input, state string
		observed           bool
	}{
		{"positive", data, "observed", true},
		{"invalid json", data + "{private-invalid}\n", "native_json_unavailable", false},
		{"same context", data + contextLine, "observed_repeated", true},
		{"changed context", data + strings.Replace(contextLine, "gpt-6-luna", "gpt-6.1-sol", 1), "native_context_conflicting", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := &omb3255SyntheticRuntimeStream{Reader: strings.NewReader(tc.input)}
			result := omb3255InspectRuntimeState(omb3255RuntimeDeps{
				Prepare: func() ([]omb3255RuntimeRef, error) { return []omb3255RuntimeRef{ref}, nil },
				Open:    func() (omb3255RuntimeStream, error) { return stream, nil },
				Recheck: func([]omb3255RuntimeRef) bool { return true },
			})
			if result.state != tc.state || stream.closes != 1 || len(result.records) != 1 {
				t.Fatalf("diagnosis/close mismatch: state=%s close=%d", result.state, stream.closes)
			}
			if !tc.observed && result.records[0].Observation != omb3255ContextUnavailable() {
				t.Fatal("diagnostic returned partial proof")
			}
			var out bytes.Buffer
			runOMB3255RuntimeCLI(nil, &out, func() omb3255RuntimeAcquisition { return result })
			if strings.Contains(out.String(), "private") || strings.Contains(out.String(), "synthetic-native") {
				t.Fatal("diagnostic disclosed private source content or native identifiers")
			}
		})
	}
	var out bytes.Buffer
	runOMB3255RuntimeCLI(nil, &out, func() omb3255RuntimeAcquisition {
		return omb3255RuntimeAcquisition{state: "private-path-and-error"}
	})
	var decoded map[string]any
	if json.Unmarshal(out.Bytes(), &decoded) != nil || decoded["acquisition_state"] != "unavailable" || strings.Contains(out.String(), "private") {
		t.Fatal("unknown diagnostic text escaped")
	}
}

func TestOMB3255NativeMetadataFoldsOnlyConsistentContextsInOriginalWindow(t *testing.T) {
	ref, data := omb3255ReaderFixture()
	line := strings.Split(data, "\n")[1] + "\n"
	for _, second := range []string{
		line,
		strings.Replace(line, "13:31:00Z", "13:32:00Z", 1),
		strings.Replace(line, "2026-10-24T13:31:00Z", "2026-10-24T13:30:36.233Z", 1),
		strings.Replace(line, "2026-10-24T13:31:00Z", "2026-10-24T13:35:28.582Z", 1),
		strings.Replace(line, "gpt-6-luna", `gpt-\u0036-luna`, 1),
	} {
		obs, ok, state := omb3255NativeMetadataState([]byte(data+second), []omb3255RuntimeRef{ref})
		if !ok || state != "observed_repeated" || len(obs) != 1 || obs[0].Model == nil || *obs[0].Model != "gpt-6-luna" || obs[0].Effort == nil || *obs[0].Effort != "low" || obs[0].ContextAt == nil || obs[0].ContextAt.Format(time.RFC3339) != "2026-10-24T13:31:00Z" {
			t.Fatalf("same actual values were discarded: valid=%v state=%s", ok, state)
		}
	}
	for name, second := range map[string]string{
		"different model":       strings.Replace(line, "gpt-6-luna", "gpt-6.1-sol", 1),
		"different effort":      strings.Replace(line, `"effort":"low"`, `"effort":"max"`, 1),
		"effort removed":        strings.Replace(line, `,"effort":"low"`, "", 1),
		"null instead of value": strings.Replace(line, `"effort":"low"`, `"effort":null`, 1),
		"bad timestamp":         strings.Replace(line, "2026-10-24T13:31:00Z", "private-invalid-time", 1),
	} {
		t.Run(name, func(t *testing.T) {
			obs, ok, _ := omb3255NativeMetadataState([]byte(data+second), []omb3255RuntimeRef{ref})
			if ok || obs != nil {
				t.Fatal("conflicting or out-of-window context supplied proof")
			}
		})
	}
	for _, stamp := range []string{"2026-10-24T13:30:36.232Z", "2026-10-24T13:35:28.583Z"} {
		outside := strings.Replace(line, "2026-10-24T13:31:00Z", stamp, 1)
		outside = strings.Replace(outside, "gpt-6-luna", "gpt-6.1-sol", 1)
		outside = strings.Replace(outside, `"effort":"low"`, `"effort":"max"`, 1)
		obs, ok, _ := omb3255NativeMetadataState([]byte(data+outside), []omb3255RuntimeRef{ref})
		if !ok || obs[0].Model == nil || *obs[0].Model != "gpt-6-luna" || obs[0].Effort == nil || *obs[0].Effort != "low" {
			t.Fatal("outside execution overwrote the actual original observation")
		}
		onlyOutside := strings.Split(data, "\n")[0] + "\n" + outside
		obs, ok, _ = omb3255NativeMetadataState([]byte(onlyOutside), []omb3255RuntimeRef{ref})
		if !ok || obs[0] != omb3255ContextUnavailable() {
			t.Fatal("outside-only context manufactured an original observation")
		}
	}
	missing := strings.Replace(data, `,"model":"gpt-6-luna"`, "", 1)
	obs, ok, _ := omb3255NativeMetadataState([]byte(missing+strings.Split(missing, "\n")[1]+"\n"), []omb3255RuntimeRef{ref})
	if !ok || obs[0].Model != nil || obs[0].ModelState != "missing" || obs[0].Effort == nil || *obs[0].Effort != "low" {
		t.Fatal("consistent missing model became a requested model")
	}
}
