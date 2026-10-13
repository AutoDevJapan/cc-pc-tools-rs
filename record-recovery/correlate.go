package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const recoveryGuild = "1552944707464597524"
const recoveryBot = "1554269962229649478"
const recoveryOwner = "730822776147148835"

// Acquisition receipts and message hashes are verified before any text is used.
// Multiple acquisitions with different content are a conflict, not a reason to
// silently choose the newest body as the historical original.
func acquiredHistories(survey obj) (map[string]obj, int, error) {
	stages := map[string]obj{}
	conflicts := 0
	for _, entry := range array(survey["acquisitions"]) {
		source := object(entry)
		if e := verifyPrivateSource(str(source["path"])); e != nil {
			return nil, 0, e
		}
		report, b, e := readJSON(filepath.Join(str(source["path"]), "report.json"))
		if e != nil {
			return nil, 0, e
		}
		if digest(b) != str(source["report_sha256"]) || report["kind"] != "original-record-report" || report["guild_id"] != recoveryGuild || report["bot_id"] != recoveryBot || report["owner_id"] != recoveryOwner {
			return nil, 0, errors.New("acquisition_receipt_mismatch")
		}
		for _, x := range array(report["threads"]) {
			r := object(x)
			tid := str(r["thread_id"])
			if !numeric.MatchString(tid) || r["status"] != "staged" || r["history_complete"] != true {
				return nil, 0, errors.New("acquisition_incomplete_or_invalid")
			}
			stage, raw, e := readJSON(filepath.Join(str(source["path"]), tid+".private.json"))
			if e != nil {
				return nil, 0, e
			}
			if digest(raw) != str(r["stage_sha256"]) || stage["source"] != "authenticated Discord API GET" || stage["thread_id"] != tid || stage["guild_id"] != recoveryGuild || stage["bot_id"] != recoveryBot || stage["owner_id"] != recoveryOwner || stage["history_complete"] != true || (stage["parent_id"] != requestForum && stage["parent_id"] != "1554276307997692004") {
				return nil, 0, errors.New("acquisition_stage_mismatch")
			}
			messages := []obj{}
			seen := map[string]bool{}
			for _, x := range array(stage["messages"]) {
				m := object(x)
				id := str(m["id"])
				aid := str(m["author_id"])
				if !numeric.MatchString(id) || seen[id] || m["thread_id"] != tid || (aid != recoveryBot && aid != recoveryOwner) || digest([]byte(str(m["content"]))) != str(m["content_sha256"]) {
					return nil, 0, errors.New("acquisition_message_mismatch")
				}
				seen[id] = true
				messages = append(messages, obj{"id": id, "channel_id": tid, "content": m["content"], "author": obj{"id": aid, "bot": aid == recoveryBot}, "timestamp": m["timestamp"], "edited_timestamp": m["edited_timestamp"], "message_reference": m["message_reference"], "attachments": m["attachments"], "embeds": m["embeds"], "components": m["components"]})
			}
			indexed, _ := stageHistory(tid, recoveryBot, recoveryOwner, messages, true)
			indexed["guild_id"] = recoveryGuild
			indexed["parent_id"] = stage["parent_id"]
			indexed["acquisition_report_sha256"] = digest(b)
			indexed["acquisition_stage_sha256"] = digest(raw)
			if old := stages[tid]; old != nil {
				if messageFingerprint(old) != messageFingerprint(indexed) {
					conflicts++
					old["content_conflict"] = true
				}
				continue
			}
			stages[tid] = indexed
		}
	}
	return stages, conflicts, nil
}

func messageFingerprint(stage obj) string {
	parts := []string{}
	for _, x := range array(stage["messages"]) {
		m := object(x)
		b, _ := json.Marshal(m)
		parts = append(parts, digest(b))
	}
	sort.Strings(parts)
	return digest([]byte(str(stage["parent_id"]) + "\n" + strings.Join(parts, "\n")))
}

