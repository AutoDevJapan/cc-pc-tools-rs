use super::*;
use cc_job_policy::{todo_history, todo_store};

const DELIVERED: &str = "配送済みの結果\n依頼と次の作業を保持してください。";
const UPDATED: &str = "2026-10-24 22:00";

fn delivered(f: &Fixture) -> Value {
    let mut db = f.e.todo().unwrap();
    let row = &mut db["items"][0];
    row["note"] = json!(DELIVERED);
    row["updated"] = json!(UPDATED);
    row["note_history"] = json!([{"note":"以前のメモ","updated":"2026-10-23 10:00"}]);
    let old = row.clone();
    write_json(&f.e.todo_path, &db, 1).unwrap();
    old
}

fn retained(f: &Fixture, old: &Value) {
    let row = f.get(1);
    assert_ne!(row["note"], old["note"]);
    let mut expected = old["note_history"].as_array().unwrap().clone();
    expected.push(json!({"note":old["note"],"updated":old["updated"]}));
    assert_eq!(row["note_history"], json!(expected));
}

#[test]
fn requeue_preserves_delivery_for_every_reason() {
    for (reason, message) in [
        ("resource_wait", "指定モデルの利用枠を待っています"),
        ("hung", "30分間進捗がなかったため停止しました"),
        ("lost", "担当ジョブのプロセスが消失しました"),
        ("memory", "空きメモリ不足で停止しました"),
        ("failed", "担当ジョブが失敗しました"),
        (
            "no_action",
            "30分間実操作がなく報告だけが続いたため担当を復旧します",
        ),
        ("unknown", "担当ジョブが停止しました"),
        (
            "stale_review_spec",
            "レビュー中に仕様が更新されたため、旧結果を保管して新しい仕様から再開します",
        ),
    ] {
        let mut f = Fixture::new();
        f.managed(1, 1, "other", 0);
        let old = delivered(&f);
        f.e.requeue_if(&json!(1), &json!("j1"), reason, Some(&old))
            .unwrap();
        retained(&f, &old);
        let row = f.get(1);
        let expected = if reason == "stale_review_spec" {
            format!("{DELIVERED}\n{message}。")
        } else {
            format!("{message}。原因を引き継ぎ、資源の空きを確認して自動で再開します。")
        };
        assert_eq!(row["note"], expected);
        assert_eq!(row["status"], "todo");
        assert_eq!(row["scheduler"]["last_job"], "j1");
        assert!(row["scheduler"]["job"].is_null());
    }
}

#[test]
fn repeating_the_same_state_message_does_not_duplicate_history() {
    let mut f = Fixture::new();
    f.managed(1, 1, "other", 0);
    delivered(&f);
    f.e.requeue(&json!(1), &json!("j1"), "resource_wait")
        .unwrap();
    let previous = f.get(1);
    f.e.edit(&json!(1), |row| {
        row["status"] = json!("doing");
        row["scheduler"]["job"] = json!("j1");
    })
    .unwrap();
    f.e.requeue(&json!(1), &json!("j1"), "resource_wait")
        .unwrap();
    assert_eq!(f.get(1)["note_history"], previous["note_history"]);
}

#[test]
fn all_result_outcomes_and_cancellation_preserve_delivery() {
    for (text, status, note) in [
        (
            "状態: 継続中\nテストが残っています。",
            "todo",
            "作業が残っています。資源の空きを確認して自動で続けます。",
        ),
        ("状態: 完了", "done", "割り当てたジョブが完了しました。"),
        (
            "状態: 要判断\n公開先はどちらにしますか？\na. 全員へ公開（推奨）\nb. 招待者だけへ公開",
            "block",
            "ジョブの結果に判断が必要です。",
        ),
        (
            "cancel",
            "block",
            "担当ジョブが取り消されました。再開する場合は担当者が確認してください。",
        ),
    ] {
        let mut f = Fixture::new();
        f.managed(1, 1, "other", 0);
        let old = delivered(&f);
        f.e.backend.records[0]["status"] = json!(if text == "cancel" {
            "cancelled"
        } else {
            "succeeded"
        });
        f.e.backend.records[0]["last_message"] = json!(text);
        f.e.backend.ps.clear();
        f.reconcile();
        retained(&f, &old);
        assert_eq!(f.get(1)["status"], status, "{text}");
        assert_eq!(f.get(1)["note"], note);
    }
}

