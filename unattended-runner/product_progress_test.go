package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type progressFixture struct {
	t            *testing.T
	r            *Runner
	now          time.Time
	source       Message
	rows         []map[string]any
	mu           sync.Mutex
	messages     map[string]Message
	threads      map[string]Thread
	ledger       newMessageLedger
	sends        int
	requestIDs   []string
	loseResponse bool
	loseEffect   bool
	proof        productRetryProof
}

func progressTestFixture(t *testing.T) *progressFixture {
	t.Helper()
	r, c := fixture(t)
	r.Config.Home = filepath.Join(t.TempDir(), "unattended")
	r.JobsPath = t.TempDir()
	if err := os.MkdirAll(r.Config.Home, 0700); err != nil {
		t.Fatal(err)
	}
	f := &progressFixture{t: t, r: r, now: c.Add(time.Minute), source: msg(c.Add(time.Second), "501", "300", "PRIVATE_RAW_BODY password=fixture-value https://private.invalid/fixture"), messages: map[string]Message{}, threads: map[string]Thread{}, ledger: newMessageLedger{Domain: "new-owner-recovery-messages"}}
	r.Now = func() time.Time { return f.now }
	sum := sha256.Sum256([]byte(f.source.Content))
	r.Config.ProductReports = []ProductReportConfig{{Todo: 2639, Project: "fixture-project", Source: OwnerSource{Thread: f.source.Channel, Message: f.source.ID, Created: f.source.Timestamp, SHA256: hex.EncodeToString(sum[:])}}}
	f.rows = []map[string]any{{"id": uint64(2639), "project": "fixture-project", "status": "doing", "note": "PRIVATE_RAW_NOTE", "spec": "PRIVATE_RAW_PATH", "progress": 40, "jobs": []any{"fixture-job"}, "scheduler": map[string]any{"job": "fixture-job"}}}
	f.saveRows()
	r.Run = f.run
	r.ProcessAlive = func(pid uint32, created uint64) bool { return pid == 77 && created == 88 }
	server(t, r, f.serve)
	return f
}
func (f *progressFixture) saveRows() {
	f.t.Helper()
	raw, _ := json.Marshal(map[string]any{"items": f.rows})
	if err := os.WriteFile(f.r.TodoPath, raw, 0600); err != nil {
		f.t.Fatal(err)
	}
}
func (f *progressFixture) saveLedger() {
	f.t.Helper()
	if err := publish(filepath.Join(filepath.Dir(f.r.Config.Home), "recovery-messages.json"), f.ledger); err != nil {
		f.t.Fatal(err)
	}
}
func argValue(args []string, key string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key {
			return args[i+1]
		}
	}
	return ""
}
func (f *progressFixture) run(_ context.Context, exe string, args ...string) (string, error) {
	if len(args) == 0 {
		f.t.Fatal("empty external command")
	}
	switch args[0] {
	case "--verify-product-resume":
		if len(args) != 2 || args[1] != "1626" {
			f.t.Fatal("qualification scope expanded")
		}
		raw, _ := json.Marshal(f.proof)
		return string(raw), nil
	case "repair-step":
		return `{"repair_mode":true,"error":null}`, nil
	case "recovery-log":
		if exe != "D:/CC/Tools/discord-ask/discord_ask.exe" {
			f.t.Fatal("sender changed")
		}
		f.sends++
		id := argValue(args, "--request-id")
		f.requestIDs = append(f.requestIDs, id)
		if f.loseEffect {
			return "", errors.New("fixture timeout before verified effect")
		}
		body, err := os.ReadFile(argValue(args, "--body-file"))
		if err != nil {
			f.t.Fatal(err)
		}
		if strings.Contains(string(body), "PRIVATE_RAW") || strings.Contains(string(body), "fixture-value") || strings.Contains(string(body), "private.invalid") || strings.Contains(argValue(args, "--title"), "fixture-project") {
			f.t.Fatal("private text was copied to a report")
		}
		thread := argValue(args, "--thread")
		if thread == "" {
			thread = fmt.Sprint(800 + f.sends)
		}
		mid := fmt.Sprint(900 + f.sends)
		m := Message{ID: mid, Channel: thread, Content: string(body) + "\n-# verified", Timestamp: f.now.UTC().Format(time.RFC3339Nano), Flags: 4096}
		m.Author.ID = "400"
		m.Author.Bot = true
		f.mu.Lock()
		f.messages[thread+"/"+mid] = m
		f.threads[thread] = Thread{ID: thread, Type: 11, Guild: "100", Parent: "200", Owner: "400"}
		f.mu.Unlock()
		f.ledger.Events = append(f.ledger.Events, newMessageEvent{Event: "verified", Request: id, Phase: "log:0", Receipt: &newMessageReceipt{Thread: thread, Message: mid, Author: "400", Timestamp: m.Timestamp, Flags: 4096}})
		f.saveLedger()
		if f.loseResponse {
			return "", errors.New("fixture timeout after verified effect")
		}
		return fmt.Sprintf(`{"delivery":"verified","thread_id":"%s"}`, thread), nil
	default:
		f.t.Fatalf("reporting/delivery invoked an execution or permission command: %s", args[0])
	}
	return "", nil
}
func (f *progressFixture) serve(w http.ResponseWriter, q *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if q.Method != "GET" {
		f.t.Error("unexpected HTTP mutation")
		w.WriteHeader(405)
		return
	}
	if q.URL.Path == "/channels/501" {
		respond(w, Thread{ID: "501", Type: 11, Guild: "100", Parent: requestForum, Owner: "300"})
		return
	}
	if q.URL.Path == "/channels/501/messages/"+f.source.ID {
		respond(w, f.source)
		return
	}
	parts := strings.Split(strings.TrimPrefix(q.URL.Path, "/channels/"), "/")
	if len(parts) == 1 {
		if th, ok := f.threads[parts[0]]; ok {
			respond(w, th)
			return
		}
	}
	if len(parts) == 3 && parts[1] == "messages" {
		if m, ok := f.messages[parts[0]+"/"+parts[2]]; ok {
			respond(w, m)
			return
		}
	}
	w.WriteHeader(404)
}

