package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const programTestSource = `package main
import ("os"; "fmt"; "image"; "image/png"; "path/filepath"; "golang.org/x/image/font"; "golang.org/x/image/font/opentype")
func must(err error) {if err!=nil{panic(err)}}
func typeface(path string,size float64) font.Face {
 b,e:=os.ReadFile(path);must(e); f,e:=opentype.Parse(b);must(e)
 face,e:=opentype.NewFace(f,&opentype.FaceOptions{Size:size,DPI:72,Hinting:font.HintingFull});must(e);return face
}
func main(){_ = typeface("C:/Windows/Fonts/arial.ttf",24)
for i:=range []bool{false,true}{img:=image.NewRGBA(image.Rect(0,0,2,2))
file:=filepath.Join(os.Args[1],fmt.Sprintf("drawing-%02d.png",i+1))
f,e:=os.OpenFile(file,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);must(e);must(png.Encode(f,img));must(f.Close());fmt.Println(file)
}}
`

type programFixture struct {
	f        *progressFixture
	e        programQuestionEvidence
	d        programQuestionDeps
	manifest string
	lines    [][]byte
}

func newProgramFixture(t *testing.T) *programFixture {
	t.Helper()
	f := progressTestFixture(t)
	f.source.Content = "An original owner illustration request."
	configureStateRequestForTest(f, 2639)
	f.r.Config.Project = "fixture-project"
	root := t.TempDir()
	work := filepath.Join(root, "worktree")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	session := "11111111-2222-3333-4444-555555555555"
	e := programQuestionEvidence{Domain: programEvidenceDomain, Request: "test-owner-image", Origin: "501", Scope: generatedQuestionScope{Todo: 2639, Owner: "300", Guild: "100", Forum: "200"}, Job: "20261025-010203-todo-2639", Worktree: work, Session: session, Rollout: filepath.Join(root, "rollout-2026-10-25-"+session+".jsonl"), SourceMessage: f.source.ID, SourceVersion: ordinaryVersion(f.r.State.Requests[f.source.ID]), Program: filepath.Join(work, "drawing.go"), Body: filepath.Join(work, "question.txt"), Decision: filepath.Join(work, "decision.json")}
	f.rows[0]["status"] = "doing"
	delete(f.rows[0], "wait_on")
	f.rows[0]["note"] = "intake-owner-source:" + e.SourceMessage + ":" + e.SourceVersion
	f.rows[0]["jobs"] = []string{e.Job}
	f.rows[0]["scheduler"] = map[string]any{"job": e.Job, "worktree": work}
	f.saveRows()
	job := map[string]any{"id": e.Job, "todo_id": e.Scope.Todo, "cwd": work, "thread_id": session}
	for _, dir := range []string{filepath.Join(f.r.JobsPath, e.Job), filepath.Join(f.r.Config.Home, "requests", "versions")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	conversationWriteJSON(t, filepath.Join(f.r.JobsPath, e.Job, "job.json"), job)
	q := f.r.State.Requests[f.source.ID]
	conversationWriteJSON(t, filepath.Join(f.r.Config.Home, "requests", "versions", q.Message+"-"+e.SourceVersion+".json"), q)
	body := []byte("Choose the drawing: a or b.\r\n")
	decision := map[string]any{"todo_id": e.Scope.Todo, "decision": "direction", "scope": map[string]any{"resource": "synthetic-drawing"}}
	if err := os.WriteFile(e.Program, []byte(programTestSource), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.Body, body, 0600); err != nil {
		t.Fatal(err)
	}
	conversationWriteJSON(t, e.Decision, decision)
	e.ProgramSHA = bytesHash([]byte(programTestSource))
	e.Scope.Body = bytesHash(body)
	e.Scope.Decision = canonicalHash(decision)
	for _, name := range []string{"drawing-01.png", "drawing-02.png"} {
		p := filepath.Join(work, name)
		data := []byte("\x89PNG\r\n\x1a\nsynthetic-" + name)
		if err := os.WriteFile(p, data, 0600); err != nil {
			t.Fatal(err)
		}
		e.Images = append(e.Images, programQuestionImage{Path: p, SHA: bytesHash(data), Size: int64(len(data))})
	}
	p := &programFixture{f: f, e: e, manifest: filepath.Join(work, "evidence.json")}
	marshal := func(v any) []byte {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	p.lines = append(p.lines, marshal(map[string]any{"type": "session_meta", "payload": map[string]any{"id": session, "cwd": work}}))
	p.e.SessionSHA = bytesHash(p.lines[0])
	pair := func(code string, blocks any) []generatedEventBound {
		call := "call-" + string(rune('a'+len(p.lines)))
		var result []generatedEventBound
		for i, payload := range []map[string]any{{"type": "custom_tool_call", "name": "exec", "call_id": call, "input": code}, {"type": "custom_tool_call_output", "call_id": call, "output": blocks}} {
			b := marshal(map[string]any{"timestamp": f.now.Add(-time.Minute + time.Duration(len(p.lines))*time.Second), "type": "response_item", "payload": payload})
			p.lines = append(p.lines, b)
			result = append(result, generatedEventBound{Line: len(p.lines), SHA: bytesHash(b), Kind: []string{"call", "result"}[i], Call: call})
		}
		return result
	}
	patch := "*** Begin Patch\n*** Add File: " + e.Program + "\n+" + strings.ReplaceAll(strings.TrimSuffix(programTestSource, "\n"), "\n", "\n+") + "\n*** End Patch"
	patchJSON, _ := json.Marshal(patch)
	p.e.Creation = pair("text(await tools.apply_patch("+string(patchJSON)+"));", []map[string]any{{"type": "input_text", "text": "Script completed\n"}, {"type": "input_text", "text": "{}"}})
	runArgs, _ := json.Marshal(map[string]any{"cmd": "go mod tidy\ngo run drawing.go .", "workdir": work, "yield_time_ms": 10000, "max_output_tokens": 1500})
	runResult, _ := json.Marshal(map[string]any{"exit_code": 0, "output": "drawing-01.png\ndrawing-02.png\n"})
	p.e.Execution = pair("const r=await tools.exec_command("+string(runArgs)+");text(r);", []map[string]any{{"type": "input_text", "text": "Script completed\n"}, {"type": "input_text", "text": string(runResult)}})
	blocks := []map[string]any{{"type": "input_text", "text": "Script completed\n"}}
	for _, im := range e.Images {
		b, _ := os.ReadFile(im.Path)
		blocks = append(blocks, map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)})
	}
	p.e.Preview = pair(programPreviewCode(e.Images), blocks)
	if err := os.WriteFile(e.Rollout, bytesJoinLines(p.lines), 0600); err != nil {
		t.Fatal(err)
	}
	p.d = programQuestionDeps{Sessions: root, Read: generatedQuestionRead, Guard: func(context.Context, string) (bool, error) { return true, nil }, Clear: func(string) bool { return true }, Now: f.r.Now, Bind: func(ctx context.Context, e programQuestionEvidence) (bool, error) {
		return programQuestionBinding(ctx, f.r, f.r.JobsPath, e)
	}}
	p.save(t)
	return p
}

