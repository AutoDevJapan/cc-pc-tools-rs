package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func text(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	return ""
}
func integer(v json.RawMessage) int64 { n, _ := domainInt(v); return n }
func object(v json.RawMessage) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(v, &m) != nil {
		return nil
	}
	return m
}
func timestamp(t time.Time) string {
	return t.In(time.FixedZone("JST", 9*3600)).Format("2006-01-02 15:04")
}

// editDocument proposes upserts against one snapshot. Store.Update owns the CAS
// and commits the selected item and dependency releases in one transaction.
// An unchanged status is an edit, not another invocation of start/block/done.
func editDocument(document json.RawMessage, id int64, title, note, newStatus string, percent *int, at time.Time) ([]json.RawMessage, error) {
	root := object(document)
	items, ok := domainArray(root["items"])
	if !ok {
		return nil, errors.New("invalid task collection")
	}
	rows := make([]map[string]json.RawMessage, len(items))
	seen := make(map[int64]bool, len(items))
	index := -1
	for i, b := range items {
		rows[i] = object(b)
		rowID, valid := domainInt(rows[i]["id"])
		if rows[i] == nil || !valid || seen[rowID] {
			return nil, errors.New("invalid task collection")
		}
		seen[rowID] = true
		if rowID == id {
			index = i
		}
	}
	if index < 0 {
		return nil, errors.New("task not found")
	}
	if strings.TrimSpace(title) == "" || !utf8.ValidString(title) {
		return nil, errors.New("タスク名を入力してください")
	}
	if !utf8.ValidString(note) {
		return nil, errors.New("メモをUTF-8で入力してください")
	}
	if percent != nil && (*percent < 0 || *percent > 100) {
		return nil, errors.New("進捗は0〜100で指定してください")
	}
	item := rows[index]
	oldStatus := text(item["status"])
	if newStatus == "" {
		newStatus = oldStatus
	}
	transition := newStatus != oldStatus
	if newStatus == "done" && percent != nil && *percent != 100 {
		return nil, errors.New("完了したタスクの進捗は100です。再開してから変更してください")
	}
	if transition {
		switch newStatus {
		case "doing":
			if oldStatus == "recovery_unverified" {
				return nil, errors.New("復旧したToDoは既存CLIのresume-verifiedで保存記録を照合してください")
			}
			if hasPending(rows, item) {
				return nil, errors.New("待っているToDoが未完了です。完了すると自動で再開します")
			}
			if domainPresent(item["wait_ready"]) && !domainRetryDue(item["wait_ready"], at) {
				return nil, errors.New("再開条件をまだ確認できません。既存CLIの再開条件確認手続きを使ってください")
			}
		case "block":
		case "done":
			if !domainCompletionAllowed(item) {
				return nil, errors.New("容量判定は既存CLIのcapacity-resultで現在の測定世代の結果を記録してから完了してください")
			}
		default:
			return nil, errors.New("この状態への変更は、依存・復旧情報を保持する既存CLIの手続きから行ってください")
		}
	}
	// Browsers submit CRLF in textarea values. Compare the same normalized text
	// as edit_item, so changing a title does not manufacture a note revision.
	{
		normalized := strings.TrimPrefix(note, "\ufeff")
		normalized = strings.ReplaceAll(strings.ReplaceAll(normalized, "\r\n", "\n"), "\r", "\n")
		if !(transition && (newStatus == "block" || newStatus == "done")) && normalized != text(item["note"]) && (len(note) > 1<<20 || strings.TrimFunc(normalized, func(c rune) bool { return unicode.IsSpace(c) || c >= '\x1c' && c <= '\x1f' }) == "") {
			return nil, errors.New("メモは空白だけにせず、1 MiB以内で入力してください")
		}
		note = normalized
	}
	// Done's empty --note keeps the saved note. Block's reason may be empty.
	noteChanged := note != text(item["note"])
	if transition && newStatus == "done" && note == "" {
		noteChanged = false
	}
	if transition && newStatus == "block" && !domainEqual(item["note"], raw(note)) {
		noteChanged = true
	}
	var history []json.RawMessage
	if noteChanged {
		if existing, exists := item["note_history"]; exists {
			var valid bool
			history, valid = domainArray(existing)
			if !valid {
				return nil, errors.New("メモ履歴が配列ではありません。既存の履歴を保持して形式を確認してください")
			}
		}
	}
	changed := make([]bool, len(rows))
	if title != text(item["title"]) {
		item["title"] = raw(title)
		changed[index] = true
	}
	if noteChanged {
		if previous, exists := item["note"]; exists {
			history = append(history, domainObject(map[string]json.RawMessage{"note": previous, "updated": domainValue(item["updated"])}))
			item["note_history"] = domainEncodeArray(history)
		}
		item["note"] = raw(note)
		changed[index] = true
	}
	if transition {
		switch newStatus {
		case "doing":
			// Start releases dependencies before clearing wait_ready or assigning
			// the new state; this also releases the selected wait itself.
			domainReleaseReady(rows, changed)
			delete(item, "wait_ready")
			if oldStatus == "done" && percent == nil {
				delete(item, "progress")
			}
			if err := domainResumeWithoutWorker(item); err != nil {
				return nil, err
			}
		case "block":
			if oldStatus == "done" && percent == nil {
				delete(item, "progress")
			}
		case "done":
			item["progress"] = raw(100)
			delete(item, "wait_on")
			delete(item, "wait_ready")
		}
		item["status"] = raw(newStatus)
		changed[index] = true
		if newStatus == "done" {
			domainReleaseReady(rows, changed)
		}
	}
	if percent != nil {
		item["progress"] = raw(*percent)
		changed[index] = true
	}
	if changed[index] {
		item["updated"] = raw(timestamp(at))
	}
	result := make([]json.RawMessage, 0)
	for i, row := range rows {
		if changed[i] {
			result = append(result, domainObject(row))
		}
	}
	return result, nil
}