func TestFiniteProductReportsLostResponseRecoversAndUnchangedStateDoesNotRepeat(t *testing.T) {
	f := progressTestFixture(t)
	f.loseResponse = true
	var h Health
	f.r.reportProducts(context.Background(), &h)
	state := f.r.State.ProductReports[2639]
	if f.sends != 1 || h.ProductReports != 1 || state.Pending != nil || state.Thread == "" || len(f.r.State.ProgressLinks) != 1 {
		t.Fatal("lost sender response did not recover the proven effect", h)
	}
	f.r.NoticeAttempted = false
	f.now = f.now.Add(24 * time.Hour)
	f.r.reportProducts(context.Background(), &h)
	if f.sends != 1 {
		t.Fatal("unchanged state produced a new report")
	}
}

func TestFiniteProductRetryReusesSameRequestIDAndSavedBody(t *testing.T) {
	f := progressTestFixture(t)
	f.loseEffect = true
	var h Health
	f.r.reportProducts(context.Background(), &h)
	pending := *f.r.State.ProductReports[2639].Pending
	var restored State
	if err := readJSON(filepath.Join(f.r.Config.Home, "unattended-state.json"), maxState, &restored); err != nil {
		t.Fatal(err)
	}
	f.r.State = &restored
	f.rows[0]["progress"] = 70
	f.saveRows()
	f.now = f.now.Add(time.Minute)
	f.r.NoticeAttempted = false
	f.loseEffect = false
	f.r.reportProducts(context.Background(), &h)
	if len(f.requestIDs) != 2 || f.requestIDs[0] != f.requestIDs[1] || f.r.State.ProductReports[2639].Signature != pending.Signature {
		t.Fatal("retry changed identity or silently replaced the pending observation")
	}
}