#[test]
fn preparation_failure_and_retry_limit_release_preserve_the_previous_note() {
    let mut f = Fixture::new();
    f.put(item(1, "other", "todo", &[]));
    // Already reserved: the failure is the next mutation, so UPDATED is the exact
    // pre-replacement timestamp rather than an earlier delivery timestamp.
    let workspace = f.temp.path().join("repo-wt/todo-1-work");
    f.e.edit(&json!(1), |row| {
        row["scheduler"] = json!({
            "worktree":workspace,"branch":"scheduler/todo-1-work",
            "key":"a".repeat(32),"phase":"preparing"
        })
    })
    .unwrap();
    let old = delivered(&f);
    f.e.backend.worktree_failure = Some(1);
    assert_eq!(f.e.dispatch(&f.cfg, &f.pri).unwrap_err().0, "OriginMissing");
    retained(&f, &old);
    assert_eq!(f.get(1)["status"], "todo");
    assert!(s(&f.get(1)["note"]).starts_with("起動の準備: "));

    let mut f = Fixture::new();
    let mut row = item(1, "other", "block", &[]);
    row["note"] = json!("ジョブが失敗。2回止まったため、オーナーに再開方法を確認します。");
    row["updated"] = json!(UPDATED);
    row["note_history"] = json!([]);
    row["scheduler"] = json!({"retry_count":2,"phase":"blocked"});
    f.put(row);
    let old = f.get(1);
    f.reconcile();
    retained(&f, &old);
    assert_eq!(f.get(1)["status"], "todo");
}

#[test]
fn cas_rejection_noop_and_shared_lock_never_create_history() {
    let mut f = Fixture::new();
    f.managed(1, 1, "other", 0);
    let old = delivered(&f);
    let mut stale = old.clone();
    stale["note"] = json!("配送前の古いsnapshot");
    f.e.requeue_if(&json!(1), &json!("j1"), "lost", Some(&stale))
        .unwrap();
    assert_eq!(f.get(1), old);
    assert_eq!(f.e.edit(&json!(1), |_| {}).unwrap(), old);
    let lock = ByteLock::acquire(&f.e.todo_path.with_file_name("todo.lock"), Duration::ZERO)
        .unwrap()
        .unwrap();
    assert_eq!(
        f.e.requeue(&json!(1), &json!("j1"), "lost").unwrap_err().0,
        "TimeoutError"
    );
    drop(lock);
    assert_eq!(f.get(1), old);
}

#[test]
fn malformed_history_fails_without_replacing_the_store() {
    for malformed in [json!(null), json!("not an array"), json!({"saved":"keep"})] {
        let mut f = Fixture::new();
        f.managed(1, 1, "other", 0);
        let mut db = f.e.todo().unwrap();
        db["items"][0]["note_history"] = malformed;
        write_json(&f.e.todo_path, &db, 1).unwrap();
        let bytes = fs::read(&f.e.todo_path).unwrap();
        assert!(f.e.requeue(&json!(1), &json!("j1"), "lost").is_err());
        assert_eq!(fs::read(&f.e.todo_path).unwrap(), bytes);
    }
}

#[test]
fn a_delivery_during_result_read_is_preserved_from_the_locked_current_row() {
    let mut f = Fixture::new();
    f.managed(1, 1, "other", 0);
    let before = delivered(&f);
    let note = f.temp.path().join("late-delivery.md");
    fs::write(&note, "結果の読取中に配送したメモ\n完了しても保持する。").unwrap();
    f.e.backend.outcome_edit = Some((
        f.e.todo_path.clone(),
        cc_todo::Command::Edit {
            id: 1,
            note_file: Some(note),
            spec: None,
            expected_note_sha256: Some(cc_job_policy::product_retry::digest(
                s(&before["note"]).as_bytes(),
            )),
            scope_file: None,
            expected_state_file: None,
        },
    ));
    f.e.backend.records[0]["status"] = json!("succeeded");
    f.e.backend.ps.clear();
    f.reconcile();
    let delivered = &f.e.backend.outcome_saved.as_ref().unwrap()["items"][0];
    retained(&f, delivered);
    assert_eq!(f.get(1)["status"], "done");
}

#[test]
fn missing_and_empty_notes_match_todo_edit_history_semantics() {
    for note in [None, Some(json!("")), Some(json!(null))] {
        let mut f = Fixture::new();
        let mut row = item(1, "other", "todo", &[]);
        remove(&mut row, "note");
        if let Some(note) = &note {
            row["note"] = note.clone();
        }
        f.put(row);
        let result =
            f.e.edit(&json!(1), |row| row["note"] = json!("状態のメモ"))
                .unwrap();
        let expected = note
            .map(|note| json!([{"note":note,"updated":null}]))
            .unwrap_or(Value::Null);
        assert_eq!(result["note_history"], expected);
        assert_eq!(result, f.get(1));
    }
}

