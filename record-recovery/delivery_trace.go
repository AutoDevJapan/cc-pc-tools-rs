package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var postedJobFooter = regexp.MustCompile(`^-# ジョブ ([A-Za-z0-9][A-Za-z0-9_.-]{0,159})。続きは(?:このスレッドに返信してください|自動で再開します)。$`)

func postingJob(body string) string {
	lines := strings.Split(strings.TrimRight(body, "\r\n"), "\n")
	var marker byte
	width := 0
	quoted := false
	for _, line := range lines[:len(lines)-1] {
		trim := strings.TrimSpace(line)
		if trim == "" {
			quoted = false
		}
		// A paragraph after a quoted line can be a lazy quote continuation;
		// require a blank boundary before accepting the terminal footer.
		if marker == 0 && strings.HasPrefix(trim, ">") {
			quoted = true
		}
		if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			continue
		}
		if len(trim) > 0 && (trim[0] == '`' || trim[0] == '~') {
			n := 0
			for n < len(trim) && trim[n] == trim[0] {
				n++
			}
			if n >= 3 {
				if marker == 0 {
					marker, width = trim[0], n
				} else if marker == trim[0] && n >= width && strings.TrimSpace(trim[n:]) == "" {
					marker, width = 0, 0
				}
			}
		}
	}
	last := lines[len(lines)-1]
	if marker != 0 || quoted || strings.HasPrefix(last, "    ") || strings.HasPrefix(last, "\t") {
		return ""
	}
	m := postedJobFooter.FindStringSubmatch(strings.TrimSpace(last))
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func originalDeliveryRecord(dir, tid string) (obj, []byte, error) {
	ledger, body, err := readJSON(filepath.Join(dir, "attachment-delivery-"+tid+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return readJSON(filepath.Join(dir, "attachment-delivery.json"))
	}
	return ledger, body, err
}

// A completed single-part, attachment-free delivery has a fingerprint of the
// exact posted body. The first message is the generation anchor, not the latest
// post or any reply cursor. Permission scope and original ToDo remain separate.
func exactDeliveredQuestion(ledger, question obj) bool {
	attachments, ok := ledger["attachment_hashes"].([]any)
	return numeric.MatchString(str(question["id"])) && numeric.MatchString(str(question["thread_id"])) &&
		ok && len(attachments) == 0 && ledger["complete"] == true && ledger["next"] == float64(1) &&
		ledger["thread_id"] == question["thread_id"] && ledger["first"] == question["id"] &&
		str(ledger["fingerprint"]) == digest([]byte(str(question["content"])))
}

func traceDeliveries(source, todo, jobs, output string) (obj, error) {
	if e := verifyPrivateSource(source); e != nil {
		return nil, e
	}
	prior, _, e := readJSON(filepath.Join(source, "correlation.private.json"))
	if e != nil {
		return nil, e
	}
	if prior["kind"] != "original-correspondence-correlation" {
		return nil, errors.New("original_correlation_required")
	}
	survey, e := checkedSurvey(str(prior["survey_source"]))
	if e != nil {
		return nil, e
	}
	stages, conflicts, e := acquiredHistories(survey)
	if e != nil {
		return nil, e
	}
	if conflicts != 0 {
		return nil, errors.New("acquisition_conflict_requires_resolution")
	}
	db, dbb, e := readJSON(todo)
	if e != nil {
		return nil, e
	}
	private := obj{"kind": "original-delivery-generation-trace", "original_correlation_source": source, "todo_store_sha256": digest(dbb), "rows": []any{}, "authority": "original API and posted generation evidence; no inferred ToDo or permission"}
	publicRows := []any{}
	footers := 0
	verified := 0
	keys := []string{}
	for tid := range stages {
		keys = append(keys, tid)
	}
	sort.Strings(keys)
	for _, tid := range keys {
		stage := stages[tid]
		byID := map[string]obj{}
		for _, x := range array(stage["messages"]) {
			m := object(x)
			byID[str(m["id"])] = m
		}
		for _, x := range array(stage["owner_replies"]) {
			r := object(x)
			if r["association"] != "explicit_same_thread_reference" {
				continue
			}
			q := byID[str(r["question_message"])]
			reply := byID[str(r["message_id"])]
			m := obj{"index": len(publicRows), "posting_footer_present": false, "posted_generation_verified": false, "original_todo_candidates": 0, "decision_scope_verified": false, "delivery_to_worker": false, "application_verified": false}
			row := obj{"question": q, "owner_reply": reply, "reference": r, "acquisition_stage_sha256": stage["acquisition_stage_sha256"], "acquisition_report_sha256": stage["acquisition_report_sha256"], "assessment": m}
			missing := []string{}
			jid := postingJob(str(q["content"]))
			if jid == "" {
				missing = append(missing, "posting_job_footer_missing")
			} else {
				footers++
				m["posting_footer_present"] = true
				row["posting_job_id"] = jid
				dir := filepath.Join(jobs, jid)
				job, jb, err := readJSON(filepath.Join(dir, "job.json"))
				if err != nil || job["id"] != jid {
					missing = append(missing, "posting_job_record_unavailable_or_mismatched")
				} else {
					row["job_record"] = job
					row["job_record_sha256"] = digest(jb)
					candidates := todoCandidates(db, job)
					row["original_todo_candidates"] = candidates
					m["original_todo_candidates"] = len(candidates)
					if len(candidates) == 0 {
						missing = append(missing, "original_todo_binding_missing")
					}
					if len(candidates) > 1 {
						missing = append(missing, "original_todo_binding_ambiguous")
					}
					// Match the existing writer's scoped-first, legacy-fallback rule.
					ledger, lb, err := originalDeliveryRecord(dir, tid)
					if err != nil {
						missing = append(missing, "original_delivery_record_unavailable")
					} else {
						row["delivery_record"] = ledger
						row["delivery_record_sha256"] = digest(lb)
						if exactDeliveredQuestion(ledger, q) {
							verified++
							m["posted_generation_verified"] = true
						} else {
							missing = append(missing, "delivery_generation_or_full_body_unverified")
						}
					}
				}
			}
			missing = append(missing, "actual_resources_and_permission_scope_unproven")
			m["missing"] = missing
			publicRows = append(publicRows, m)
			private["rows"] = append(array(private["rows"]), row)
		}
	}
	report := obj{"ok": true, "kind": "original-delivery-generation-report", "explicit_reference_rows": len(publicRows), "posting_footers": footers, "posted_generations_verified": verified, "rows": publicRows, "live_state_changed": false, "guard_changed": false, "messages_sent": 0, "workers_started": 0, "applications_verified": 0, "original_task_complete": false}
	if e = privateDir(output); e != nil {
		return nil, e
	}
	if e = writeNew(filepath.Join(output, "delivery-trace.private.json"), private); e != nil {
		return nil, e
	}
	if e = writeNew(filepath.Join(output, "delivery-trace-report.json"), report); e != nil {
		return nil, e
	}
	return report, nil
}