func TestFiniteProductSignatureReturnUsesNewGenerationAndExactOldTarget(t *testing.T) {
	f := progressTestFixture(t)
	var h Health
	f.r.reportProducts(context.Background(), &h)
	first := f.r.State.ProductReports[2639].Signature
	f.r.NoticeAttempted = false
	f.now = f.now.Add(productReportInterval)
	f.rows[0]["progress"] = 41
	f.saveRows()
	f.r.reportProducts(context.Background(), &h)
	f.r.NoticeAttempted = false
	f.now = f.now.Add(productReportInterval)
	f.rows[0]["progress"] = 40
	f.saveRows()
	f.r.reportProducts(context.Background(), &h)
	if f.sends != 3 || f.requestIDs[0] == f.requestIDs[2] || f.r.State.ProductReports[2639].Signature != first || len(f.threads) != 1 {
		t.Fatal("returning to an old signature duplicated its send key or changed thread")
	}
}

func TestFiniteProductSameStateOnOtherTodoNeverReusesOriginOrSignatureKey(t *testing.T) {
	f := progressTestFixture(t)
	other := f.r.Config.ProductReports[0]
	other.Todo = 2640
	f.r.Config.ProductReports = append(f.r.Config.ProductReports, other)
	f.rows = append(f.rows, map[string]any{"id": uint64(2640), "project": "fixture-project", "status": "doing", "progress": 40, "jobs": []any{"fixture-job"}, "scheduler": map[string]any{"job": "fixture-job"}})
	f.saveRows()
	var h Health
	f.r.reportProducts(context.Background(), &h)
	f.r.NoticeAttempted = false
	f.r.reportProducts(context.Background(), &h)
	a, b := f.r.State.ProductReports[2639], f.r.State.ProductReports[2640]
	if a == nil || b == nil || a.Signature != b.Signature || a.Thread == b.Thread || f.requestIDs[0] == f.requestIDs[1] || len(f.r.State.ProgressLinks) != 2 {
		t.Fatal("same observed state merged different original todos")
	}
}

func TestProductReplyKeepsExpiredQualificationAndStopsUnchanged(t *testing.T) {
	f := progressTestFixture(t)
	db, at := productCandidateFixture(t)
	proof := candidateRow(db)["product_resume_qualification"].(map[string]any)["receipt"].(productRetryProof)
	f.now = at
	f.proof = proof
	f.r.Config.ProductReports[0].Todo = 1626
	f.rows[0]["id"] = uint64(1626)
	f.rows[0]["status"] = "block"
	f.rows[0]["dispatchable"] = false
	f.rows[0]["product_resume_qualification"] = map[string]any{"receipt": proof}
	f.saveRows()
	var h Health
	f.r.reportProducts(context.Background(), &h)
	thread := f.r.State.ProductReports[1626].Thread
	f.r.loadProgressLinks(context.Background(), map[string]bool{thread: true})
	link := f.r.ProgressLinks[thread]
	f.now = time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	req := &Request{Thread: thread, Message: "999", Origin: &link.ProgressOrigin, Phase: "captured"}
	before, _ := os.ReadFile(f.r.TodoPath)
	if err := f.r.registerOrigin(context.Background(), req, link.ProgressOrigin); err == nil {
		t.Fatal("expired finite qualification delivered new scope")
	}
	after, _ := os.ReadFile(f.r.TodoPath)
	if req.Todo != 0 || string(before) != string(after) || f.sends != 1 {
		t.Fatal("expiry changed a stop or created an execution")
	}
}

func TestTamperedPendingProductBodyNeverReachesSender(t *testing.T) {
	f := progressTestFixture(t)
	f.loseEffect = true
	var h Health
	f.r.reportProducts(context.Background(), &h)
	f.r.State.ProductReports[2639].Pending.Body = "PRIVATE_RAW_BODY"
	f.r.NoticeAttempted = false
	f.r.reportProducts(context.Background(), &h)
	if f.sends != 1 {
		t.Fatal("tampered pending body reached sender")
	}
}

func TestBadProductDoesNotGloballyBlockVerifiedProductOrigin(t *testing.T) {
	f := progressTestFixture(t)
	var h Health
	f.r.reportProducts(context.Background(), &h)
	thread := f.r.State.ProductReports[2639].Thread
	bad := ProgressLink{ProgressOrigin: ProgressOrigin{Scope: productReportScope, RequestID: "bad", Guild: "100", Thread: "899", Message: "999", Todo: 1626, Project: "different"}, Author: "400", Created: f.now.UTC().Format(time.RFC3339Nano)}
	f.r.State.ProgressLinks[bad.Thread] = bad
	f.r.loadProgressLinks(context.Background(), map[string]bool{thread: true, bad.Thread: true})
	if f.r.ProgressLinkRegistryInvalid || f.r.BlockedProgressThreads[thread] || !f.r.BlockedProgressThreads[bad.Thread] || f.r.ProgressLinks[thread].Todo != 2639 {
		t.Fatal("unrelated invalid finite product globally disabled known origins")
	}
}