// Explicit request ownership and question routes stay separate. A quoted thread
// in a prompt, or a similar title, is deliberately not an ownership proof.
func jobThreadMatches(job, association obj, tid string) (bool, bool) {
	request := job["discord_thread_id"] == tid
	if job["discord_thread_ids_source"] != "prompt" {
		for _, x := range array(job["discord_thread_ids"]) {
			if x == tid {
				request = true
			}
		}
	}
	if association["source"] != "prompt" {
		for _, x := range array(association["thread_ids"]) {
			if x == tid {
				request = true
			}
		}
	}
	route := object(object(association["question_routes"])[tid])
	question := false
	for _, x := range array(association["question_thread_ids"]) {
		if x == tid && route["question_thread_id"] == tid && route["posting_job"] == job["id"] {
			question = true
		}
	}
	return request, question
}

func todoCandidates(db obj, job obj) []obj {
	ids := map[float64]bool{}
	if n, ok := job["todo_id"].(float64); ok && n > 0 {
		ids[n] = true
	}
	for _, x := range array(job["related_todo_ids"]) {
		if n, ok := x.(float64); ok && n > 0 {
			ids[n] = true
		}
	}
	for _, x := range array(db["items"]) {
		item := object(x)
		for _, j := range array(item["jobs"]) {
			if j == job["id"] {
				if n, ok := item["id"].(float64); ok {
					ids[n] = true
				}
			}
		}
	}
	rows := []obj{}
	for _, x := range array(db["items"]) {
		item := object(x)
		n, _ := item["id"].(float64)
		if ids[n] {
			rows = append(rows, item)
		}
	}
	return rows
}

// Exact body/ID occurrences in saved worker documents are candidates only.
// A repair worker may quote evidence about another job; quoting does not make
// it the original owner of that question or permit applying an answer.
func savedWorkerDocuments(jobs string, candidates []obj, question obj, reply obj) ([]any, int) {
	rows := []any{}
	matches := 0
	for _, candidate := range candidates {
		job := object(candidate["job"])
		jid := str(job["id"])
		for _, name := range []string{"prompt.md", "execution-prompt.md", "context-continue.md", "result.txt"} {
			b, e := boundedText(filepath.Join(jobs, jid, name))
			if e != nil {
				continue
			}
			text := string(b)
			body := str(question["content"])
			bodyMatch := body != "" && strings.Contains(text, body)
			idMatch := false
			for _, link := range discordLinks(text) {
				if link[1] == recoveryGuild && link[2] == str(question["thread_id"]) && link[3] == str(question["id"]) {
					idMatch = true
				}
			}
			if bodyMatch && idMatch {
				matches++
			}
			rows = append(rows, obj{"job_id": jid, "kind": name, "sha256": digest(b), "original_question_body_exact": bodyMatch, "original_question_message_link_exact": idMatch, "reply_id_present": strings.Contains(text, str(reply["id"])), "body": text, "authority": "document occurrence only; no generation or scope approval"})
		}
	}
	return rows, matches
}

func savedQuestionRecords(home string, stages map[string]obj) (map[string][]obj, int, error) {
	matched := map[string][]obj{}
	entries, e := os.ReadDir(filepath.Join(home, "question-progress"))
	if os.IsNotExist(e) {
		return matched, 0, nil
	}
	if e != nil {
		return nil, 0, errors.New("question_generation_store_unavailable")
	}
	if len(entries) > 1000 {
		return nil, 0, errors.New("question_generation_store_limit_exceeded")
	}
	read := 0
	for _, en := range entries {
		if en.IsDir() || en.Type()&os.ModeSymlink != 0 || filepath.Ext(en.Name()) != ".json" {
			continue
		}
		v, b, e := readJSON(filepath.Join(home, "question-progress", en.Name()))
		if e != nil {
			return nil, read, e
		}
		read++
		tid := str(v["question_thread_id"])
		if stages[tid] == nil {
			continue
		}
		if v["guild"] != recoveryGuild || v["parent"] != "1554276307997692004" || v["sender"] != recoveryBot || v["complete"] != true {
			continue
		}
		qid := str(object(v["question"])["starter"])
		parts := array(v["text_parts"])
		// A partial multi-message question needs every original part. Never
		// verify it using just the first fragment.
		if len(parts) != 1 {
			continue
		}
		for _, x := range array(stages[tid]["messages"]) {
			m := object(x)
			if m["id"] == qid && m["author_id"] == recoveryBot && m["content"] == parts[0] {
				matched[tid+":"+qid] = append(matched[tid+":"+qid], obj{"record": v, "record_sha256": digest(b)})
			}
		}
	}
	return matched, read, nil
}