func bytesJoinLines(lines [][]byte) []byte {
	var result []byte
	for _, line := range lines {
		result = append(result, line...)
		result = append(result, '\n')
	}
	return result
}
func (p *programFixture) save(t *testing.T) { t.Helper(); conversationWriteJSON(t, p.manifest, p.e) }

func TestProgramQuestionExactNativeDrawingAndTaskQualification(t *testing.T) {
	p := newProgramFixture(t)
	before, _ := os.ReadFile(p.f.r.TodoPath)
	proof, err := checkProgramQuestion(context.Background(), p.manifest, p.d)
	if err != nil || !proof.Validated || !proof.SendAllowed || len(proof.Images) != 2 {
		t.Fatalf("exact native drawing not qualified: %#v", err)
	}
	after, _ := os.ReadFile(p.f.r.TodoPath)
	if string(before) != string(after) || p.f.sends != 0 {
		t.Fatal("read-only qualification mutated/sent")
	}
}

func TestProgramQuestionRejectsChangedScopeSourceAndEvidence(t *testing.T) {
	for _, change := range []string{"image", "program", "copy", "job", "todo", "body", "decision", "owner", "guild", "forum", "origin", "unknown-source", "source-version", "source-marker", "native", "preview", "run", "expired", "guard", "protection", "done", "stopped", "owner-wait"} {
		t.Run(change, func(t *testing.T) {
			p := newProgramFixture(t)
			// The successful control exercises the same dependency before mutation.
			if _, err := checkProgramQuestion(context.Background(), p.manifest, p.d); err != nil {
				t.Fatalf("positive control: %#v", err)
			}
			switch change {
			case "image":
				_ = os.WriteFile(p.e.Images[0].Path, []byte("\x89PNG\r\n\x1a\nchanged"), 0600)
			case "program":
				_ = os.WriteFile(p.e.Program, []byte(programTestSource+"// changed\n"), 0600)
			case "copy":
				b, _ := os.ReadFile(p.e.Images[0].Path)
				p.e.Images[0].Path = filepath.Join(p.e.Worktree, "copy.png")
				_ = os.WriteFile(p.e.Images[0].Path, b, 0600)
			case "job":
				p.e.Job = "20261025-010204-todo-2639"
			case "todo":
				p.e.Scope.Todo++
			case "body":
				p.e.Scope.Body = strings.Repeat("a", 64)
			case "decision":
				p.e.Scope.Decision = strings.Repeat("a", 64)
			case "owner":
				p.e.Scope.Owner = "999"
			case "guild":
				p.e.Scope.Guild = "999"
			case "forum":
				p.e.Scope.Forum = "999"
			case "origin":
				p.e.Origin = "999"
			case "unknown-source":
				p.e.SourceMessage = "999"
			case "source-version":
				p.e.SourceVersion = strings.Repeat("a", 64)
			case "source-marker":
				p.f.rows[0]["note"] = "unverified"
				p.f.saveRows()
			case "native":
				p.e.Creation[0].SHA = strings.Repeat("a", 64)
			case "preview":
				p.e.Preview[1].SHA = strings.Repeat("a", 64)
			case "run":
				p.e.Execution = p.e.Creation
			case "expired":
				p.d.Now = func() time.Time { return p.f.now.Add(8 * 24 * time.Hour) }
			case "guard":
				p.d.Guard = func(context.Context, string) (bool, error) { return false, nil }
			case "protection":
				p.d.Clear = func(string) bool { return false }
			case "done":
				p.f.rows[0]["status"] = "done"
				p.f.saveRows()
			case "stopped":
				p.f.rows[0]["stopped"] = true
				p.f.saveRows()
			case "owner-wait":
				p.f.rows[0]["status"] = "block"
				p.f.saveRows()
			}
			p.save(t)
			if _, err := checkProgramQuestion(context.Background(), p.manifest, p.d); err == nil {
				t.Fatal("changed proof qualified")
			}
			if p.f.sends != 0 {
				t.Fatal("rejection sent")
			}
		})
	}
}

