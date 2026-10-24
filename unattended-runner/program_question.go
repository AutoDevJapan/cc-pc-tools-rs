package main

// A separate, read-only purpose for newly drawn Go illustrations. Native
// execution and preview records are the authority, never a caller's approval
// statement or an imagegen-shaped summary. No program is executed here.
import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const programQuestionDomain = "owner-program-question-image-v1"
const programEvidenceDomain = "owner-program-question-evidence-v1"

type programQuestionEvidence struct {
	created       time.Time
	Domain        string                 `json:"domain"`
	Request       string                 `json:"request_id"`
	Origin        string                 `json:"origin_thread_id"`
	Scope         generatedQuestionScope `json:"scope"`
	Job           string                 `json:"job_id"`
	Worktree      string                 `json:"worktree"`
	Session       string                 `json:"session"`
	Rollout       string                 `json:"rollout_path"`
	SessionSHA    string                 `json:"session_record_sha256"`
	SourceMessage string                 `json:"source_message_id"`
	SourceVersion string                 `json:"source_version_sha256"`
	Program       string                 `json:"program_path"`
	ProgramSHA    string                 `json:"program_sha256"`
	Body          string                 `json:"body_path"`
	Decision      string                 `json:"decision_path"`
	Images        []programQuestionImage `json:"images"`
	Creation      []generatedEventBound  `json:"creation_events"`
	Execution     []generatedEventBound  `json:"execution_events"`
	Preview       []generatedEventBound  `json:"preview_events"`
}
type programQuestionImage struct {
	Path string `json:"path"`
	SHA  string `json:"sha256"`
	Size int64  `json:"size"`
}
type programQuestionProof struct {
	Domain      string                 `json:"domain"`
	Validated   bool                   `json:"validated"`
	SendAllowed bool                   `json:"send_allowed"`
	Request     string                 `json:"request_id"`
	Origin      string                 `json:"origin_thread_id"`
	Scope       generatedQuestionScope `json:"scope"`
	Images      []programQuestionImage `json:"images"`
}
type programQuestionDeps struct {
	Sessions string
	Read     func(string, int64) ([]byte, error)
	Guard    func(context.Context, string) (bool, error)
	Bind     func(context.Context, programQuestionEvidence) (bool, error)
	Clear    func(string) bool
	Now      func() time.Time
	Replay   func(context.Context, programQuestionEvidence, []byte, []byte) error
	Budget   time.Duration
}
type programNative struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Payload   struct {
		Type, Name, Input string
		Call              string          `json:"call_id"`
		Raw               json.RawMessage `json:"output"`
	} `json:"payload"`
}

func programPair(e programQuestionEvidence, bound []generatedEventBound, now time.Time) ([]programNative, error) {
	if len(bound) != 2 || bound[0].Line >= bound[1].Line {
		return nil, errConversationMedia
	}
	raw, err := generatedQuestionRecords(e.Rollout, bound)
	if err != nil {
		return nil, errConversationMedia
	}
	pair := make([]programNative, 2)
	for i := range pair {
		if json.Unmarshal(raw[i], &pair[i]) != nil || pair[i].Type != "response_item" || pair[i].Timestamp.IsZero() || pair[i].Timestamp.After(now) {
			return nil, errConversationMedia
		}
	}
	if pair[0].Payload.Type != "custom_tool_call" || pair[0].Payload.Name != "exec" || pair[1].Payload.Type != "custom_tool_call_output" || pair[0].Payload.Call == "" || pair[0].Payload.Call != pair[1].Payload.Call || pair[1].Timestamp.Before(pair[0].Timestamp) {
		return nil, errConversationMedia
	}
	return pair, nil
}

