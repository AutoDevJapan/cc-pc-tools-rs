package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func replayTestModules() []programReplayModule {
	return []programReplayModule{{Path: "golang.org/x/image", Version: "v0.31.0", Sum: "h1:" + strings.Repeat("a", 43) + "=", GoModSum: "h1:" + strings.Repeat("b", 43) + "="}, {Path: "golang.org/x/text", Version: "v0.29.0", Sum: "h1:" + strings.Repeat("c", 43) + "=", GoModSum: "h1:" + strings.Repeat("d", 43) + "="}}
}

func TestProgramReplayRejectsAmbientCodeAndUnverifiedDependencies(t *testing.T) {
	for _, module := range []string{"module x\ngo 1.27\nrequire golang.org/x/image v0.31.0\nreplace golang.org/x/image => ../private\n", "module x\ngo 1.27\ntool private/tool\n", "module x\ngo 1.27\nrequire private/module v1.0.0\n"} {
		if programModule([]byte(module)) {
			t.Fatal("ambient/unapproved module accepted")
		}
	}
	modules := replayTestModules()
	if !programReplayModules(modules) {
		t.Fatal("public pinned control rejected")
	}
	modules[0].Path = "private/module"
	if programReplayModules(modules) {
		t.Fatal("private dependency accepted")
	}
	if _, err := programReplayDecodeModules([]byte(`{"Path":"golang.org/x/image","Replace":{"Dir":"../private"}}`)); err == nil {
		t.Fatal("local replacement accepted")
	}
	p := newProgramFixture(t)
	event := p.lines[p.e.Creation[0].Line-1]
	var native map[string]any
	_ = json.Unmarshal(event, &native)
	code := native["payload"].(map[string]any)["input"].(string)
	if _, err := programPatchModule(code, p.e.Program); err != nil {
		t.Fatal("exact two-file native add rejected", err)
	}
	patch, _ := programPatchFiles(code, p.e.Program)
	literal := "*** Begin Patch\n*** Add File: " + p.e.Program + "\n+" + strings.ReplaceAll(strings.TrimSuffix(string(patch["program"]), "\n"), "\n", "\n+") + "\n*** Add File: " + filepath.Join(p.e.Worktree, "go.mod") + "\n+" + strings.ReplaceAll(strings.TrimSuffix(string(patch["module"]), "\n"), "\n", "\n+") + "\n*** Add File: " + filepath.Join(p.e.Worktree, "go.work") + "\n+use ../private\n*** End Patch"
	quoted, _ := json.Marshal(literal)
	if _, err := programPatchModule("text(await tools.apply_patch("+string(quoted)+"));", p.e.Program); err == nil {
		t.Fatal("extra native file supplied code outside the drawing")
	}
}