func TestFiniteProductProgressRequiresChangeAndMinimumInterval(t *testing.T) {
	f := progressTestFixture(t)
	var h Health
	f.r.reportProducts(context.Background(), &h)
	f.rows[0]["progress"] = 41
	f.saveRows()
	f.r.NoticeAttempted = false
	f.now = f.now.Add(productReportInterval - time.Second)
	f.r.reportProducts(context.Background(), &h)
	if f.sends != 1 {
		t.Fatal("progress report interval was bypassed")
	}
	f.now = f.now.Add(time.Second)
	f.r.reportProducts(context.Background(), &h)
	if f.sends != 2 {
		t.Fatal("changed measured progress was not reported at the interval")
	}
}

func TestFiniteProductRejectsOtherOwnerBotSourceAndChangedBody(t *testing.T) {
	for _, mutate := range []func(*Message){func(m *Message) { m.Author.ID = "999" }, func(m *Message) { m.Author.Bot = true }, func(m *Message) { m.Content = "different" }, func(m *Message) { m.Channel = "999" }} {
		f := progressTestFixture(t)
		f.mu.Lock()
		mutate(&f.source)
		f.mu.Unlock()
		var h Health
		f.r.reportProducts(context.Background(), &h)
		if f.sends != 0 || len(f.r.State.ProgressLinks) != 0 {
			t.Fatal("unverified owner source became a product-report origin")
		}
	}
}

func TestFiniteProductReceiptRejectsDifferentThreadAuthorOrNotification(t *testing.T) {
	for _, kind := range []string{"thread", "author", "mention", "flags", "body"} {
		t.Run(kind, func(t *testing.T) {
			f := progressTestFixture(t)
			var h Health
			f.r.reportProducts(context.Background(), &h)
			p := f.ledger.Events[0].Receipt
			f.mu.Lock()
			m := f.messages[p.Thread+"/"+p.Message]
			switch kind {
			case "thread":
				p.Thread = "999"
			case "author":
				m.Author.ID = "999"
			case "mention":
				m.MentionEveryone = true
			case "flags":
				m.Flags = 0
			case "body":
				m.Content = "different"
			}
			f.messages[m.Channel+"/"+m.ID] = m
			f.mu.Unlock()
			f.saveLedger()
			body := strings.TrimSuffix(m.Content, "\n-# verified")
			if kind == "body" {
				body = "original expected body"
			}
			if _, err := f.r.recoverProductNotice(context.Background(), f.requestIDs[0], body, "801"); err == nil {
				t.Fatal("foreign or notifying effect accepted")
			}
		})
	}
}

func TestFiniteProductScopeDoesNotExpandProjectOrCreateArbitraryTarget(t *testing.T) {
	for _, kind := range []string{"unknown", "wrong-project", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			f := progressTestFixture(t)
			switch kind {
			case "unknown":
				f.r.Config.ProductReports[0].Todo = 999
			case "wrong-project":
				f.rows[0]["project"] = "different"
				f.saveRows()
			case "duplicate":
				f.r.Config.ProductReports = append(f.r.Config.ProductReports, f.r.Config.ProductReports[0])
			}
			var h Health
			f.r.reportProducts(context.Background(), &h)
			if f.sends != 0 || len(f.r.State.ProgressLinks) != 0 || f.r.Config.Project != "cc-pc-tools-rs" {
				t.Fatal("finite report config expanded execution scope")
			}
		})
	}
}