func programPatchFiles(code, path string) (map[string][]byte, error) {
	tokens, err := generatedJSTokens(code)
	if err != nil {
		return nil, errConversationMedia
	}
	p := generatedJSParser{tokens: tokens}
	if !p.sequence("text", "(", "await", "tools", ".", "apply_patch", "(") {
		return nil, errConversationMedia
	}
	patch, ok := p.literal()
	if !ok || !p.sequence(")", ")", ";") || p.at != len(tokens) {
		return nil, errConversationMedia
	}
	lines := strings.Split(patch, "\n")
	if len(lines) < 4 || lines[0] != "*** Begin Patch" || lines[len(lines)-1] != "*** End Patch" {
		return nil, errConversationMedia
	}
	files := map[string][]byte{}
	module := filepath.Join(filepath.Dir(filepath.FromSlash(path)), "go.mod")
	current := ""
	for _, line := range lines[1 : len(lines)-1] {
		if strings.HasPrefix(line, "*** Add File: ") {
			name := filepath.FromSlash(strings.TrimPrefix(line, "*** Add File: "))
			switch {
			case conversationMediaSamePath(name, filepath.FromSlash(path)):
				current = "program"
			case conversationMediaSamePath(name, module):
				current = "module"
			default:
				return nil, errConversationMedia
			}
			if _, exists := files[current]; exists {
				return nil, errConversationMedia
			}
			files[current] = nil
			continue
		}
		if !strings.HasPrefix(line, "+") {
			return nil, errConversationMedia
		}
		if current == "" {
			return nil, errConversationMedia
		}
		files[current] = append(files[current], []byte(line[1:]+"\n")...)
	}
	if len(files) != 2 || len(files["program"]) == 0 || len(files["module"]) == 0 {
		return nil, errConversationMedia
	}
	return files, nil
}
func programPatchSource(code, path string) ([]byte, error) {
	files, err := programPatchFiles(code, path)
	if err != nil {
		return nil, err
	}
	return files["program"], nil
}
func programPatchModule(code, path string) ([]byte, error) {
	files, err := programPatchFiles(code, path)
	if err != nil {
		return nil, err
	}
	if !programModule(files["module"]) {
		return nil, errConversationMedia
	}
	return files["module"], nil
}