func TestProgramReplayBuildConsumesFrozenBytesAndChecksAllOutputBytes(t *testing.T) {
	for _, changed := range []bool{false, true} {
		p := newProgramFixture(t)
		source := []byte(programTestSource)
		module := []byte("module local/test\ngo 1.27.0\nrequire golang.org/x/image v0.31.0\n")
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		var calls []string
		run := func(_ context.Context, dir, path string, args ...string) ([]byte, error) {
			calls = append(calls, filepath.Base(path)+" "+strings.Join(args, " "))
			program, err := os.ReadFile(filepath.Join(dir, filepath.Base(p.e.Program)))
			if err != nil || !bytes.Equal(program, source) {
				return nil, errConversationMedia
			}
			if path == programReplayGo {
				switch strings.Join(args, " ") {
				case "mod tidy":
					return nil, os.WriteFile(filepath.Join(dir, "go.sum"), []byte("synthetic checked sums"), 0600)
				case "mod download -json all":
					var b bytes.Buffer
					for _, m := range replayTestModules() {
						_ = json.NewEncoder(&b).Encode(m)
					}
					return b.Bytes(), nil
				case "mod verify":
					return []byte("all modules verified"), nil
				default:
					if len(args) > 0 && args[0] == "build" {
						return nil, nil
					}
					return nil, errConversationMedia
				}
			}
			if path != filepath.Join(dir, "drawing.exe") {
				return nil, errConversationMedia
			}
			for _, im := range p.e.Images {
				b, err := os.ReadFile(im.Path)
				if err != nil {
					return nil, err
				}
				if changed {
					b = append(b, 1)
				}
				if err = os.WriteFile(filepath.Join(dir, "out", filepath.Base(im.Path)), b, 0600); err != nil {
					return nil, err
				}
			}
			return nil, nil
		}
		_, _, err = programReplayBuild(context.Background(), root, p.e, source, module, run)
		if (err != nil) != changed {
			t.Fatal("replayed PNG comparison differs", changed, err)
		}
		if len(calls) != 5 {
			t.Fatal("verified dependency/build/run sequence was skipped", calls)
		}
	}
	joined := strings.Join(programReplayEnv("fixture-root"), "\n")
	for _, setting := range []string{"GOWORK=off", "GOENV=off", "GOAUTH=off", "GOTOOLCHAIN=local", "CGO_ENABLED=0", "GOPRIVATE=\n", "GONOSUMDB=\n", "GOSUMDB=sum.golang.org", "GOPROXY=https://proxy.golang.org"} {
		if !strings.Contains(joined, setting) {
			t.Fatal("replay environment did not isolate", setting)
		}
	}
}

func TestProgramReplaySignedReceiptCannotBeHandwrittenOrReused(t *testing.T) {
	p := newProgramFixture(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(root, "go.exe")
	if os.WriteFile(tool, []byte("synthetic verified Go tool"), 0600) != nil {
		t.Fatal("fixture tool")
	}
	calls := 0
	r := &programReplayRegistry{Home: filepath.Join(root, "replay-store"), GoTool: tool, Read: func(path string, limit int64) ([]byte, error) {
		if strings.HasPrefix(path, "C:/Windows/Fonts/") {
			return []byte("public font fixture"), nil
		}
		return generatedQuestionRead(path, limit)
	}, Protect: func(b []byte) ([]byte, error) { return append([]byte("sealed:"), b...), nil }, Unprotect: func(b []byte) ([]byte, error) {
		if !bytes.HasPrefix(b, []byte("sealed:")) {
			return nil, errConversationMedia
		}
		return b[7:], nil
	}, Produce: func(context.Context, programQuestionEvidence, []byte, []byte) ([]programReplayModule, string, *programReplayOwned, error) {
		calls++
		return replayTestModules(), strings.Repeat("a", 64), nil, nil
	}}
	source := []byte(programTestSource)
	module := []byte("module local/test\ngo 1.27.0\nrequire golang.org/x/image v0.31.0\n")
	if r.check(context.Background(), p.e, source, module) == nil {
		t.Fatal("unregistered caller-written evidence qualified")
	}
	if err = r.register(context.Background(), p.e, source, module); err != nil {
		t.Fatal(err)
	}
	if err = r.check(context.Background(), p.e, source, module); err != nil || calls != 1 {
		t.Fatal("exact signed replay not reusable", err, calls)
	}
	other := p.e
	other.Scope.Todo++
	if r.check(context.Background(), other, source, module) == nil {
		t.Fatal("signed replay crossed ToDo scope")
	}
	input, err := r.input(p.e, source, module)
	if err != nil {
		t.Fatal(err)
	}
	var envelope programReplayEnvelope
	b, _ := os.ReadFile(r.receiptPath(input))
	_ = json.Unmarshal(b, &envelope)
	envelope.Payload = bytes.Replace(envelope.Payload, []byte(programReplayDomain), []byte("tampered-replay-domain"), 1)
	b, _ = json.Marshal(envelope)
	_ = os.WriteFile(r.receiptPath(input), b, 0600)
	if r.check(context.Background(), p.e, source, module) == nil {
		t.Fatal("rewritten signed replay qualified")
	}
}
