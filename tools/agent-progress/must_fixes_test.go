package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNewestAndPinnedJobsBeyondTenThousand(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"status":"doing","jobs":["20261001-000000-job00000","../escape"]},{"id":2,"status":"recovery_unverified","scheduler":{"job":"20261001-000000-job00001"}}]}`)
	f.opts.MaxJobs = 10000
	for n := 0; n < 10003; n++ {
		id := fmt.Sprintf("20261001-000000-job%05d", n)
		f.job(t, id, fmt.Sprintf(`{"id":%q,"status":"succeeded"}`, id))
	}
	check := func(newest string) {
		t.Helper()
		loaded, source := loadJobs(context.Background(), f.opts, testNow, taskReferences(map[int64]todoItem{
			1: {Jobs: json.RawMessage(`["20261001-000000-job00000"]`), Status: "doing"},
			2: {Scheduler: struct {
				Job string `json:"job"`
			}{Job: "20261001-000000-job00001"}, Status: "recovery_unverified"},
		}, true))
		seen := map[string]bool{}
		for _, j := range loaded {
			seen[j.meta.ID] = true
		}
		if !seen[newest] || !seen["20261001-000000-job00000"] || !seen["20261001-000000-job00001"] {
			t.Fatal("newest job or old canonical references were starved")
		}
		if source.Complete || source.Status != "directory_limit" || source.FailedReads != 0 || source.RecordsRead != 10002 {
			t.Fatal("recent-history omission must be explicit while pinned reads survive the count cap")
		}
	}
	check("20261001-000000-job10002")
	id := "20261013-120000-new"
	f.job(t, id, fmt.Sprintf(`{"id":%q,"status":"running"}`, id))
	check(id)
}

func TestPinnedJobsByteDeadlineAndInvalidPaths(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"status":"doing","jobs":["a","../escape",{"job":"wrong"}],"scheduler":{"job":"b"}}]}`)
	f.job(t, "a", `{"id":"a","status":"running"}`)
	f.job(t, "b", `{"id":"b","status":"running"}`)
	f.opts.MaxJobs = 1
	s := snap(f)
	if !s.Sources["jobs"].Complete || len(s.Jobs) != 2 || s.Status != "ok" {
		t.Fatal("both canonical references must survive the recent-history cap")
	}
	f.opts.JobsBytes = int64(len(`{"id":"b","status":"running"}`))
	s = snap(f)
	if s.Sources["jobs"].Complete || s.Sources["jobs"].Status != "byte_limit" {
		t.Fatal("pinned reads must still obey aggregate bytes")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s := collect(ctx, f.opts, testNow, absentProbe); s.Sources["jobs"].Status != "deadline" {
		t.Fatal("pinned reads must obey the acquisition deadline")
	}
}

func TestMissingPinnedReferenceDoesNotHideHistoryOmission(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"status":"doing","jobs":["missing"]}]}`)
	f.job(t, "old", `{"id":"old","status":"succeeded"}`)
	f.job(t, "new", `{"id":"new","status":"running"}`)
	f.opts.MaxJobs = 1
	s := snap(f)
	if s.Sources["jobs"].Complete || s.Sources["jobs"].Status != "directory_limit" || s.Sources["jobs"].FailedReads != 1 {
		t.Fatal("a missing pinned reference hid omitted metadata directories")
	}
}

func TestFinalizedPIDReuseDoesNotResurrectJobs(t *testing.T) {
	for _, status := range []string{"succeeded", "failed", "cancelled", "lost"} {
		for _, worker := range []string{"unknown", "alive_identity_unknown", "identity_mismatch", "absent", "not_recorded"} {
			if execution(jobMeta{Status: status}, worker, "verified_alive") != "finished" {
				t.Fatal("terminal metadata was resurrected without a verified live worker")
			}
		}
		if execution(jobMeta{Status: status}, "verified_alive", "unknown") != "running" {
			t.Fatal("a verified exact live worker identity must remain observable")
		}
	}
	f := fixtureFor(t, `{"items":[]}`)
	f.job(t, "old", `{"id":"old","status":"succeeded","codex_pid":55,"codex_pid_created":50}`)
	s := collect(context.Background(), f.opts, testNow, func(uint32) processObservation { return processObservation{} })
	if len(s.Jobs) != 0 {
		t.Fatal("an inaccessible reused PID retained a standalone ended job")
	}
	f = fixtureFor(t, `{"items":[{"id":1,"status":"doing","jobs":["old"]}]}`)
	f.job(t, "old", `{"id":"old","status":"succeeded","codex_pid":55,"codex_pid_created":50}`)
	s = collect(context.Background(), f.opts, testNow, func(uint32) processObservation { return processObservation{} })
	if len(s.Jobs) != 1 || s.Jobs[0].Execution != "finished" || s.Jobs[0].WorkerPresence != "unknown" || s.Tasks[0].Execution != "finished" {
		t.Fatal("latest linked terminal evidence must survive without claiming execution")
	}
}

func TestRecoveryUnverifiedContract(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"status":"recovery_unverified","project":"PRIVATE_SYNTHETIC","progress":42}]}`)
	s := snap(f)
	row := s.Tasks[0]
	if row.RecordedStatus != "recovery_unverified" || row.Phase != "waiting" || row.Execution != "not_observed" ||
		len(row.WaitReasons) != 1 || row.WaitReasons[0].Kind != "recovery_unverified" || row.WaitReasons[0].Label != "復旧状況が未確認" ||
		row.PublicLabel != "名称未確認" || row.Percent == nil || *row.Percent != 42 {
		t.Fatal("unverified recovery must retain its dedicated safe explanation and measured progress")
	}
	schemaBytes, err := os.ReadFile("snapshot.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	var output any
	data, _ := json.Marshal(s)
	if json.Unmarshal(schemaBytes, &schema) != nil || json.Unmarshal(data, &output) != nil || validateContract(schema, schema, output) != nil {
		t.Fatal("dedicated recovery state and label violate the published schema")
	}
	if bytes.Contains(data, []byte("PRIVATE_SYNTHETIC")) {
		t.Fatal("private recovery name escaped")
	}
}