// A deliberately small pinned public dependency contract. Local replacements,
// workspace files, tools and additional packages are not replay inputs.
func programModule(b []byte) bool {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	module, version, require := 0, 0, 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case regexp.MustCompile(`^module [A-Za-z0-9._/-]+$`).MatchString(line):
			module++
		case regexp.MustCompile(`^go 1\.[0-9]+(?:\.[0-9]+)?$`).MatchString(line):
			version++
		case regexp.MustCompile(`^require golang\.org/x/image v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(line):
			require++
		default:
			return false
		}
	}
	return module == 1 && version == 1 && require == 1
}

// Restricted protocol: a single command result, printed directly. No dynamic
// commands, injected environment, chained JS, hidden loads or shell expansion.
func programRunInput(code string) (string, string, error) {
	code = strings.TrimSpace(code)
	prefix, suffix := "const r=await tools.exec_command(", ");text(r);"
	if !strings.HasPrefix(code, prefix) || !strings.HasSuffix(code, suffix) {
		return "", "", errConversationMedia
	}
	obj := strings.TrimSuffix(strings.TrimPrefix(code, prefix), suffix)
	if strings.HasPrefix(obj, "{cmd:") {
		obj = "{\"cmd\":" + strings.TrimPrefix(obj, "{cmd:")
	}
	var args struct {
		Cmd     string `json:"cmd"`
		Workdir string `json:"workdir"`
		Yield   int    `json:"yield_time_ms"`
		Max     int    `json:"max_output_tokens"`
	}
	if historyStrictJSON([]byte(obj), &args) != nil {
		return "", "", errConversationMedia
	}
	return args.Cmd, args.Workdir, nil
}

func programRunResult(raw json.RawMessage) (string, error) {
	var blocks []struct{ Type, Text string }
	if json.Unmarshal(raw, &blocks) != nil || len(blocks) != 2 || blocks[0].Type != "input_text" || !strings.HasPrefix(blocks[0].Text, "Script completed\n") || blocks[1].Type != "input_text" {
		return "", errConversationMedia
	}
	var result struct {
		Code    *int   `json:"exit_code"`
		Output  string `json:"output"`
		Session any    `json:"session_id"`
	}
	if json.Unmarshal([]byte(blocks[1].Text), &result) != nil || result.Code == nil || *result.Code != 0 || result.Session != nil {
		return "", errConversationMedia
	}
	return result.Output, nil
}

func programPreviewCode(paths []programQuestionImage) string {
	var calls []string
	for _, image := range paths {
		s, _ := json.Marshal(image.Path)
		calls = append(calls, "tools.view_image({path:"+string(s)+"})")
	}
	return "for(const r of await Promise.allSettled([" + strings.Join(calls, ",") + "])){if(r.status===\"fulfilled\")image(r.value.image_url);else text(String(r.reason));}"
}

func checkProgramQuestion(ctx context.Context, path string, d programQuestionDeps) (programQuestionProof, error) {
	stage := "evidence_guard"
	bad := func() (programQuestionProof, error) {
		return programQuestionProof{}, generatedFailure(stage, "program_evidence_not_qualified")
	}
	allowed, err := d.Guard(ctx, path)
	if err != nil || !allowed {
		return bad()
	}
	stage = "evidence_schema"
	b, err := d.Read(path, 1<<20)
	var e programQuestionEvidence
	if err != nil || historyStrictJSON(b, &e) != nil || e.Domain != programEvidenceDomain || !conversationID.MatchString(e.Request) || e.Scope.Todo == 0 || !validJob(e.Job) || !generatedUUID.MatchString(e.Session) || !shaPattern.MatchString(e.SessionSHA) || !shaPattern.MatchString(e.ProgramSHA) || !shaPattern.MatchString(e.SourceVersion) || len(e.Images) == 0 || len(e.Images) > 10 {
		return bad()
	}
	for _, id := range []string{e.Origin, e.SourceMessage, e.Scope.Owner, e.Scope.Guild, e.Scope.Forum} {
		if _, ok := number(id); !ok {
			return bad()
		}
	}
	if !shaPattern.MatchString(e.Scope.Body) || !shaPattern.MatchString(e.Scope.Decision) || !generatedWithin(d.Sessions, e.Rollout) || !strings.HasSuffix(filepath.Base(filepath.FromSlash(e.Rollout)), "-"+e.Session+".jsonl") {
		return bad()
	}
	stage = "source_paths"
	for _, p := range []string{e.Worktree, e.Program, e.Body, e.Decision} {
		if !d.Clear(p) {
			return bad()
		}
		ok, err := d.Guard(ctx, p)
		if err != nil || !ok {
			return bad()
		}
	}
	if !generatedWithin(e.Worktree, e.Program) || !generatedWithin(e.Worktree, e.Body) || !generatedWithin(e.Worktree, e.Decision) {
		return bad()
	}
	stage = "native_session"
	meta, err := generatedQuestionRecords(e.Rollout, []generatedEventBound{{Line: 1, SHA: e.SessionSHA}})
	var session struct {
		Type    string
		Payload struct{ ID, Cwd string }
	}
	if err != nil || json.Unmarshal(meta[0], &session) != nil || session.Type != "session_meta" || session.Payload.ID != e.Session || !conversationMediaSamePath(filepath.FromSlash(session.Payload.Cwd), filepath.FromSlash(e.Worktree)) {
		return bad()
	}
	stage = "drawing_source"
	program, err := d.Read(e.Program, 128<<10)
	if err != nil || bytesHash(program) != e.ProgramSHA || programDrawingSource(program) != nil {
		return bad()
	}
	names, err := programDrawingOutputs(program, len(e.Images))
	if err != nil {
		return bad()
	}
	stage = "native_creation"
	creation, err := programPair(e, e.Creation, d.Now())
	if err != nil {
		return bad()
	}
	original, err := programPatchSource(creation[0].Payload.Input, e.Program)
	if err != nil || !bytes.Equal(program, original) {
		return bad()
	}
	e.created = creation[0].Timestamp
	// apply_patch's native completed output is required, not just its call.
	var patchResult []struct{ Type, Text string }
	if json.Unmarshal(creation[1].Payload.Raw, &patchResult) != nil || len(patchResult) != 2 || patchResult[1].Type != "input_text" || patchResult[1].Text != "{}" {
		return bad()
	}
	stage = "native_execution"
	run, err := programPair(e, e.Execution, d.Now())
	if err != nil {
		return bad()
	}
	cmd, dir, err := programRunInput(run[0].Payload.Input)
	want := "go run " + filepath.Base(e.Program) + " ."
	if err != nil || !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*\.go$`).MatchString(filepath.Base(e.Program)) || !conversationMediaSamePath(filepath.FromSlash(dir), filepath.Dir(filepath.FromSlash(e.Program))) || (cmd != want && cmd != "go mod tidy\n"+want) || run[0].Timestamp.Before(creation[1].Timestamp) || !d.Now().Before(run[0].Timestamp.Add(7*24*time.Hour)) {
		return bad()
	}
	output, err := programRunResult(run[1].Payload.Raw)
	if err != nil {
		return bad()
	}
	stage = "original_images"
	var outputNames []string
	seen := map[string]bool{}
	var total int64
	for i, image := range e.Images {
		p := filepath.FromSlash(image.Path)
		key := strings.ToLower(p)
		if !shaPattern.MatchString(image.SHA) || image.Size <= 0 || image.Size > conversationMediaLimit || seen[key] || !conversationMediaSamePath(filepath.Dir(p), filepath.Dir(filepath.FromSlash(e.Program))) || filepath.Base(p) != names[i] || filepath.Ext(p) != ".png" || !d.Clear(p) {
			return bad()
		}
		ok, err := d.Guard(ctx, p)
		if err != nil || !ok {
			return bad()
		}
		data, err := d.Read(p, conversationMediaLimit)
		if err != nil || int64(len(data)) != image.Size || bytesHash(data) != image.SHA || !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
			return bad()
		}
		seen[key] = true
		total += image.Size
		outputNames = append(outputNames, filepath.Base(p))
	}
	if total > 23<<20 || strings.ReplaceAll(output, "\r\n", "\n") != strings.Join(outputNames, "\n")+"\n" {
		return bad()
	}
	stage = "native_preview"
	preview, err := programPair(e, e.Preview, d.Now())
	if err != nil || preview[0].Timestamp.Before(run[1].Timestamp) {
		return bad()
	}
	actualTokens, aerr := generatedJSTokens(preview[0].Payload.Input)
	wantTokens, werr := generatedJSTokens(programPreviewCode(e.Images))
	if aerr != nil || werr != nil || !reflect.DeepEqual(actualTokens, wantTokens) {
		return bad()
	}
	var blocks []struct {
		Type string `json:"type"`
		URL  string `json:"image_url"`
		Text string `json:"text"`
	}
	if json.Unmarshal(preview[1].Payload.Raw, &blocks) != nil {
		return bad()
	}
	index := 0
	for _, block := range blocks {
		if block.Type == "input_text" && strings.HasPrefix(block.Text, "Script completed\n") {
			continue
		}
		if block.Type != "input_image" || index >= len(e.Images) || !strings.HasPrefix(block.URL, "data:image/png;base64,") {
			return bad()
		}
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(block.URL, "data:image/png;base64,"))
		if err != nil || bytesHash(data) != e.Images[index].SHA || int64(len(data)) != e.Images[index].Size {
			return bad()
		}
		index++
	}
	if index != len(e.Images) {
		return bad()
	}
	stage = "question_scope"
	body, err := d.Read(e.Body, 128<<10)
	if err != nil || bytesHash(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))) != e.Scope.Body {
		return bad()
	}
	decision, err := d.Read(e.Decision, 128<<10)
	if err != nil {
		return bad()
	}
	var v map[string]any
	if historyStrictJSON(bytes.TrimPrefix(decision, []byte("\xef\xbb\xbf")), &v) != nil || canonicalHash(v) != e.Scope.Decision {
		return bad()
	}
	if n, ok := v["todo_id"].(float64); !ok || n != float64(e.Scope.Todo) {
		return bad()
	}
	stage = "current_task_and_owner_source"
	send, err := d.Bind(ctx, e)
	if err != nil || ctx.Err() != nil {
		return bad()
	}
	stage = "closed_drawing_replay"
	module, err := programPatchModule(creation[0].Payload.Input, e.Program)
	if err != nil || d.Replay == nil || d.Replay(ctx, e, program, module) != nil {
		return bad()
	}
	return programQuestionProof{Domain: programQuestionDomain, Validated: true, SendAllowed: send, Request: e.Request, Origin: e.Origin, Scope: e.Scope, Images: e.Images}, nil
}