func TestProductCompletionAndRunningRequireActualEvidence(t *testing.T) {
	f := progressTestFixture(t)
	row, err := f.r.todo(2639)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.r.JobsPath, "fixture-job"), 0700); err != nil {
		t.Fatal(err)
	}
	job := map[string]any{"id": "fixture-job", "status": "running", "pid": 77, "pid_created": 89}
	if err := publish(filepath.Join(f.r.JobsPath, "fixture-job", "job.json"), job); err != nil {
		t.Fatal(err)
	}
	s, _ := f.r.productSnapshot(row)
	if s.Stage == "running" {
		t.Fatal("reused or stale process identity claimed live work")
	}
	job["pid_created"] = 88
	_ = publish(filepath.Join(f.r.JobsPath, "fixture-job", "job.json"), job)
	s, _ = f.r.productSnapshot(row)
	if s.Stage != "running" {
		t.Fatal("verified live process was not observed")
	}
	row.Status = "done"
	s, _ = f.r.productSnapshot(row)
	if s.Stage == "complete" {
		t.Fatal("task registration/status alone claimed completion")
	}
	job["status"] = "succeeded"
	_ = publish(filepath.Join(f.r.JobsPath, "fixture-job", "job.json"), job)
	s, _ = f.r.productSnapshot(row)
	if s.Stage != "complete" {
		t.Fatal("completed task and successful execution were lost")
	}
}