func domainInt(v json.RawMessage) (int64, bool) {
	n, err := strconv.ParseInt(string(bytes.TrimSpace(v)), 10, 64)
	return n, err == nil
}
func domainUint(v json.RawMessage) (uint64, bool) {
	n, err := strconv.ParseUint(string(bytes.TrimSpace(v)), 10, 64)
	return n, err == nil
}
func domainArray(v json.RawMessage) ([]json.RawMessage, bool) {
	var a []json.RawMessage
	v = bytes.TrimSpace(v)
	if len(v) == 0 || v[0] != '[' || json.Unmarshal(v, &a) != nil {
		return nil, false
	}
	return a, true
}
func domainPresent(v json.RawMessage) bool {
	return len(bytes.TrimSpace(v)) > 0 && !bytes.Equal(bytes.TrimSpace(v), []byte("null"))
}
func domainValue(v json.RawMessage) json.RawMessage {
	if len(v) == 0 {
		return json.RawMessage("null")
	}
	return v
}

// Keep unknown values as raw JSON, including number lexemes and key spelling.
func domainObject(m map[string]json.RawMessage) json.RawMessage {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(raw(key))
		b.WriteByte(':')
		b.Write(domainValue(m[key]))
	}
	b.WriteByte('}')
	return b.Bytes()
}
func domainEncodeArray(a []json.RawMessage) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, value := range a {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(domainValue(value))
	}
	b.WriteByte(']')
	return b.Bytes()
}
func domainEqual(a, b json.RawMessage) bool {
	decode := func(v json.RawMessage) any {
		d := json.NewDecoder(bytes.NewReader(domainValue(v)))
		d.UseNumber()
		var value any
		_ = d.Decode(&value)
		return domainComparable(value)
	}
	return reflect.DeepEqual(decode(a), decode(b))
}
func domainComparable(v any) any {
	switch x := v.(type) {
	case json.Number:
		if strings.ContainsAny(string(x), ".eE") {
			if f, err := strconv.ParseFloat(string(x), 64); err == nil {
				// serde_json distinguishes integer and floating-point numbers.
				return struct{ Float float64 }{f}
			}
		}
	case []any:
		for i := range x {
			x[i] = domainComparable(x[i])
		}
	case map[string]any:
		for key := range x {
			x[key] = domainComparable(x[key])
		}
	}
	return v
}
func domainDoneIDs(items []map[string]json.RawMessage) map[int64]bool {
	done := make(map[int64]bool)
	for _, row := range items {
		if id, ok := domainInt(row["id"]); ok && text(row["status"]) == "done" {
			done[id] = true
		}
	}
	return done
}
func domainPending(done map[int64]bool, item map[string]json.RawMessage) bool {
	on, _ := domainArray(item["wait_on"])
	for _, v := range on {
		if id, ok := domainInt(v); ok && !done[id] {
			return true
		}
	}
	return false
}
func hasPending(items []map[string]json.RawMessage, item map[string]json.RawMessage) bool {
	return domainPending(domainDoneIDs(items), item)
}

