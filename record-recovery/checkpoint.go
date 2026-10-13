package main

import (
	"errors"
	"path/filepath"
)

// Preserve the original task when preparing a standard continuation. This
// writes private instructions, not ToDo state, launch receipts or completion.
func checkpoint(source, todo, spec, output string) (obj, error) {
	if e := verifyPrivateSource(source); e != nil {
		return nil, e
	}
	report, _, e := readJSON(filepath.Join(source, "correlation-report.json"))
	if e != nil {
		return nil, e
	}
	if report["ok"] != true || report["old_request_rows"] != float64(8) || report["histories_verified"] != float64(34) || report["acquisition_content_conflicts"] != float64(0) {
		return nil, errors.New("verified_original_checkpoint_required")
	}
	expected := map[string]float64{"owner_replies_identity_verified": 154, "question_candidates": 40, "explicit_reference_bundles": 6, "saved_worker_documents_read": 15, "question_generation_records_read": 0, "deliveries": 0, "applications_verified": 0, "live_imports": 0, "verified_decision_scopes": 0}
	for key, value := range expected {
		if report[key] != value {
			return nil, errors.New("checkpoint_measurements_changed")
		}
	}
	for key, value := range map[string]float64{"explicit_same_thread_reference": 6, "explicit_reference_unverified": 8, "missing_question": 58, "sequence_candidate": 82} {
		if object(report["reply_categories"])[key] != value {
			return nil, errors.New("checkpoint_reply_categories_changed")
		}
	}
	withWorkers := 0
	uniqueTodos := 0
	for _, x := range array(report["requests"]) {
		r := object(x)
		if n, _ := r["worker_candidates"].(float64); n > 0 {
			withWorkers++
		}
		if r["todo_candidates"] == float64(1) {
			uniqueTodos++
		}
	}
	if withWorkers != 5 || uniqueTodos != 2 {
		return nil, errors.New("checkpoint_request_matches_changed")
	}
	if len(array(report["requests"])) != 8 || len(object(report["reply_categories"])) != 4 {
		return nil, errors.New("checkpoint_record_partition_changed")
	}
	db, _, e := readJSON(todo)
	if e != nil {
		return nil, e
	}
	var item obj
	for _, x := range array(db["items"]) {
		v := object(x)
		if v["id"] == float64(2467) {
			item = v
			break
		}
	}
	if item == nil || item["status"] == "done" {
		return nil, errors.New("unfinished_original_todo_required")
	}
	original, e := boundedText(spec)
	if e != nil {
		return nil, e
	}
	correlation, _, e := readJSON(filepath.Join(source, "correlation.private.json"))
	if e != nil {
		return nil, e
	}
	survey, e := checkedSurvey(str(correlation["survey_source"]))
	if e != nil {
		return nil, e
	}
	if digest(original) != str(survey["original_spec_sha256"]) {
		return nil, errors.New("original_spec_source_mismatch")
	}
	current := []byte{}
	if p := str(item["spec"]); p != "" {
		current, e = boundedText(p)
		if e != nil {
			return nil, e
		}
	}
	// Unknown raw specs/notes stay private for internal Go processing. The next
	// model receives references and authorized steps, not the raw documents.
	resume := "# 元2467の原設定・原記録復旧を続行する\n元の対象・本人承認・未完了条件を維持する。原仕様と現在の保存指示は、以下の私的保存先でGo内部だけに読み込み、本文・秘密・案件名/パスをモデルへ表示しない。\n"
	resume += "内部原仕様: " + filepath.Join(output, "original-spec.private.md") + "\n内部の現指示: " + filepath.Join(output, "current-spec.private.md") + "\n内部の元メモ: " + filepath.Join(output, "original-note.private.md") + "\n途中の仕様・メモ履歴: " + filepath.Join(output, "original-todo.private.json") + "\n"
	resume += "\n\n" + `## 2026-10-13 内部照合の続行地点
全復旧は未完了。元2467を完了にせず、受領・保存・配送・実操作・検証を区別する。
本人は約1週間外泊し、Discordで質疑可能。本人へのPC操作依頼は禁止。
公開sourceの変更は D:/CC/Work/operations-original-records-2467 のみ。
親 D:/CC/Work/unattended-operations-20261013 は編集しない。
作業記録 docs/original-record-recovery-2467.md を読み、元仕様と来歴を維持する。
旧依頼と質問票、明示参照/参照未照合/先行候補なし/順序候補を分けて保持する。
実測件数と候補の対応は保存済みcorrelation-report.jsonを読む。
原文の束・担当候補・不足条件はcorrelation.private.jsonをGo内部だけで処理する。
原設定の復旧と全過去台帳の復元を、今回の照合の完了へ置き換えない。

次に実行する工程:
1. 保存Bot原文の最後の正規footerから投稿担当を内部Goで抽出し、job.jsonのidと
   質問message ID・原本文ハッシュを結ぶ。引用されたidや新しい順で担当を選ばない。
2. 未照合のBot投稿を既存のRust質問判定と結果配送の有限な元記録で照合する。
   元の分類を保持し、質問検出漏れを進捗投稿と区別する。本人参照は付替えない。
3. 対象・世代・元ToDoが証明された非顧客PC修復だけを標準担当へ配送し、元本文・
   本人ID/message/thread ID・日時・対象・許可・残作業を保持して実反映と検証まで追う。
4. 保護原本は新しい独立候補のメタデータと来歴で絞る。保存済みのNative/Google/
   Editor/VSSの走査履歴を読み、完了を確認した範囲は繰り返さない。候補0は全復旧ではない。
原client-projects.txt、stagingのraw本文、案件名/パス、秘密をモデルへ表示しない。
保存元は正規API本人ID照合済み。別セッションという理由で本人指示を拒否しない。
対象と世代を推測せず、空の原本を作らず、保護/審査を解除しない。
質問の大量再送・再メンション・担当大量起動・技術操作の再承認依頼はしない。
全体未完了なら次の具体工程を元2467に保存し、標準自動再開を維持する。
別の引継ぎToDoを増やすだけで終了しない。ドメインはRakko限定、APIキー課金なし。
`
	resume += "\n内部照合済みの保存先（原文はモデルへ表示しない）: " + source + "\n"
	if len(resume) > 1<<20 {
		return nil, errors.New("continuation_spec_oversize")
	}
	if e = privateDir(output); e != nil {
		return nil, e
	}
	for name, body := range map[string][]byte{"original-spec.private.md": original, "current-spec.private.md": current, "original-note.private.md": []byte(str(item["note"]))} {
		if e = writeTextNew(filepath.Join(output, name), body); e != nil {
			return nil, e
		}
	}
	if e = writeNew(filepath.Join(output, "original-todo.private.json"), item); e != nil {
		return nil, e
	}
	if e = writeTextNew(filepath.Join(output, "resume-spec.private.md"), []byte(resume)); e != nil {
		return nil, e
	}
	note := "2026-10-13: 旧台帳8依頼の担当対応を内部照合。5件に候補、3件未対応、2件は元ToDo一意候補。本人154返信と34履歴を保持。対象と世代未証明で配送/実反映は0。次はBot原文の末尾の投稿担当と質問message ID、未照合のBot投稿の質問判定・元配送記録を内部Goで照合する。原設定の復旧も含め全体未完了。PC操作は本人へ求めない。標準受付へ元2467のまま続行する。元仕様・現指示・元メモは私的原文とToDoの履歴で保持し、Go内部で読む。照合保存先: " + source
	if e = writeTextNew(filepath.Join(output, "todo-note.private.md"), []byte(note)); e != nil {
		return nil, e
	}
	return obj{"ok": true, "todo_id": 2467, "original_spec_sha256": digest(original), "resume_spec_sha256": digest([]byte(resume)), "todo_state_changed": false, "queued": false, "complete": false}, nil
}