func TestOfficialPolicySourceAndProjectRankParity(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "cc-job-policy", "src", "priority.rs"))
	if err != nil {
		t.Fatal("canonical ranking source unavailable")
	}
	// Git's Windows checkout can use CRLF; policy meaning and fixture hashes
	// are bound to the canonical LF bytes, not checkout newline conversion.
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != "6ca1c0ac725ef1ed4b529c1d0e047a70ea4aa2e24a9c3b5e5037764c490fcb0a" {
		t.Fatal("canonical ranking source changed; review the port against its new fixed revision")
	}
	// Check every exact canonical rule in memory, including coded aliases.
	// Diagnostic messages contain only the rule number, never source literals.
	blocks := regexp.MustCompile(`(?s)match (normalized|p)\.as_str\(\) \{(.*?)\n    \}`).FindAllSubmatch(data, -1)
	count := 0
	for _, block := range blocks {
		rules := regexp.MustCompile(`(?m)^\s*((?:"[^"\r\n]+"(?:\s*\|\s*)?)+)\s*=>\s*(?:return\s+)?(\d+),`).FindAllSubmatch(block[2], -1)
		for _, rule := range rules {
			want, _ := strconv.Atoi(string(rule[2]))
			for _, literal := range regexp.MustCompile(`"([^"\r\n]+)"`).FindAllSubmatch(rule[1], -1) {
				count++
				if projectRank(string(literal[1])) != want {
					t.Fatalf("canonical ranking rule %d differs", count)
				}
			}
		}
	}
	if count != 46 {
		t.Fatal("canonical ranking rule extraction incomplete")
	}
	for _, tc := range []struct {
		project    string
		rank       int
		department string
	}{
		{"Social Pilot", 10, "受託部"}, {"civ6-banseiki", 20, "万世紀"},
		{"CC-example", 30, "情報システム部"}, {"opencodex-example", 30, "情報システム部"},
		{"chatgpt-web", 30, "情報システム部"}, {"chrome-bookmark-worker", 30, "情報システム部"}, {"常駐", 30, "情報システム部"},
		{"example-youtube-data", 40, "発信部"}, {"example-health-channel", 40, "発信部"},
		{"example-adj-daily-shorts", 40, "発信部"}, {"example-x-skill", 40, "発信部"}, {"example-xブックマーク", 40, "発信部"},
		{"Skills", 50, "情報システム部"}, {"GoDD-MCP-Alpha", 60, "GoDD事業部"},
		{"GoDD-Press-example", 61, "GoDD事業部"}, {"GoDD-AO", 62, "GoDD事業部"}, {"13a13e1", 63, "GoDD事業部"},
		{"GoDD-Rosseta", 64, "GoDD事業部"}, {"GoDD-Genesis", 65, "GoDD事業部"},
		{"GoDD-VirtualStreamer", 66, "GoDD事業部"}, {"godd-example", 67, "GoDD事業部"}, {"Re:X", 100, "自社サービス部"},
	} {
		got := itemLabel(todoItem{Project: tc.project}, priorityPolicy{})
		if got.rank != tc.rank || got.department != tc.department {
			t.Fatal("public ranking fixture differs")
		}
	}
	for _, private := range []string{"cc-SYNTHETIC_PRIVATE", "GoDD-Press-SYNTHETIC_PRIVATE", "SYNTHETIC_PRIVATE-youtube"} {
		l := publicLabel(private)
		if l.name != "名称未確認" || l.kind != "generic" || l.rank == 100 {
			t.Fatal("rank matching leaked a private label or lost official classification")
		}
	}
}