func correlate(source, home, todo, jobs, output string) (obj, error) {
	survey, e := checkedSurvey(source)
	if e != nil {
		return nil, e
	}
	stages, conflicts, e := acquiredHistories(survey)
	if e != nil {
		return nil, e
	}
	if len(stages) == 0 || len(stages) > 100 {
		return nil, errors.New("bounded_acquired_histories_required")
	}
	generationRecords, generationReads, e := savedQuestionRecords(home, stages)
	if e != nil {
		return nil, e
	}
	db, dbb, e := readJSON(todo)
	if e != nil {
		return nil, e
	}
	entries, e := os.ReadDir(jobs)
	if e != nil {
		return nil, errors.New("job_metadata_store_unavailable")
	}
	if len(entries) > 20000 {
		return nil, errors.New("job_metadata_store_limit_exceeded")
	}
	jobPattern := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,159}$`)
	matched := map[string][]obj{}
	jobReads := 0
	jobUnavailable := 0
	jobInvalid := 0
	jobIdentityMismatch := 0
	associationReads := 0
	invalidAssociations := 0
	for _, en := range entries {
		if !en.IsDir() || en.Type()&os.ModeSymlink != 0 || !jobPattern.MatchString(en.Name()) {
			continue
		}
		job, jb, err := readJSON(filepath.Join(jobs, en.Name(), "job.json"))
		if err != nil {
			if err.Error() == "source_unavailable" {
				jobUnavailable++
			} else {
				jobInvalid++
			}
			continue
		}
		jobReads++
		if job["id"] != en.Name() {
			jobIdentityMismatch++
			continue
		}
		a, ab, err := readJSON(filepath.Join(jobs, en.Name(), "discord-threads.json"))
		if err != nil && err.Error() != "source_unavailable" {
			invalidAssociations++
			continue
		}
		if err == nil {
			associationReads++
		}
		for tid := range stages {
			req, q := jobThreadMatches(job, a, tid)
			if !req && !q {
				continue
			}
			matched[tid] = append(matched[tid], obj{"job": job, "job_record_sha256": digest(jb), "association": a, "association_sha256": digest(ab), "request_owner": req, "question_route": q, "todo_candidates": todoCandidates(db, job)})
			if job["todo_id"] == float64(2467) {
				matched[tid][len(matched[tid])-1]["role"] = "recovery_worker_candidate"
			} else {
				matched[tid][len(matched[tid])-1]["role"] = "worker_candidate"
			}
		}
	}
	private := obj{"kind": "original-correspondence-correlation", "created_at": time.Now().UTC().Format(time.RFC3339), "survey_source": source, "todo_store_sha256": digest(dbb), "acquired_histories": stages, "thread_jobs": matched, "request_bundles": []any{}, "question_record_bundles": []any{}, "reply_bundles": []any{}}
	private["question_generation_records"] = generationRecords
	report := obj{"ok": true, "histories_verified": len(stages), "acquisition_content_conflicts": conflicts, "job_metadata_read": jobReads, "association_records_read": associationReads, "invalid_association_records": invalidAssociations, "old_request_rows": 0, "old_question_rows": 0, "live_state_changed": false, "guard_changed": false, "messages_sent": 0, "workers_started": 0, "live_imports": 0, "verified_decision_scopes": 0, "deliveries": 0, "applications_verified": 0}
	report["question_generation_records_read"] = generationReads
	report["job_metadata_unavailable"] = jobUnavailable
	report["job_metadata_invalid"] = jobInvalid
	report["job_identity_mismatches"] = jobIdentityMismatch
	requestMeta := []any{}
	surveyRows := array(survey["records"])
	if surveyRows == nil {
		surveyRows = array(survey["questions"])
	}
	requestRows := 0
	questionRows := 0
	for i, x := range surveyRows {
		r := object(x)
		q := object(r["question"])
		kind := str(r["record_kind"])
		if kind == "" {
			if str(q["request_thread_key"]) != "" {
				kind = "request"
			} else {
				kind = "question"
			}
		}
		if kind != "request" && kind != "question" {
			return nil, errors.New("invalid_original_record_kind")
		}
		if kind == "request" {
			requestRows++
		} else {
			questionRows++
		}
		tid := str(q["request_thread_key"])
		if tid == "" {
			tid = str(q["thread_id"])
		}
		js := matched[tid]
		recoveryWorkers := 0
		for _, j := range js {
			if j["role"] == "recovery_worker_candidate" {
				recoveryWorkers++
			}
		}
		todos := map[float64]bool{}
		for _, j := range js {
			for _, t := range array(j["todo_candidates"]) {
				n, _ := object(t)["id"].(float64)
				if n > 0 {
					todos[n] = true
				}
			}
		}
		missing := []string{}
		if stages[tid] == nil {
			missing = append(missing, "authenticated_history_missing")
		}
		if stages[tid] != nil && stages[tid]["content_conflict"] == true {
			missing = append(missing, "acquisition_conflict")
		}
		if len(js) == 0 {
			missing = append(missing, "explicit_worker_association_missing")
		}
		if len(js) > 1 {
			missing = append(missing, "multiple_worker_generations")
		}
		if len(todos) == 0 {
			missing = append(missing, "original_todo_missing")
		}
		if len(todos) > 1 {
			missing = append(missing, "original_todo_ambiguous")
		}
		m := obj{"index": i, "record_kind": kind, "history_verified": stages[tid] != nil && stages[tid]["content_conflict"] != true, "worker_candidates": len(js), "recovery_worker_candidates": recoveryWorkers, "todo_candidates": len(todos), "missing": missing}
		bundle := obj{"index": i, "original_record": q, "thread_id": tid, "workers": js, "assessment": m}
		if kind == "request" {
			requestMeta = append(requestMeta, m)
			private["request_bundles"] = append(array(private["request_bundles"]), bundle)
		} else {
			private["question_record_bundles"] = append(array(private["question_record_bundles"]), bundle)
		}
	}
	report["requests"] = requestMeta
	report["old_request_rows"] = requestRows
	report["old_question_rows"] = questionRows
	categories := map[string]int{}
	explicitBundles := 0
	explicitWithWorker := 0
	explicitWithTodo := 0
	generationMatches := 0
	documentMatches := 0
	documentsRead := 0
	replyMeta := []any{}
	keys := []string{}
	for tid := range stages {
		keys = append(keys, tid)
	}
	sort.Strings(keys)
	questionCount := 0
	ownerCount := 0
	unverifiedReasons := map[string]int{}
	for _, tid := range keys {
		stage := stages[tid]
		questionCount += len(array(stage["questions"]))
		byID := map[string]obj{}
		for _, x := range array(stage["messages"]) {
			m := object(x)
			byID[str(m["id"])] = m
		}
		for _, x := range array(stage["owner_replies"]) {
			r := object(x)
			ownerCount++
			cat := str(r["association"])
			categories[cat]++
			if cat == "explicit_reference_unverified" {
				m := byID[str(r["message_id"])]
				ref := object(m["message_reference"])
				targetStage := stages[str(ref["channel_id"])]
				target := obj{}
				if targetStage != nil {
					for _, x := range array(targetStage["messages"]) {
						v := object(x)
						if v["id"] == ref["message_id"] {
							target = v
							break
						}
					}
				}
				reason := "reference_target_not_in_acquired_identity_messages"
				if len(target) > 0 {
					switch {
					case target["author_id"] == recoveryOwner:
						reason = "reference_targets_owner_message"
					case !less(str(target["id"]), str(m["id"])):
						reason = "reference_target_not_earlier"
					case !question(str(target["content"])):
						reason = "reference_bot_message_not_detected_as_question"
					case ref["channel_id"] != tid:
						reason = "reference_question_in_other_thread"
					default:
						reason = "reference_requires_further_validation"
					}
				}
				unverifiedReasons[reason]++
			}
			// Sequence and unverified references remain separate candidates.
			if cat != "explicit_same_thread_reference" || stage["content_conflict"] == true {
				continue
			}
			explicitBundles++
			js := matched[tid]
			todoIDs := map[float64]bool{}
			if len(js) > 0 {
				explicitWithWorker++
			}
			for _, j := range js {
				for _, t := range array(j["todo_candidates"]) {
					n, _ := object(t)["id"].(float64)
					if n > 0 {
						todoIDs[n] = true
					}
				}
			}
			if len(todoIDs) == 1 {
				explicitWithTodo++
			}
			bundle := obj{"thread_id": tid, "question": byID[str(r["question_message"])], "owner_reply": byID[str(r["message_id"])], "original_reference": r, "worker_candidates": js, "question_generation_reference_verified": true, "decision_scope_verified": false, "ready_for_delivery": false, "missing": []string{"actual_decision_scope_unproven", "worker_question_generation_unproven"}, "acquisition_report_sha256": stage["acquisition_report_sha256"], "acquisition_stage_sha256": stage["acquisition_stage_sha256"]}
			gs := generationRecords[tid+":"+str(r["question_message"])]
			bundle["generation_candidates"] = gs
			if len(gs) > 0 {
				generationMatches++
			}
			documents, bodyAndID := savedWorkerDocuments(jobs, js, object(bundle["question"]), object(bundle["owner_reply"]))
			bundle["saved_worker_documents"] = documents
			documentsRead += len(documents)
			documentMatches += bodyAndID
			replyMeta = append(replyMeta, obj{"index": len(replyMeta), "worker_candidates": len(js), "todo_candidates": len(todoIDs), "exact_question_record_candidates": len(gs), "saved_worker_documents_read": len(documents), "exact_body_and_message_link_occurrences": bodyAndID, "missing": bundle["missing"]})
			private["reply_bundles"] = append(array(private["reply_bundles"]), bundle)
		}
	}
	report["question_candidates"] = questionCount
	report["owner_replies_identity_verified"] = ownerCount
	report["reply_categories"] = categories
	report["unverified_reference_reasons"] = unverifiedReasons
	report["explicit_reference_bundles"] = explicitBundles
	report["explicit_bundles_with_worker_candidates"] = explicitWithWorker
	report["explicit_bundles_with_unique_todo_candidate"] = explicitWithTodo
	report["explicit_bundles_with_exact_question_records"] = generationMatches
	report["explicit_bundles"] = replyMeta
	report["saved_worker_documents_read"] = documentsRead
	report["exact_body_and_message_link_occurrences"] = documentMatches
	if e = privateDir(output); e != nil {
		return nil, e
	}
	if e = writeNew(filepath.Join(output, "correlation.private.json"), private); e != nil {
		return nil, e
	}
	if e = writeNew(filepath.Join(output, "correlation-report.json"), report); e != nil {
		return nil, e
	}
	return report, nil
}