func TestProgramQuestionDependencyWaitPreservesReadQualificationAndForbidsSend(t *testing.T) {
	p := newProgramFixture(t)
	p.f.rows[0]["status"] = "wait"
	p.f.rows[0]["wait_on"] = []uint64{42}
	p.f.saveRows()
	before, _ := os.ReadFile(p.f.r.TodoPath)
	proof, err := checkProgramQuestion(context.Background(), p.manifest, p.d)
	if err != nil || !proof.Validated || proof.SendAllowed {
		t.Fatalf("dependency boundary lost: %#v", err)
	}
	after, _ := os.ReadFile(p.f.r.TodoPath)
	if string(before) != string(after) {
		t.Fatal("wait/owner changed")
	}
}

func TestProgramQuestionDrawingRejectsExternalInputsAndUnknownSyntax(t *testing.T) {
	for _, source := range []string{strings.Replace(programTestSource, "\"os\"", "\"net/http\"", 1), strings.Replace(programTestSource, "C:/Windows/Fonts/arial.ttf", "C:/private/image.png", 1), strings.Replace(programTestSource, "os.ReadFile(path)", "os.ReadFile(\"C:/private/file\")", 1), strings.Replace(programTestSource, "os.ReadFile(path)", "os.Getenv(\"VALUE\")", 1)} {
		if programDrawingSource([]byte(source)) == nil {
			t.Fatal("external input accepted")
		}
	}
}

func TestProgramQuestionRequiresExclusivePNGCreationNotPrintedOldNames(t *testing.T) {
	if _, err := programDrawingOutputs([]byte(programTestSource), 2); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{
		strings.Replace(programTestSource, "must(png.Encode(f,img))", "fmt.Println(\"old image\")", 1),
		strings.Replace(programTestSource, "os.O_CREATE|os.O_EXCL|os.O_WRONLY", "os.O_RDONLY", 1),
		strings.Replace(programTestSource, "if err!=nil{panic(err)}", "", 1),
	} {
		if _, err := programDrawingOutputs([]byte(source), 2); err == nil {
			t.Fatal("printed/copied/pre-existing output gained qualification")
		}
	}
}