func TestMissingRegistryStillVerifiesPersistedProductOriginAndDeliversLiteralReply(t *testing.T) {
	f := progressTestFixture(t)
	var h Health
	f.r.reportProducts(context.Background(), &h)
	thread := f.r.State.ProductReports[2639].Thread
	f.r.loadProgressLinks(context.Background(), map[string]bool{thread: true, "other": true})
	if f.r.ProgressLinkRegistryInvalid || f.r.BlockedProgressThreads[thread] || f.r.ProgressLinks[thread].Todo != 2639 {
		t.Fatal("absent registry blocked the verified finite origin")
	}
	m := msg(f.now.Add(time.Second), thread, "300", "a")
	f.mu.Lock()
	f.messages[thread+"/"+m.ID] = m
	f.mu.Unlock()
	link := f.r.ProgressLinks[thread]
	req := &Request{Thread: thread, Guild: "100", RecordVersion: 2, Message: m.ID, Author: "300", Created: m.Timestamp, Source: "discord-api-v10", Text: m.Content, Key: requestKey(thread, m.ID), Phase: "context_pending", Origin: &link.ProgressOrigin}
	f.r.State.Requests[m.ID] = req
	f.rows[0]["status"] = "block"
	f.rows[0]["dispatchable"] = false
	f.saveRows()
	before, _ := os.ReadFile(f.r.TodoPath)
	f.r.admit(context.Background(), &h)
	after, _ := os.ReadFile(f.r.TodoPath)
	if req.Todo != 2639 || req.Phase != "product_reply_saved" || req.Text != "a" || req.Context == "" || string(before) != string(after) || f.sends != 1 {
		t.Fatal("literal reply changed a stop, became an approval or launched a product")
	}
	if err := f.r.results(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestMissingRegistryDerivesOnlyExactOwnerRequestAndSenderReceipt(t *testing.T) {
	for _, wrongOwner := range []bool{false, true} {
		t.Run(fmt.Sprint(wrongOwner), func(t *testing.T) {
			f := progressTestFixture(t)
			f.rows = []map[string]any{{"id": uint64(10), "project": "cc-pc-tools-rs", "status": "doing", "jobs": []any{"fixture-job"}}}
			f.saveRows()
			key := requestKey(f.source.Channel, f.source.ID)
			req := &Request{Thread: f.source.Channel, Guild: "100", RecordVersion: 2, Message: f.source.ID, Author: "300", Created: f.source.Timestamp, Source: "discord-api-v10", Text: safeText(f.source.Content, f.r.Token), Key: key, Todo: 10, Phase: "registered", Reported: "accepted", ProgressThread: "801"}
			f.r.State.Requests[req.Message] = req
			created := f.now.UTC().Format(time.RFC3339Nano)
			m := Message{ID: "901", Channel: "801", Timestamp: created, Content: "fixed progress", Flags: 4096}
			m.Author.ID = "400"
			m.Author.Bot = true
			f.mu.Lock()
			f.messages["801/901"] = m
			f.threads["801"] = Thread{ID: "801", Type: 11, Guild: "100", Parent: "200", Owner: "400"}
			if wrongOwner {
				f.source.Author.ID = "999"
			}
			f.mu.Unlock()
			f.ledger.Events = append(f.ledger.Events, newMessageEvent{Event: "verified", Request: "unattended-" + key + "-accepted", Phase: "log:0", Receipt: &newMessageReceipt{Thread: "801", Message: "901", Author: "400", Timestamp: created, Flags: 4096}})
			f.saveLedger()
			f.r.loadProgressLinks(context.Background(), map[string]bool{"801": true})
			if (f.r.ProgressLinks["801"].Todo == 10) == wrongOwner || f.r.ProgressLinkRegistryInvalid || f.r.BlockedProgressThreads["801"] {
				t.Fatal("absent metadata either blocked all known threads or invented an owner origin")
			}
			if _, err := os.Stat(filepath.Join(f.r.Config.Home, "progress-links.json")); !os.IsNotExist(err) {
				t.Fatal("receiver manufactured an empty registry")
			}
		})
	}
}

func TestProductReplyPublishFailureKeepsAdmissionRetryable(t *testing.T) {
	f := progressTestFixture(t)
	var h Health
	f.r.reportProducts(context.Background(), &h)
	thread := f.r.State.ProductReports[2639].Thread
	f.r.loadProgressLinks(context.Background(), map[string]bool{thread: true})
	origin := f.r.ProgressLinks[thread].ProgressOrigin
	m := msg(f.now.Add(time.Second), thread, "300", "追加の指示")
	f.mu.Lock()
	f.messages[thread+"/"+m.ID] = m
	f.mu.Unlock()
	req := &Request{Thread: thread, Guild: "100", RecordVersion: 2, Message: m.ID, Author: "300", Created: m.Timestamp, Source: "discord-api-v10", Text: m.Content, Key: requestKey(thread, m.ID), Phase: "captured", Origin: &origin}
	f.r.State.Requests[m.ID] = req
	if err := os.WriteFile(filepath.Join(f.r.Config.Home, "steering"), []byte("fixture obstacle"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.r.registerOrigin(context.Background(), req, origin); err == nil || req.Todo != 0 || req.Phase != "captured" {
		t.Fatal("failed private delivery was marked as successfully saved")
	}
}

func TestPhosReportRequiresSavedAndFreshFiniteQualification(t *testing.T) {
	for _, kind := range []string{"valid", "expired", "missing-saved", "bad-fresh"} {
		t.Run(kind, func(t *testing.T) {
			f := progressTestFixture(t)
			db, at := productCandidateFixture(t)
			proof := candidateRow(db)["product_resume_qualification"].(map[string]any)["receipt"].(productRetryProof)
			f.now = at
			f.proof = proof
			f.r.Config.ProductReports[0].Todo = 1626
			f.rows[0]["id"] = uint64(1626)
			f.rows[0]["product_resume_qualification"] = map[string]any{"receipt": proof}
			switch kind {
			case "expired":
				f.now = time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
			case "missing-saved":
				delete(f.rows[0], "product_resume_qualification")
			case "bad-fresh":
				f.proof.RequestHash = "wrong"
			}
			f.saveRows()
			var h Health
			f.r.reportProducts(context.Background(), &h)
			if (f.sends == 1) != (kind == "valid") || f.r.Config.Project != "cc-pc-tools-rs" {
				t.Fatal("Phos report changed or bypassed the finite qualification", kind)
			}
		})
	}
}

func TestOneNoticePerMinuteTickAndProductDoesNotStarveBehindRequests(t *testing.T) {
	f := progressTestFixture(t)
	f.rows = append(f.rows, map[string]any{"id": uint64(10), "project": "cc-pc-tools-rs", "status": "doing"})
	f.saveRows()
	f.r.State.Requests["600"] = &Request{Thread: "501", Message: "600", Todo: 10, Phase: "registered", Key: "fixture"}
	f.r.tick(context.Background())
	if f.sends != 1 {
		t.Fatal("first tick sent multiple notices")
	}
	f.r.tick(context.Background())
	if f.sends != 2 || f.r.State.ProductReports[2639] == nil || f.r.State.ProductReports[2639].Thread == "" {
		t.Fatal("product report was starved or tick sent more than one notice")
	}
}