fn nearly_full(f: &Fixture) {
    let mut db = f.e.todo().unwrap();
    db["unrelated_data"] = json!("");
    let padding = todo_store::MAX_BYTES - encode(&db, 1).len() - 8;
    db["unrelated_data"] = json!("x".repeat(padding));
    todo_store::write(&f.e.todo_path, &db).unwrap();
    assert_eq!(
        fs::metadata(&f.e.todo_path).unwrap().len(),
        (todo_store::MAX_BYTES - 8) as u64
    );
}

#[test]
fn overflow_archives_immutably_and_todo_history_reads_the_original_order() {
    let mut f = Fixture::new();
    f.managed(1, 1, "other", 0);
    let old = delivered(&f);
    let mut db = f.e.todo().unwrap();
    todo_history::archive(&f.e.todo_path, &mut db, 1, true).unwrap();
    db["items"][0]["note_history"] =
        json!([{"note":"中間の結果".repeat(1024),"updated":"2026-10-24 21:00"}]);
    todo_store::write(&f.e.todo_path, &db).unwrap();
    nearly_full(&f);
    f.e.requeue(&json!(1), &json!("j1"), "resource_wait")
        .unwrap();
    assert!(fs::metadata(&f.e.todo_path).unwrap().len() <= todo_store::MAX_BYTES as u64);
    let row = f.get(1);
    assert_eq!(row["note_history_archives"].as_array().unwrap().len(), 2);
    let mut db = f.e.todo().unwrap();
    let paths = cc_todo::Paths::at(f.e.todo_path.parent().unwrap(), f.temp.path());
    let output = cc_todo::execute(
        &mut db,
        &cc_todo::Command::History {
            id: 1,
            offset: 0,
            limit: 10,
        },
        &paths,
    )
    .unwrap();
    let page: Value = serde_json::from_str(&output[0]).unwrap();
    assert_eq!(page["total"], 3);
    assert_eq!(page["entries"][0], old["note_history"][0]);
    assert_eq!(page["entries"][1]["note"], "中間の結果".repeat(1024));
    assert_eq!(
        page["entries"][2],
        json!({"note":DELIVERED,"updated":UPDATED})
    );
    let before = fs::read_dir(f.e.todo_path.parent().unwrap().join("note-history-v1"))
        .unwrap()
        .count();
    f.e.edit(&json!(1), |_| {}).unwrap();
    assert_eq!(
        fs::read_dir(f.e.todo_path.parent().unwrap().join("note-history-v1"))
            .unwrap()
            .count(),
        before
    );
}

#[test]
fn archive_failure_keeps_the_complete_canonical_snapshot() {
    let mut f = Fixture::new();
    f.managed(1, 1, "other", 0);
    delivered(&f);
    let mut db = f.e.todo().unwrap();
    db["items"][0]["note_history"][0]["note"] = json!("saved ".repeat(1024));
    todo_store::write(&f.e.todo_path, &db).unwrap();
    nearly_full(&f);
    fs::write(
        f.e.todo_path.parent().unwrap().join("note-history-v1"),
        "occupied",
    )
    .unwrap();
    let hash = cc_job_policy::product_retry::digest(&fs::read(&f.e.todo_path).unwrap());
    assert_eq!(
        f.e.requeue(&json!(1), &json!("j1"), "resource_wait")
            .unwrap_err()
            .0,
        "todo_history_archive_failed"
    );
    assert_eq!(
        cc_job_policy::product_retry::digest(&fs::read(&f.e.todo_path).unwrap()),
        hash
    );
    assert_eq!(f.get(1)["note"], DELIVERED);
}

#[test]
fn overflow_can_reclaim_another_history_without_changing_its_logical_row() {
    let mut f = Fixture::new();
    f.managed(1, 1, "other", 0);
    let old = delivered(&f);
    let mut other = item(2, "other", "wait", &[]);
    other["wait_on"] = json!([1]);
    other["note_history"] = json!([{"note":"保存した古い結果".repeat(1024),"updated":UPDATED}]);
    f.put(other);
    let old_other = f.get(2);
    nearly_full(&f);
    f.e.requeue(&json!(1), &json!("j1"), "resource_wait")
        .unwrap();
    retained(&f, &old);
    let other = f.get(2);
    assert_eq!(other["note_history_archives"].as_array().unwrap().len(), 1);
    assert_eq!(
        todo_history::materialized(&f.e.todo_path, &other).unwrap(),
        old_other
    );
    assert!(fs::metadata(&f.e.todo_path).unwrap().len() <= todo_store::MAX_BYTES as u64);
}