// Match todo_wait::release_ready/release_item. Owner blocks only lose their
// completed edges; reservations change for wait items only.
func domainReleaseReady(items []map[string]json.RawMessage, changed []bool) {
	done := domainDoneIDs(items)
	for i, row := range items {
		on, _ := domainArray(row["wait_on"])
		if len(on) == 0 || domainPending(done, row) || domainPresent(row["wait_ready"]) {
			continue
		}
		delete(row, "wait_on")
		changed[i] = true
		if text(row["status"]) != "wait" {
			continue
		}
		row["status"] = raw("todo")
		row["dependency_ready"] = raw(true)
		generation, _ := domainUint(row["dependency_generation"])
		if generation < math.MaxUint64 {
			generation++
		}
		row["dependency_generation"] = raw(generation)
		if scheduler := object(row["scheduler"]); scheduler != nil {
			if text(scheduler["job"]) != "" {
				scheduler["last_job"] = scheduler["job"]
			}
			for _, key := range []string{"job", "key", "selection"} {
				delete(scheduler, key)
			}
			scheduler["not_before"] = raw(0)
			scheduler["phase"] = raw("retry")
			scheduler["resume"] = raw(true)
			row["scheduler"] = domainObject(scheduler)
		}
		if recovery := object(row["stall_recovery"]); recovery != nil {
			for _, key := range []string{"pending", "report", "since"} {
				delete(recovery, key)
			}
			recovery["next_at"] = raw(0)
			row["stall_recovery"] = domainObject(recovery)
		}
		delete(row, "discord_restart_not_before")
	}
}

func domainResumeWithoutWorker(item map[string]json.RawMessage) error {
	// Follow common::add_count, including numeric and legacy nonnumeric values.
	var generation json.RawMessage
	if !domainPresent(item["resume_generation"]) {
		generation = raw(1)
	} else if n, ok := domainInt(item["resume_generation"]); ok {
		generation = raw(n + 1)
	} else if n, ok := domainUint(item["resume_generation"]); ok {
		generation = raw(n + 1)
	} else {
		var n float64
		_ = json.Unmarshal(item["resume_generation"], &n)
		generation = raw(n + 1)
		if !bytes.ContainsAny(generation, ".eE") {
			generation = append(generation, '.', '0')
		}
	}
	scheduler := object(item["scheduler"])
	job := domainValue(scheduler["job"])
	jobID := text(job)
	processed := object(item["processed_results"])
	_, received := processed[jobID]
	received = jobID != "" && received
	retire := text(scheduler["phase"]) == "finished" || text(scheduler["phase"]) == "cancelled" || received
	if retire && jobID != "" && !received && domainPresent(item["processed_results"]) && processed == nil {
		return errors.New("ジョブ結果の保存形式を保持して確認する必要があります。既存CLIのstartを使ってください")
	}
	item["resume_generation"] = generation
	item["worker_receipt"] = domainObject(map[string]json.RawMessage{"job": raw(nil), "generation": generation})
	if !retire {
		return nil
	}
	if jobID != "" && !received {
		if processed == nil {
			processed = make(map[string]json.RawMessage)
		}
		u, _ := domainUint(generation)
		processed[jobID] = domainObject(map[string]json.RawMessage{"generation": raw(u), "reason": raw("superseded")})
		item["processed_results"] = domainObject(processed)
	}
	scheduler["last_job"] = job
	scheduler["job"] = raw(nil)
	scheduler["key"] = raw(nil)
	scheduler["phase"] = raw("resumed")
	scheduler["resume"] = raw(true)
	delete(scheduler, "pending_cancel")
	delete(scheduler, "notice_pending")
	item["scheduler"] = domainObject(scheduler)
	return nil
}
func domainRetryDue(wait json.RawMessage, at time.Time) bool {
	w := object(wait)
	since, sOK := domainUint(w["since"])
	until, uOK := domainUint(w["not_before"])
	return text(w["condition"]) == "retry-time" && sOK && uOK && until >= since && until-since >= 60 && until-since <= 86400 && at.Unix() >= 0 && since <= uint64(at.Unix()) && until <= uint64(at.Unix())
}
func domainCompletionAllowed(item map[string]json.RawMessage) bool {
	decision := object(item["capacity_decision"])
	return text(decision["key"]) != "capacity-decision-v1" || domainPresent(decision["result"]) && domainEqual(object(decision["result"])["generation"], decision["active_generation"])
}
func statusLabel(s string) string {
	switch s {
	case "todo":
		return "未着手"
	case "doing":
		return "作業中"
	case "done":
		return "完了"
	case "wait":
		return "依存・再開待ち"
	case "block":
		return "判断待ち"
	case "recovery_unverified":
		return "復旧確認中"
	default:
		if s == "" {
			return "未記録"
		}
		return s
	}
}
func buildNewItem(project, title, note string, at time.Time) json.RawMessage {
	return raw(map[string]any{"project": project, "title": title, "note": note, "status": "todo", "jobs": []any{}, "created": timestamp(at), "created_s": float64(at.UnixNano()) / 1e9, "updated": timestamp(at)})
}
func formatID(id int64) string { return fmt.Sprintf("%d", id) }
