package main

// Native preview qualification is a separate purpose. It never changes a
// general guard verdict, issues imagegen credentials, or performs HTTP.
import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const nativeEvidenceDomain = "owner-native-question-evidence-v1"
const nativeQuestionDomain = "owner-native-question-proof-v1"

func canonicalJSON(b []byte) ([]byte, error) {
	var v any
	raw := bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	if historyStrictJSON(raw, &v) != nil {
		return nil, errConversationMedia
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&v) != nil {
		return nil, errConversationMedia
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(v) != nil {
		return nil, errConversationMedia
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

type nativeQuestionEvent struct {
	Line int    `json:"line"`
	SHA  string `json:"sha256"`
}
type nativeQuestionImage struct {
	Path     string `json:"path"`
	SHA      string `json:"sha256"`
	Size     int64  `json:"size"`
	Producer string `json:"producer_path,omitempty"`
}
type nativeQuestionEvidence struct {
	Domain        string                 `json:"domain"`
	Request       string                 `json:"request_id"`
	Origin        string                 `json:"origin_thread_id"`
	Scope         generatedQuestionScope `json:"scope"`
	Job           string                 `json:"job_id"`
	Worktree      string                 `json:"worktree"`
	Session       string                 `json:"session"`
	Rollout       string                 `json:"rollout_path"`
	SessionSHA    string                 `json:"session_record_sha256"`
	Assignment    nativeQuestionEvent    `json:"assignment_event"`
	Body          string                 `json:"body_path"`
	Decision      string                 `json:"decision_path"`
	Title         string                 `json:"title"`
	Images        []nativeQuestionImage  `json:"images"`
	Question      nativeQuestionEvent    `json:"question_event"`
	Content       nativeQuestionEvent    `json:"question_content_event"`
	Producer      nativeQuestionEvent    `json:"producer_event"`
	Copy          nativeQuestionEvent    `json:"copy_event"`
	CopyResult    nativeQuestionEvent    `json:"copy_result_event"`
	Preview       nativeQuestionEvent    `json:"preview_event"`
	PreviewResult nativeQuestionEvent    `json:"preview_result_event"`
	Async         nativeQuestionEvent    `json:"async_event"`
	Accepted      nativeQuestionEvent    `json:"accepted_event"`
	ContentReview string                 `json:"content_review"`
	AsyncTitle    string                 `json:"async_title"`
	AsyncOptions  []string               `json:"async_options"`
}
type nativeQuestionProof struct {
	Domain      string                 `json:"domain"`
	Validated   bool                   `json:"validated"`
	SendAllowed bool                   `json:"send_allowed"`
	Request     string                 `json:"request_id"`
	Origin      string                 `json:"origin_thread_id"`
	Scope       generatedQuestionScope `json:"scope"`
	Title       string                 `json:"title"`
	Images      []nativeQuestionImage  `json:"images"`
	AsyncCall   string                 `json:"async_call_id"`
	EvidenceSHA string                 `json:"evidence_sha256"`
}
type nativeQuestionDeps struct {
	Sessions   string
	Now        func() time.Time
	Read       func(string, int64) ([]byte, error)
	Records    func(string, []nativeQuestionEvent) ([]json.RawMessage, error)
	Bind       func(context.Context, nativeQuestionEvidence, json.RawMessage) error
	Content    func(nativeQuestionEvidence, []byte, []byte) error
	Capture    func(nativeQuestionEvidence) error
	ImageGuard func(context.Context, string, bool) error
	Clear      func(string) bool
}
type nativeQuestionRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Payload   struct {
		Type, Name, Input, Arguments, Role string
		Call                               string                        `json:"call_id"`
		Output                             json.RawMessage               `json:"output"`
		Content                            []struct{ Type, Text string } `json:"content"`
	} `json:"payload"`
}

func nativeQuestionTokenCommands(code string) ([]string, error) {
	// Only direct literal exec calls returned to text. Unknown wrappers fail.
	tokens, err := generatedJSTokens(code)
	if err != nil {
		return nil, err
	}
	p := generatedJSParser{tokens: tokens}
	var commands []string
	for p.at < len(tokens) {
		if !p.sequence("text", "(", "await", "tools", ".", "exec_command", "(", "{") {
			return nil, errConversationMedia
		}
		cmd := ""
		seen := map[string]bool{}
		for !p.take("}") {
			if p.at >= len(tokens) {
				return nil, errConversationMedia
			}
			key := tokens[p.at].text
			p.at++
			if seen[key] || !p.take(":") {
				return nil, errConversationMedia
			}
			seen[key] = true
			if key == "cmd" {
				var ok bool
				cmd, ok = p.literal()
				if !ok {
					return nil, errConversationMedia
				}
			} else {
				// Existing captures may include fixed numeric output budgets. No env,
				// dynamic command, interpolation, callbacks, or side-effect expressions.
				return nil, errConversationMedia
			}
			if p.take("}") {
				break
			}
			if !p.take(",") {
				return nil, errConversationMedia
			}
		}
		if cmd == "" || !p.sequence(")", ")", ";") {
			return nil, errConversationMedia
		}
		commands = append(commands, cmd)
	}
	return commands, nil
}
func nativeQuestionAskMatches(input string, e nativeQuestionEvidence) bool {
	// Parse the native JS literal, not a substring pasted into another command.
	// Numeric max_output_tokens is metadata, stripped only by exact lexical form.
	input = nativeQuestionMetadata(input)
	commands, err := nativeQuestionTokenCommands(input)
	if err != nil || len(commands) == 0 {
		return false
	}
	expected := []string{"--request-id '" + e.Request + "'", "--todo-id " + fmtUint(e.Scope.Todo), "--origin-thread " + e.Origin, "--title '" + e.Title + "'"}
	count := 0
	for _, command := range commands {
		for _, line := range strings.Split(strings.ReplaceAll(command, "\r\n", "\n"), "\n") {
			if !strings.HasPrefix(line, "& 'D:/CC/Tools/discord-ask/discord_ask.exe' recovery-ask ") {
				continue
			}
			count++
			for _, flag := range []string{"--request-id", "--todo-id", "--origin-thread", "--title", "--body-file", "--decision-file"} {
				if strings.Count(line, flag) != 1 {
					return false
				}
			}
			for _, part := range expected {
				if strings.Count(line, part) != 1 {
					return false
				}
			}
			// Relative original paths are resolved against the native session cwd.
			matchPath := func(flag, want string) bool {
				r := regexp.MustCompile(regexp.QuoteMeta(flag) + ` '([^']+)'`)
				m := r.FindAllStringSubmatch(line, -1)
				if len(m) != 1 {
					return false
				}
				p := filepath.FromSlash(m[0][1])
				if !filepath.IsAbs(p) {
					p = filepath.Join(e.Worktree, p)
				}
				return conversationMediaSamePath(filepath.Clean(p), filepath.FromSlash(want))
			}
			if !matchPath("--body-file", e.Body) || !matchPath("--decision-file", e.Decision) {
				return false
			}
			attaches := regexp.MustCompile(`--attach '([^']+)'`).FindAllStringSubmatch(line, -1)
			hashes := regexp.MustCompile(`--expected-image-sha256 ([a-f0-9]{64})(?: |$)`).FindAllStringSubmatch(line, -1)
			if len(attaches) != len(e.Images) || len(hashes) != len(e.Images) {
				return false
			}
			for i, im := range e.Images {
				p := filepath.FromSlash(attaches[i][1])
				if !filepath.IsAbs(p) {
					p = filepath.Join(e.Worktree, p)
				}
				if !conversationMediaSamePath(filepath.Clean(p), filepath.FromSlash(im.Path)) || hashes[i][1] != im.SHA {
					return false
				}
			}
		}
	}
	return count == 1
}
func fmtUint(n uint64) string { b, _ := json.Marshal(n); return string(b) }
func nativePreviewMatches(input string, e nativeQuestionEvidence) bool {
	// The original call may append literal exec diagnostics after the exact
	// ordered preview prefix. That suffix cannot replace emitted images.
	for _, image := range e.Images {
		q, _ := json.Marshal(image.Path)
		prefix := "image((await tools.view_image({path:" + string(q) + "})).image_url);"
		input = strings.TrimLeft(input, " \r\n\t")
		if !strings.HasPrefix(input, prefix) {
			return false
		}
		input = strings.TrimPrefix(input, prefix)
	}
	if strings.TrimSpace(input) == "" {
		return true
	}
	_, err := nativeQuestionTokenCommands(nativeQuestionMetadata(input))
	return err == nil
}
func validateNativeQuestion(ctx context.Context, e nativeQuestionEvidence, d nativeQuestionDeps) (nativeQuestionProof, error) {
	stage := "schema"
	bad := func() (nativeQuestionProof, error) {
		return nativeQuestionProof{}, generatedFailure(stage, "native_evidence_unqualified")
	}
	if d.Now == nil || d.Read == nil || d.Records == nil || d.Bind == nil || d.Content == nil || d.Capture == nil || d.ImageGuard == nil || d.Clear == nil || ctx.Err() != nil || e.Domain != nativeEvidenceDomain || !conversationID.MatchString(e.Request) || !validJob(e.Job) || !generatedUUID.MatchString(e.Session) || !shaPattern.MatchString(e.SessionSHA) || !generatedWithin(d.Sessions, e.Rollout) || !strings.HasSuffix(e.Rollout, "-"+e.Session+".jsonl") || e.Scope.Todo == 0 || len(e.Images) == 0 || len(e.Images) > 10 || len(e.ContentReview) < 16 || len(e.ContentReview) > 4096 {
		return bad()
	}
	for _, id := range []string{e.Origin, e.Scope.Owner, e.Scope.Guild, e.Scope.Forum} {
		if _, ok := number(id); !ok {
			return bad()
		}
	}
	if !shaPattern.MatchString(e.Scope.Body) || !shaPattern.MatchString(e.Scope.Decision) {
		return bad()
	}
	for _, p := range []string{e.Worktree, e.Body, e.Decision} {
		if !filepath.IsAbs(p) || !d.Clear(p) {
			return bad()
		}
	}
	if strings.TrimSpace(e.Title) == "" || len(e.Title) > 256 {
		return bad()
	}
	seen := map[string]bool{}
	for _, im := range e.Images {
		k := strings.ToLower(im.Path)
		if !filepath.IsAbs(im.Path) || !shaPattern.MatchString(im.SHA) || im.Size < 1 || im.Size > conversationMediaLimit || seen[k] || !d.Clear(im.Path) {
			return bad()
		}
		seen[k] = true
	}
	bounds := []nativeQuestionEvent{{Line: 1, SHA: e.SessionSHA}, e.Question, e.Preview, e.PreviewResult, e.Async, e.Accepted}
	for i, b := range bounds {
		if b.Line < 1 || !shaPattern.MatchString(b.SHA) || (i > 0 && bounds[i-1].Line >= b.Line) {
			return bad()
		}
	}
	stage = "native_records"
	records, err := d.Records(e.Rollout, bounds)
	if err != nil || len(records) != len(bounds) {
		return bad()
	}
	var meta struct {
		Type    string
		Payload struct{ ID, Cwd string }
	}
	if json.Unmarshal(records[0], &meta) != nil || meta.Type != "session_meta" || meta.Payload.ID != e.Session || !conversationMediaSamePath(filepath.FromSlash(meta.Payload.Cwd), filepath.FromSlash(e.Worktree)) {
		return bad()
	}
	r := make([]nativeQuestionRecord, 5)
	for i := range r {
		if json.Unmarshal(records[i+1], &r[i]) != nil || r[i].Type != "response_item" || r[i].Timestamp.IsZero() || r[i].Timestamp.After(d.Now()) || (i > 0 && r[i].Timestamp.Before(r[i-1].Timestamp)) {
			return bad()
		}
	}
	stage = "original_request"
	if !d.Now().Before(r[0].Timestamp.Add(7*24*time.Hour)) || r[0].Payload.Type != "custom_tool_call" || r[0].Payload.Name != "exec" || !nativeQuestionAskMatches(r[0].Payload.Input, e) {
		return bad()
	}
	stage = "original_preview"
	if r[1].Payload.Type != "custom_tool_call" || r[1].Payload.Name != "exec" || r[2].Payload.Type != "custom_tool_call_output" || r[1].Payload.Call == "" || r[1].Payload.Call != r[2].Payload.Call || !nativePreviewMatches(r[1].Payload.Input, e) {
		return bad()
	}
	var blocks []struct {
		Type string `json:"type"`
		URL  string `json:"image_url"`
		Text string `json:"text"`
	}
	if json.Unmarshal(r[2].Payload.Output, &blocks) != nil {
		return bad()
	}
	index := 0
	var total int64
	for _, b := range blocks {
		if b.Type == "input_text" {
			continue
		}
		if b.Type != "input_image" || index >= len(e.Images) || !strings.HasPrefix(b.URL, "data:image/png;base64,") {
			return bad()
		}
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(b.URL, "data:image/png;base64,"))
		im := e.Images[index]
		if err != nil || !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) || bytesHash(data) != im.SHA || int64(len(data)) != im.Size {
			return bad()
		}
		total += im.Size
		index++
	}
	if index != len(e.Images) || total > 23<<20 {
		return bad()
	}
	stage = "async_association"
	if r[3].Payload.Type != "function_call" || r[3].Payload.Name != "request_user_input_async" || r[4].Payload.Type != "function_call_output" || r[3].Payload.Call == "" || r[4].Payload.Call != r[3].Payload.Call {
		return bad()
	}
	var accepted struct {
		Accepted bool `json:"accepted"`
	}
	var encoded string
	raw := r[4].Payload.Output
	if json.Unmarshal(raw, &encoded) == nil {
		raw = []byte(encoded)
	}
	if historyStrictJSON(raw, &accepted) != nil || !accepted.Accepted {
		return bad()
	}
	var question struct {
		Questions []struct {
			Title   string   `json:"title"`
			Options []string `json:"options"`
		} `json:"questions"`
	}
	if historyStrictJSON([]byte(r[3].Payload.Arguments), &question) != nil || len(question.Questions) != 1 || len(question.Questions[0].Options) < 2 {
		return bad()
	}
	if question.Questions[0].Title != e.AsyncTitle || len(e.AsyncOptions) != len(question.Questions[0].Options) || e.AsyncTitle == "" {
		return bad()
	}
	for i, v := range e.AsyncOptions {
		if v != question.Questions[0].Options[i] {
			return bad()
		}
	}
	stage = "current_question"
	body, err := d.Read(e.Body, 128<<10)
	if err != nil || bytesHash(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))) != e.Scope.Body {
		return bad()
	}
	decision, err := d.Read(e.Decision, 128<<10)
	if err != nil {
		return bad()
	}
	canon, err := canonicalJSON(decision)
	if err != nil || bytesHash(canon) != e.Scope.Decision {
		return bad()
	}
	var dv map[string]any
	if json.Unmarshal(canon, &dv) != nil || dv["todo_id"] != float64(e.Scope.Todo) {
		return bad()
	}
	stage = "original_content"
	if d.Content(e, body, canon) != nil {
		return bad()
	}
	stage = "playwright_capture"
	if d.Capture(e) != nil {
		return bad()
	}
	stage = "current_origin"
	if d.Bind(ctx, e, records[0]) != nil || ctx.Err() != nil {
		return bad()
	}
	stage = "current_image"
	for _, im := range e.Images {
		if d.ImageGuard(ctx, im.Path, im.Producer != "") != nil {
			return bad()
		}
		data, err := d.Read(im.Path, conversationMediaLimit)
		if err != nil || int64(len(data)) != im.Size || bytesHash(data) != im.SHA {
			return bad()
		}
	}
	return nativeQuestionProof{Domain: nativeQuestionDomain, Validated: true, SendAllowed: false, Request: e.Request, Origin: e.Origin, Scope: e.Scope, Title: e.Title, Images: e.Images, AsyncCall: r[3].Payload.Call}, nil
}
func nativeQuestionCLIWithDeps(args []string, out io.Writer, d nativeQuestionDeps) int {
	f := flag.NewFlagSet("check-native-question-evidence", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var path string
	var diagnostic bool
	f.BoolVar(&diagnostic, "diagnostic", false, "return only the fixed failed stage")
	conversationStringFlag(f, "evidence", &path)
	fail := func() int { _, _ = io.WriteString(out, "native question evidence not qualified\n"); return 1 }
	if f.Parse(args) != nil || f.NArg() != 0 || path == "" {
		return fail()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b, err := d.Read(path, 1<<20)
	var e nativeQuestionEvidence
	if err != nil || historyStrictJSON(b, &e) != nil {
		return fail()
	}
	proof, err := validateNativeQuestion(ctx, e, d)
	if err != nil {
		if diagnostic {
			_ = json.NewEncoder(out).Encode(generatedDiagnostic(err))
			return 1
		}
		return fail()
	}
	proof.EvidenceSHA = bytesHash(b)
	if json.NewEncoder(out).Encode(proof) != nil {
		return 1
	}
	return 0
}

func nativeQuestionMetadata(code string) string {
	// Output budgets are execution metadata, but substitutions inside the
	// literal command itself would alter the very source being audited.
	var out strings.Builder
	for i := 0; i < len(code); {
		if code[i] == '\'' || code[i] == '"' {
			quote := code[i]
			start := i
			i++
			for i < len(code) {
				if code[i] == '\\' {
					i += 2
					continue
				}
				if code[i] == quote {
					i++
					break
				}
				i++
			}
			if i > len(code) {
				return code
			}
			out.WriteString(code[start:i])
			continue
		}
		if code[i] == ',' {
			m := regexp.MustCompile(`^,\s*(?:"(?:max_output_tokens|yield_time_ms)"|(?:max_output_tokens|yield_time_ms))\s*:\s*[0-9]+`).FindString(code[i:])
			if m != "" {
				i += len(m)
				continue
			}
		}
		out.WriteByte(code[i])
		i++
	}
	return out.String()
}