// Only drawing/font packages; no imports capable of reading source images or
// making requests. The only OS read is a font-loader called with font literals.
func programDrawingSource(source []byte) error {
	f, err := parser.ParseFile(token.NewFileSet(), "drawing.go", source, parser.ParseComments)
	if err != nil || f.Name.Name != "main" {
		return errConversationMedia
	}
	allowed := map[string]bool{"fmt": true, "image": true, "image/color": true, "image/draw": true, "image/png": true, "math": true, "os": true, "path/filepath": true, "golang.org/x/image/font": true, "golang.org/x/image/font/opentype": true, "golang.org/x/image/math/fixed": true, "golang.org/x/image/vector": true}
	for _, im := range f.Imports {
		name, err := strconv.Unquote(im.Path.Value)
		if err != nil || !allowed[name] || im.Name != nil {
			return errConversationMedia
		}
	}
	for _, g := range f.Comments {
		if strings.Contains(g.Text(), "go:") {
			return errConversationMedia
		}
	}
	// Restrict the font reader itself, including parameter mutation and aliases.
	fontTemplate, _ := parser.ParseFile(token.NewFileSet(), "font.go", `package main
func typeface(path string,size float64) font.Face {
b,e:=os.ReadFile(path);must(e); f,e:=opentype.Parse(b);must(e)
face,e:=opentype.NewFace(f,&opentype.FaceOptions{Size:size,DPI:72,Hinting:font.HintingFull});must(e);return face
}`, 0)
	formatted := func(n ast.Node) string {
		var b bytes.Buffer
		_ = format.Node(&b, token.NewFileSet(), n)
		return b.String()
	}
	fontFunction := formatted(fontTemplate.Decls[0])
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	ok := true
	fontCalls := 0
	ast.Inspect(f, func(node ast.Node) bool {
		if id, yes := node.(*ast.Ident); yes && id.Obj != nil && allowed[id.Name] {
			ok = false
		}
		if id, yes := node.(*ast.Ident); yes && id.Name == "typeface" {
			switch parent := parents[node].(type) {
			case *ast.FuncDecl:
				if parent.Name != id {
					ok = false
				}
			case *ast.CallExpr:
				if parent.Fun != id {
					ok = false
				}
			default:
				ok = false
			}
		}
		s, yes := node.(*ast.SelectorExpr)
		if !yes {
			return true
		}
		id, yes := s.X.(*ast.Ident)
		if !yes {
			return true
		}
		if id.Name == "os" {
			switch s.Sel.Name {
			case "ReadFile", "OpenFile", "Args", "O_CREATE", "O_EXCL", "O_WRONLY":
			default:
				ok = false
			}
			if s.Sel.Name == "ReadFile" || s.Sel.Name == "OpenFile" {
				call, yes := parents[node].(*ast.CallExpr)
				if !yes || call.Fun != s {
					ok = false
					return true
				}
				if s.Sel.Name == "OpenFile" {
					if len(call.Args) != 3 || formatted(call.Args[1]) != "os.O_CREATE | os.O_EXCL | os.O_WRONLY" {
						ok = false
					}
				}
			}
		}
		if (id.Name == "png" || id.Name == "image") && strings.HasPrefix(s.Sel.Name, "Decode") {
			ok = false
		}
		if s.Sel.Name == "Write" || s.Sel.Name == "WriteAt" || s.Sel.Name == "Read" || s.Sel.Name == "ReadAt" {
			ok = false
		}
		return true
	})
	for _, decl := range f.Decls {
		fn, yes := decl.(*ast.FuncDecl)
		if !yes {
			continue
		}
		if fn.Name.Name == "typeface" && formatted(fn) != fontFunction {
			return errConversationMedia
		}
		ast.Inspect(fn, func(node ast.Node) bool {
			call, yes := node.(*ast.CallExpr)
			if !yes {
				return true
			}
			if id, yes := call.Fun.(*ast.Ident); yes && id.Name == "typeface" {
				fontCalls++
				if len(call.Args) != 2 {
					ok = false
					return true
				}
				lit, yes := call.Args[0].(*ast.BasicLit)
				if !yes {
					ok = false
					return true
				}
				p, err := strconv.Unquote(lit.Value)
				if err != nil || !regexp.MustCompile(`^C:/Windows/Fonts/[A-Za-z0-9_-]+\.ttf$`).MatchString(p) {
					ok = false
				}
			}
			if sel, yes := call.Fun.(*ast.SelectorExpr); yes {
				if id, yes := sel.X.(*ast.Ident); yes && id.Name == "os" && sel.Sel.Name == "ReadFile" {
					if fn.Name.Name != "typeface" || len(call.Args) != 1 {
						ok = false
						return true
					}
					arg, yes := call.Args[0].(*ast.Ident)
					if !yes || arg.Name != "path" {
						ok = false
					}
				}
			}
			return true
		})
	}
	if !ok || fontCalls == 0 {
		return errConversationMedia
	}
	return nil
}