func TestMCPIdentityPortMatchesReviewedCanonicalSource(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "cc-job-policy", "src", "product_identity.rs"))
	if err != nil {
		t.Fatal("canonical identity source unavailable")
	}
	sum := sha256.Sum256(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))
	if hex.EncodeToString(sum[:]) != "01c4826712c3bfcfbca60b431d339ec6176ed4b4abd13cb042e5f8207001ff82" {
		t.Fatal("canonical identity source changed; review the port and synthetic fixtures")
	}
}
func TestOfficialLinkedJobPriorityAndInheritance(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"project":"Re:X","status":"todo"},{"id":2,"project":"cc-example","status":"wait","wait_on":[3]},{"id":3,"project":"SYNTHETIC_PRIVATE","status":"todo"}]}`)
	f.job(t, "linked", `{"id":"linked","project":"tools","status":"queued","todo_id":1}`)
	f.job(t, "parent", `{"id":"parent","project":"Re:X","status":"running","todo_id":3}`)
	f.job(t, "child", `{"id":"child","project":"Re:X","status":"queued","parent_job_id":"parent","subwork":false}`)
	f.job(t, "independent", `{"id":"independent","project":"Re:X","status":"queued","parent_job_id":"parent","subwork":{}}`)
	f.job(t, "cancelled-parent", `{"id":"cancelled-parent","project":"tools","status":"running","cancel_requested":true}`)
	f.job(t, "no-inherit", `{"id":"no-inherit","project":"Re:X","status":"queued","parent_job_id":"cancelled-parent"}`)
	s := snap(f)
	byKey := map[string]Job{}
	for _, j := range s.Jobs {
		byKey[j.Key] = j
	}
	if byKey[jobKey("linked")].Priority != 100 || byKey[jobKey("parent")].Priority != 30 ||
		byKey[jobKey("child")].Priority != 30 || byKey[jobKey("independent")].Priority != 100 || byKey[jobKey("no-inherit")].Priority != 100 {
		t.Fatal("linked ToDo ownership, prerequisite promotion, independent subwork, or cancellation differs from official job_ranks")
	}
	for _, row := range s.Tasks {
		if row.ID == 3 && (row.Priority != 30 || row.Department != "情報システム部" || row.PublicLabel != "名称未確認") {
			t.Fatal("effective official rank and safe label must remain separate")
		}
	}
	// Empty strings are explicit as_str values in the Rust policy; they must
	// not activate a fallback project/department. Policy keys fold ASCII only.
	l := itemLabel(todoItem{ID: 1, Project: "tools", Department: "受託部"}, priorityPolicy{Items: map[string]json.RawMessage{"1": json.RawMessage(`{"project":"","department":""}`)}})
	if l.rank != 100 || l.name != "名称未確認" {
		t.Fatal("explicit empty ownership did not override fallback fields")
	}
	if asciiEqualFold("ſkills", "Skills") || !asciiEqualFold("TOOLS", "tools") {
		t.Fatal("project policy key matching must fold ASCII only")
	}
}

func TestLegacyOffsetTimeAndUnspecifiedQueueWait(t *testing.T) {
	for _, s := range []string{"2026-10-13T19:00:00+0900", "2026-10-13T03:00:00-0700", "2026-10-13T19:00:00.000000000+0900"} {
		if timeClock(s, testNow, time.Minute).State != "fresh" {
			t.Fatal("canonical legacy offset time was rejected")
		}
	}
	w := jobWaits(jobMeta{Status: "queued"}, testNow, time.Minute)
	if len(w) != 1 || w[0].Kind != "unknown" {
		t.Fatal("unrecorded wait cause was inferred as a slot limit")
	}
	if strings.Contains(w[0].Label, "実行") {
		t.Fatal("unrecorded queue reason cannot claim execution")
	}
}
