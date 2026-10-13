package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The survey reads only supplied operational records. Private values, even
// filenames and arbitrary JSON keys, never become diagnostic output.
func survey(home, todo, jobs, spec, output string) (obj, error) {
	state, sb, e := readJSON(filepath.Join(home, "state.json"))
	if e != nil {
		return nil, e
	}
	db, dbb, e := readJSON(todo)
	if e != nil {
		return nil, e
	}
	original, e := boundedText(spec)
	if e != nil {
		return nil, e
	}
	private := obj{"kind": "original-correspondence-survey", "created_at": time.Now().UTC().Format(time.RFC3339), "state_sha256": digest(sb), "todo_store_sha256": digest(dbb), "original_spec_sha256": digest(original), "original_spec": string(original), "records": []any{}, "acquisitions": []any{}}
	summary := obj{"ok": true, "old_questions": len(array(state["questions"])), "original_spec_read": true, "original_spec_sha256": digest(original), "live_state_changed": false, "guard_changed": false, "messages_sent": 0, "workers_started": 0}
	summary["state_sha256"] = digest(sb)
	summary["state_bytes"] = len(sb)
	entries := []any{}
	for _, entry := range array(state["questions"]) {
		copy := obj{}
		for k, v := range object(entry) {
			copy[k] = v
		}
		copy["record_kind"] = "question"
		entries = append(entries, copy)
	}
	requestKeys := []string{}
	for key := range object(state["requests"]) {
		requestKeys = append(requestKeys, key)
	}
	sort.Strings(requestKeys)
	for _, key := range requestKeys {
		q := object(object(state["requests"])[key])
		copy := obj{}
		for k, v := range q {
			copy[k] = v
		}
		copy["request_thread_key"] = key
		copy["record_kind"] = "request"
		entries = append(entries, copy)
	}
	summary["old_requests"] = len(requestKeys)
	meta := []any{}
	jobPattern := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,159}$`)
	for i, entry := range entries {
		q := object(entry)
		row := obj{"index": i, "record_kind": q["record_kind"], "question": q}
		m := obj{"index": i, "record_kind": q["record_kind"], "present_fields": []string{}}
		fields := []string{"request_thread_key", "thread_id", "after", "question_message", "original_question", "job_id", "job", "todo_id", "related_todos", "context", "cwd", "open", "seen_reply", "last_reply", "decision_identity", "generation", "id", "status", "created", "created_at", "seen", "handled", "jobs", "last_job", "source_job"}
		present := []string{}
		for _, k := range fields {
			if _, ok := q[k]; ok {
				present = append(present, k)
			}
		}
		m["present_fields"] = present
		ctx := object(q["context"])
		jid := str(q["job_id"])
		if jid == "" {
			jid = str(q["job"])
		}
		if jid == "" {
			jid = str(ctx["job_id"])
		}
		if jid == "" {
			jid = str(ctx["job"])
		}
		if jid == "" {
			jid = str(object(q["job"])["id"])
		}
		if jid == "" {
			jid = str(object(q["job"])["job_id"])
		}
		m["job_string_bytes"] = len(str(q["job"]))
		m["job_object_fields"] = len(object(q["job"]))
		row["job_id"] = jid
		candidates := []any{}
		for _, item := range array(db["items"]) {
			v := object(item)
			if jid == "" {
				continue
			}
			for _, j := range array(v["jobs"]) {
				if str(j) == jid {
					candidates = append(candidates, v)
					break
				}
			}
		}
		row["todo_candidates"] = candidates
		m["todo_candidates_from_job"] = len(candidates)
		if jid != "" && jobPattern.MatchString(jid) {
			v, b, err := readJSON(filepath.Join(jobs, jid, "job.json"))
			if err != nil {
				m["job_record_status"] = err.Error()
			} else {
				row["job_record"] = v
				row["job_record_sha256"] = digest(b)
				m["job_record_status"] = "read"
				jf := []string{}
				for _, k := range []string{"id", "todo_id", "related_todos", "status", "cwd", "prompt_file", "prompt_path", "source_job", "parent_job"} {
					if _, ok := v[k]; ok {
						jf = append(jf, k)
					}
				}
				m["job_present_fields"] = jf
				prompts := []any{}
				for _, name := range []string{"prompt.md", "execution-prompt.md", "context-continue.md", "result.txt"} {
					b, err := boundedText(filepath.Join(jobs, jid, name))
					p := obj{"kind": name, "status": "unavailable"}
					if err == nil {
						p["status"] = "read"
						p["sha256"] = digest(b)
						p["body"] = string(b)
					}
					prompts = append(prompts, p)
				}
				row["job_documents"] = prompts
			}
		} else {
			m["job_record_status"] = "job_identifier_missing_or_invalid"
		}
		private["records"] = append(array(private["records"]), row)
		meta = append(meta, m)
	}
	summary["records"] = meta
	// Previous 2467 notes may contain private acquisition paths. Discover only
	// direct report parents under the known operational home, never a PC scan.
	roots := map[string]bool{}
	dirsRead := 0
	entriesRead := 0
	var walkErr error
	var walk func(string, int)
	walk = func(dir string, depth int) {
		if walkErr != nil {
			return
		}
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil || !operationalEvidenceHint(resolved, home) {
			return
		}
		dirsRead++
		if dirsRead > 256 {
			walkErr = errors.New("evidence_discovery_directory_limit_exceeded")
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		entriesRead += len(entries)
		if entriesRead > 10000 {
			walkErr = errors.New("evidence_discovery_entry_limit_exceeded")
			return
		}
		for _, en := range entries {
			if en.Type()&os.ModeSymlink != 0 {
				continue
			}
			p := filepath.Join(dir, en.Name())
			if en.IsDir() {
				if depth < 3 {
					walk(p, depth+1)
				}
				continue
			}
			if en.Name() == "report.json" {
				roots[dir] = true
			}
		}
	}
	walk(home, 0)
	// Extract exact existing directory hints from this original ToDo's own
	// saved notes/specifications. The hints stay private and grant no authority.
	texts := []string{string(original)}
	priorJobsRead := 0
	for _, entry := range array(db["items"]) {
		v := object(entry)
		if v["id"] != float64(2467) {
			continue
		}
		texts = append(texts, str(v["note"]))
		for _, j := range array(v["jobs"]) {
			jid := str(j)
			if !jobPattern.MatchString(jid) {
				continue
			}
			for _, name := range []string{"prompt.md", "result.txt", "progress.jsonl", "events.jsonl", "stdout.jsonl", "output.jsonl"} {
				b, err := boundedText(filepath.Join(jobs, jid, name))
				if err == nil {
					texts = append(texts, string(b))
					priorJobsRead++
				}
			}
		}
		for _, h := range array(v["note_history"]) {
			texts = append(texts, str(object(h)["note"]))
		}
		ps := []string{str(v["spec"])}
		for _, h := range array(v["spec_history"]) {
			ps = append(ps, str(object(h)["spec"]))
		}
		for _, p := range ps {
			b, err := boundedText(p)
			if err == nil {
				texts = append(texts, string(b))
			}
		}
	}
	pathRE := regexp.MustCompile("[A-Za-z]:[\\\\/][^\\s`<>\"'|]+")
	hints := []any{}
	seenHints := map[string]bool{}
	for _, text := range texts {
		for _, token := range pathRE.FindAllString(text, -1) {
			p := strings.TrimRight(token, "。，、;；:：)）]}")
			p = strings.ReplaceAll(p, "\\", "/")
			if seenHints[p] {
				continue
			}
			seenHints[p] = true
			if !operationalEvidenceHint(p, home) {
				hints = append(hints, obj{"path_sha256": digest([]byte(p)), "excluded": true})
				continue
			}
			resolved, resolveErr := filepath.EvalSymlinks(p)
			if resolveErr != nil || !operationalEvidenceHint(resolved, home) {
				hints = append(hints, obj{"path_sha256": digest([]byte(p)), "excluded": true})
				continue
			}
			info, err := os.Lstat(p)
			h := obj{"path": p, "exists": err == nil}
			hints = append(hints, h)
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			dir := p
			if !info.IsDir() {
				dir = filepath.Dir(p)
			}
			if _, err := os.Stat(filepath.Join(dir, "report.json")); err == nil {
				roots[dir] = true
				continue
			}
			lower := strings.ToLower(dir)
			if info.IsDir() && (strings.Contains(lower, "staging") || strings.Contains(lower, "recovery-2467")) {
				walk(dir, 0)
			}
		}
	}
	private["saved_path_hints"] = hints
	if walkErr != nil {
		return nil, walkErr
	}
	summary["saved_path_hints"] = len(hints)
	summary["prior_job_documents_read"] = priorJobsRead
	hintMeta := []any{}
	for i, x := range hints {
		h := object(x)
		if h["excluded"] == true {
			hintMeta = append(hintMeta, obj{"index": i, "excluded": true})
			continue
		}
		p := str(h["path"])
		typ := "unavailable"
		if info, err := os.Stat(p); err == nil {
			if info.IsDir() {
				typ = "directory"
			} else {
				typ = "file"
			}
		}
		hintMeta = append(hintMeta, obj{"index": i, "exists": h["exists"], "type": typ, "staging_hint": strings.Contains(strings.ToLower(p), "staging"), "path_sha256": digest([]byte(p))})
	}
	private["path_hint_metadata"] = hintMeta
	paths := []string{}
	canonicalRoots := map[string]bool{}
	for p := range roots {
		p = filepath.Clean(p)
		key := strings.ToLower(p)
		if !canonicalRoots[key] {
			paths = append(paths, p)
			canonicalRoots[key] = true
		}
	}
	sort.Strings(paths)
	acquired := []any{}
	threads := 0
	for _, p := range paths {
		v, b, err := readJSON(filepath.Join(p, "report.json"))
		if err != nil {
			continue
		}
		if v["kind"] != "original-record-report" {
			continue
		}
		if err := verifyPrivateSource(p); err != nil {
			return nil, err
		}
		acquired = append(acquired, obj{"path": p, "report_sha256": digest(b), "report": v})
		threads += len(array(v["threads"]))
	}
	private["acquisitions"] = acquired
	summary["acquisition_reports"] = len(acquired)
	summary["acquisition_thread_rows"] = threads
	for _, entry := range array(db["items"]) {
		v := object(entry)
		if v["id"] != float64(2467) {
			continue
		}
		private["todo2467"] = v
		m := obj{"status": v["status"], "job_count": len(array(v["jobs"])), "dispatchable": v["dispatchable"]}
		if progress, ok := v["progress"]; ok {
			m["progress"] = progress
		}
		summary["todo2467"] = m
		// Save every available original/current spec internally for subsequent
		// correlation; do not turn its content into model-visible instructions.
		ps := []string{str(v["spec"])}
		for _, h := range array(v["spec_history"]) {
			ps = append(ps, str(object(h)["spec"]))
		}
		specs := []any{}
		for _, p := range ps {
			b, err := boundedText(p)
			if err == nil {
				specs = append(specs, obj{"path": p, "sha256": digest(b), "body": string(b)})
			}
		}
		private["todo2467_specs"] = specs
	}
	if e = privateDir(output); e != nil {
		return nil, e
	}
	if e = writeNew(filepath.Join(output, "survey.private.json"), private); e != nil {
		return nil, e
	}
	if e = writeNew(filepath.Join(output, "survey-report.json"), summary); e != nil {
		return nil, e
	}
	return summary, nil
}

func checkedSurvey(path string) (obj, error) {
	if e := verifyPrivateSource(path); e != nil {
		return nil, e
	}
	v, _, e := readJSON(filepath.Join(path, "survey.private.json"))
	if e != nil {
		return nil, e
	}
	if v["kind"] != "original-correspondence-survey" {
		return nil, errors.New("invalid_correspondence_survey")
	}
	return v, nil
}

// Location text cannot authorize arbitrary project metadata reads. Limit it to
// the supplied operational home and this original ToDo's acquisition containers.
func operationalEvidenceHint(path, home string) bool {
	p := strings.ToLower(strings.ReplaceAll(filepath.Clean(path), "\\", "/"))
	h := strings.ToLower(strings.ReplaceAll(filepath.Clean(home), "\\", "/"))
	if p == h || strings.HasPrefix(p, h+"/") {
		return true
	}
	const work = "d:/cc/work/"
	if !strings.HasPrefix(p, work) {
		return false
	}
	container := strings.Split(strings.TrimPrefix(p, work), "/")[0]
	return strings.Contains(container, "original") && regexp.MustCompile(`(?:^|[^0-9])2467(?:[^0-9]|$)`).MatchString(container)
}