// The one visible filename per loop iteration is printed only after an
// exclusive new file was PNG-encoded and closed successfully. Merely printing
// old filenames or viewing a copied image cannot establish new production.
func programDrawingOutputs(source []byte, count int) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "drawing.go", source, 0)
	if err != nil {
		return nil, errConversationMedia
	}
	formatNode := func(n ast.Node) string {
		var b bytes.Buffer
		_ = format.Node(&b, token.NewFileSet(), n)
		return b.String()
	}
	writes, prints, encodes := 0, 0, 0
	var pattern string
	var body *ast.BlockStmt
	mustFound := false
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name == "must" {
			mustFound = formatNode(fn) == "func must(err error) {\n\tif err != nil {\n\t\tpanic(err)\n\t}\n}"
		}
		if fn.Name.Name == "main" {
			for _, stmt := range fn.Body.List {
				if loop, ok := stmt.(*ast.RangeStmt); ok {
					if body != nil {
						return nil, errConversationMedia
					}
					body = loop.Body
				}
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		s, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := s.X.(*ast.Ident)
		if !ok {
			return true
		}
		switch id.Name + "." + s.Sel.Name {
		case "os.OpenFile":
			writes++
		case "fmt.Println":
			prints++
		case "png.Encode":
			encodes++
		case "fmt.Sprintf":
			if len(c.Args) == 2 {
				if lit, ok := c.Args[0].(*ast.BasicLit); ok {
					v, e := strconv.Unquote(lit.Value)
					if e == nil && regexp.MustCompile(`^[A-Za-z0-9_-]+-%02d\.png$`).MatchString(v) {
						pattern = v
					}
				}
			}
		}
		return true
	})
	if !mustFound || body == nil || len(body.List) < 6 || writes != 1 || prints != 1 || encodes != 1 || pattern == "" || count < 1 {
		return nil, errConversationMedia
	}
	tail := &ast.BlockStmt{List: body.List[len(body.List)-6:]}
	template, err := parser.ParseFile(token.NewFileSet(), "writer.go", `package main
func main(){file:=filepath.Join(os.Args[1],fmt.Sprintf(`+strconv.Quote(pattern)+`,i+1))
f,e:=os.OpenFile(file,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);must(e);must(png.Encode(f,img));must(f.Close());fmt.Println(file)}`, 0)
	if err != nil || formatNode(tail) != formatNode(template.Decls[0].(*ast.FuncDecl).Body) {
		return nil, errConversationMedia
	}
	var names []string
	for i := 1; i <= count; i++ {
		names = append(names, fmt.Sprintf(pattern, i))
	}
	return names, nil
}

func programQuestionCLI(args []string, out io.Writer, d programQuestionDeps) int {
	f := flag.NewFlagSet("check-program-question-evidence", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var path string
	var diagnostic bool
	conversationStringFlag(f, "evidence", &path)
	f.BoolVar(&diagnostic, "diagnostic", false, "report only the failed stage")
	fail := func() int { _, _ = io.WriteString(out, "program question evidence not qualified\n"); return 1 }
	if f.Parse(args) != nil || f.NArg() != 0 || path == "" {
		return fail()
	}
	budget := d.Budget
	if budget == 0 {
		budget = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	proof, err := checkProgramQuestion(ctx, path, d)
	if err != nil {
		if diagnostic {
			stage := "validation"
			var failure *generatedQuestionFailure
			if errors.As(err, &failure) {
				stage = failure.stage
			}
			_ = json.NewEncoder(out).Encode(map[string]any{"domain": "owner-program-question-diagnostic-v1", "validated": false, "stage": stage})
			return 1
		}
		return fail()
	}
	if json.NewEncoder(out).Encode(proof) != nil {
		return 1
	}
	return 0
}
