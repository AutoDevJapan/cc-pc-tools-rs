mod common;
use cc_ask::{commands::*, core::*, decision_identity};
use common::Harness;
use serde_json::{json, Value};

fn identity(target: &str) -> Value {
    json!({"todo_id":2380,"resources":[{"kind":"path","id":target}],"decision":"archive-and-delete",
        "scope":{"archive":"drive-folder","verification":"size-and-sha256","retain":"running-ollama","spec_revision":1}})
}
fn args(h: &Harness, title: &str, target: &str) -> AskArgs {
    let path = h.root.path().join("decision.json");
    atomic_json(&path, &identity(target), 2).unwrap();
    let body = h.root.path().join("question.txt");
    std::fs::write(
        &body,
        "別保存モデルを保管して削除してよいですか？\na 照合してから削除\nb 残す",
    )
    .unwrap();
    AskArgs {
        title: title.into(),
        body_file: body,
        decision_file: Some(path),
        cwd: Some("D:/cleanup".into()),
        owner_only: Some(cc_ask::ask_gate::OwnerOnly::Irreversible),
        agent: "Codex".into(),
        ..Default::default()
    }
}
fn creates(h: &Harness) -> usize {
    h.fake
        .borrow()
        .http_calls
        .iter()
        .filter(|c| c["method"] == "POST")
        .count()
}

#[test]
fn title_paraphrase_and_another_worker_reuse_an_unanswered_decision_without_posts() {
    let mut h = Harness::new();
    let a = args(&h, "元ファイルの扱い", "D:/models/unused");
    let first = cmd_ask(&mut h.ctx, &a).unwrap();
    let before = creates(&h);
    let mut b = a.clone();
    b.title = "別コピーの削除判断".into();
    b.agent = "Claude".into();
    b.cwd = Some("D:/other-worker".into());
    std::fs::write(
        &b.body_file,
        "別の場所にあるコピーを整理してよいですか？\na 保管・照合して削除\nb 保持",
    )
    .unwrap();
    let second = cmd_ask(&mut h.ctx, &b).unwrap();
    assert_eq!(first["thread_id"], second["thread_id"]);
    assert_eq!(second["suppressed"], "same_decision");
    assert_eq!(creates(&h), before);
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
}
#[test]
fn closed_consumed_owner_answer_is_returned_with_original_scope_and_sender() {
    let mut h = Harness::new();
    let mut a = args(&h, "保管と削除", "D:/models/unused");
    let first = cmd_ask(&mut h.ctx, &a).unwrap();
    let tid = s(&first["thread_id"]);
    let reply = h.reply("a", tid);
    let mut state = h.state();
    state["questions"][0]["open"] = json!(false);
    state["questions"][0]["seen_reply"] = json!(reply);
    state["answer_receipts"][&reply] = json!({"handled_by":"old-worker"});
    h.save(&state);
    h.fake
        .borrow_mut()
        .messages
        .get_mut(tid)
        .unwrap()
        .push(common::message("9999", "b", "FOREIGN"));
    let before = creates(&h);
    a.title = "題名変更".into();
    std::fs::write(&a.body_file, "逆順の選択肢\na 残す\nb 削除").unwrap();
    let result = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_eq!(result["suppressed"], "already_answered");
    assert_eq!(result["latest_owner_reply"]["id"], reply);
    assert_eq!(result["latest_owner_reply"]["author_id"], "OWN");
    assert_eq!(result["latest_owner_reply"]["thread_id"], tid);
    assert!(s(&result["question_body"]).contains("a 照合してから削除"));
    assert_eq!(creates(&h), before);
    assert_eq!(h.state(), state);
    let record = std::fs::read_dir(h.ctx.here.join("question-progress"))
        .unwrap()
        .map(|e| read_json(&e.unwrap().path()).unwrap())
        .next()
        .unwrap();
    assert_eq!(record["owner_answers"][0]["content"], "a");
}
#[test]
fn genuinely_new_target_scope_and_specification_never_inherit_old_a() {
    let mut h = Harness::new();
    let a = args(&h, "削除確認", "D:/models/one");
    let first = cmd_ask(&mut h.ctx, &a).unwrap();
    h.reply("a", s(&first["thread_id"]));
    for mode in ["target", "scope", "spec"] {
        let a = args(&h, "同じ題名", "D:/models/one");
        let mut value = identity("D:/models/one");
        match mode {
            "target" => value["resources"][0]["id"] = json!("D:/models/two"),
            "scope" => value["scope"]["retain"] = json!("also-delete-running-model"),
            _ => value["scope"]["spec_revision"] = json!(2),
        };
        atomic_json(a.decision_file.as_ref().unwrap(), &value, 2).unwrap();
        let next = cmd_ask(&mut h.ctx, &a).unwrap();
        assert_ne!(first["thread_id"], next["thread_id"]);
        assert!(next["latest_owner_reply"].is_null());
    }
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 4);
}
#[test]
fn missing_scope_after_paraphrase_never_resends_or_applies_an_old_answer() {
    let mut h = Harness::new();
    let a = args(&h, "削除確認", "D:/models/one");
    let first = cmd_ask(&mut h.ctx, &a).unwrap();
    h.reply("a", s(&first["thread_id"]));
    let before = creates(&h);
    let mut b = a;
    b.decision_file = None;
    b.title = "審査で拒否されたので再確認".into();
    std::fs::write(&b.body_file, "別モデルも削除しますか？\na 削除\nb 保持").unwrap();
    let result = cmd_ask(&mut h.ctx, &b).unwrap();
    assert_eq!(result["suppressed"], "decision_scope_required");
    assert!(result["latest_owner_reply"].is_null());
    assert_eq!(creates(&h), before);
}
#[test]
fn different_owner_cannot_consume_a_previous_owners_approval() {
    let mut h = Harness::new();
    let a = args(&h, "削除確認", "D:/models/one");
    let first = cmd_ask(&mut h.ctx, &a).unwrap();
    h.reply("a", s(&first["thread_id"]));
    h.ctx.cfg["owner_user_id"] = json!("OTHER");
    let next = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_ne!(first["thread_id"], next["thread_id"]);
    assert!(next["latest_owner_reply"].is_null());
}
#[test]
fn canonical_scope_order_and_path_spelling_keep_one_permission() {
    let h = Harness::new();
    let a = identity("D:/models/one");
    let mut b = identity("D:/models/a/../one/");
    b["scope"] = json!({"spec_revision":1,"retain":"running-ollama","verification":"size-and-sha256","archive":"drive-folder"});
    assert_eq!(
        decision_identity::normalize(&h.ctx, &a).unwrap(),
        decision_identity::normalize(&h.ctx, &b).unwrap()
    );
}
#[test]
fn decision_metadata_is_removed_from_owner_visible_text() {
    let text = format!(
        "質問\n<!-- cc-decision {} -->\na 削除\nb 保持",
        identity("D:/models/one")
    );
    let (body, scope) = decision_identity::extract(&text).unwrap();
    assert_eq!(body, "質問\n\na 削除\nb 保持");
    assert_eq!(scope.unwrap(), identity("D:/models/one"));
    assert!(decision_identity::extract("<!-- cc-decision invalid -->").is_err());
}
#[test]
fn original_todo_is_recovered_from_its_recorded_job_not_its_title() {
    let h = Harness::new();
    atomic_json(
        &h.ctx.todo,
        &json!({"items":[{"id":2380,"jobs":["old-worker"]}]}),
        2,
    )
    .unwrap();
    assert_eq!(
        decision_identity::todo(
            &h.ctx,
            &json!({"posting_job":"old-worker","title":"anything"})
        )
        .unwrap(),
        2380
    );
}
#[test]
fn two_concurrent_workers_with_different_titles_create_one_pair() {
    use std::sync::{Arc, Barrier};
    let shared = Harness::new();
    let barrier = Arc::new(Barrier::new(3));
    let workers: Vec<_> = (0..2)
        .map(|i| {
            let barrier = barrier.clone();
            let here = shared.ctx.here.clone();
            std::thread::spawn(move || {
                let mut h = Harness::new();
                h.ctx.here = here;
                let a = args(&h, &format!("別の題名{i}"), "D:/models/one");
                barrier.wait();
                let result = cmd_ask(&mut h.ctx, &a).unwrap();
                (creates(&h), result["thread_id"].clone())
            })
        })
        .collect();
    barrier.wait();
    let results: Vec<_> = workers.into_iter().map(|w| w.join().unwrap()).collect();
    assert_eq!(results.iter().map(|r| r.0).sum::<usize>(), 2);
    assert_eq!(results[0].1, results[1].1);
    assert_eq!(shared.state()["questions"].as_array().unwrap().len(), 1);
}

struct FailAfterPost {
    inner: common::SharedFake,
    root: std::path::PathBuf,
    fired: bool,
}
impl Effects for FailAfterPost {
    fn http(&mut self, m: &str, u: &str, b: Option<&Value>, d: bool) -> Result<Value> {
        let value = self.inner.http(m, u, b, d)?;
        if !self.fired && m == "POST" && u.ends_with("/channels/T/threads") {
            self.fired = true;
            std::fs::create_dir(self.root.join("state.json")).unwrap();
        }
        Ok(value)
    }
    fn run(&mut self, a: &[String], t: u64) -> Result<Output> {
        self.inner.run(a, t)
    }
}
#[test]
fn failed_registration_then_paraphrased_retry_keeps_the_original_question_and_one_mention() {
    let mut h = Harness::new();
    let a = args(&h, "最初の題名", "D:/models/one");
    h.ctx.effects = Box::new(FailAfterPost {
        inner: common::SharedFake(h.fake.clone()),
        root: h.ctx.here.clone(),
        fired: false,
    });
    assert!(cmd_ask(&mut h.ctx, &a).is_err());
    assert_eq!(creates(&h), 1);
    std::fs::remove_dir(h.ctx.here.join("state.json")).unwrap();
    let mut b = a;
    b.title = "言い換えた題名".into();
    std::fs::write(&b.body_file, "別の言い方\na 削除\nb 保持").unwrap();
    let result = cmd_ask(&mut h.ctx, &b).unwrap();
    assert_eq!(creates(&h), 2);
    assert!(s(&h.state()["questions"][0]["question_body"]).contains("a 照合してから削除"));
    assert_eq!(result["thread_id"], h.state()["questions"][0]["thread_id"]);
}
#[test]
fn unavailable_reply_history_is_a_failure_and_never_a_reason_to_resend() {
    let mut h = Harness::new();
    let a = args(&h, "削除確認", "D:/models/one");
    let first = cmd_ask(&mut h.ctx, &a).unwrap();
    let before = creates(&h);
    h.fake.borrow_mut().http_errors.insert(
        format!(
            "GET /channels/{}/messages?limit=100",
            s(&first["thread_id"])
        ),
        1,
    );
    assert!(cmd_ask(&mut h.ctx, &a).is_err());
    assert_eq!(creates(&h), before);
    assert_eq!(
        cmd_ask(&mut h.ctx, &a).unwrap()["thread_id"],
        first["thread_id"]
    );
    assert_eq!(creates(&h), before);
}
struct FailSnapshotSave {
    inner: common::SharedFake,
    file: std::path::PathBuf,
    fired: bool,
}
impl Effects for FailSnapshotSave {
    fn http(&mut self, m: &str, u: &str, b: Option<&Value>, d: bool) -> Result<Value> {
        let value = self.inner.http(m, u, b, d)?;
        if !self.fired && m == "GET" && u.contains("/messages?limit=100") {
            self.fired = true;
            std::fs::rename(&self.file, self.file.with_extension("before")).unwrap();
            std::fs::create_dir(&self.file).unwrap();
        }
        Ok(value)
    }
    fn run(&mut self, a: &[String], t: u64) -> Result<Output> {
        self.inner.run(a, t)
    }
}
#[test]
fn answer_snapshot_save_failure_does_not_send_and_retry_returns_same_answer() {
    let mut h = Harness::new();
    let a = args(&h, "削除確認", "D:/models/one");
    let first = cmd_ask(&mut h.ctx, &a).unwrap();
    let reply = h.reply("a", s(&first["thread_id"]));
    let before = creates(&h);
    let file = std::fs::read_dir(h.ctx.here.join("question-progress"))
        .unwrap()
        .next()
        .unwrap()
        .unwrap()
        .path();
    h.ctx.effects = Box::new(FailSnapshotSave {
        inner: common::SharedFake(h.fake.clone()),
        file: file.clone(),
        fired: false,
    });
    assert!(cmd_ask(&mut h.ctx, &a).is_err());
    assert_eq!(creates(&h), before);
    std::fs::remove_dir(&file).unwrap();
    std::fs::rename(file.with_extension("before"), &file).unwrap();
    let result = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_eq!(result["latest_owner_reply"]["id"], reply);
    assert_eq!(creates(&h), before);
}
#[test]
fn changed_option_letter_order_cannot_replace_the_original_answer_meaning() {
    let mut h = Harness::new();
    let a = args(&h, "削除確認", "D:/models/one");
    let first = cmd_ask(&mut h.ctx, &a).unwrap();
    h.reply("a", s(&first["thread_id"]));
    let text = format!(
        "状態: 要判断\n逆順\na 保持\nb 削除\n<!-- cc-decision {} -->",
        identity("D:/models/one")
    );
    let result =
        cc_ask::question_progress::ask_result(&mut h.ctx, s(&first["thread_id"]), &text, "job2")
            .unwrap();
    assert_eq!(result["latest_owner_reply"]["content"], "a");
    assert!(s(&result["question_body"]).contains("a 照合してから削除"));
    assert_eq!(creates(&h), 2);
}
#[test]
fn decision_actions_cannot_be_replaced_with_new_titles() {
    let h = Harness::new();
    let mut value = identity("D:/models/one");
    value["decision"] = json!("題名を変えた削除の確認");
    assert!(decision_identity::normalize(&h.ctx, &value).is_err());
}
#[test]
fn explicit_original_todo_cannot_escape_its_recorded_worker_lineage() {
    let h = Harness::new();
    atomic_json(
        &h.ctx.todo,
        &json!({"items":[{"id":2380,"jobs":["worker"]}]}),
        2,
    )
    .unwrap();
    assert!(
        decision_identity::todo(&h.ctx, &json!({"posting_job":"worker","todo_id":2345})).is_err()
    );
}

#[test]
fn reviewed_four_duplicates_keep_all_replies_and_return_latest_owner_identity_without_posts() {
    let mut h = Harness::new();
    let mut threads = Vec::new();
    let mut latest = String::new();
    for i in 0..4 {
        let result=cc_ask::question_progress::ask(&mut h.ctx,&json!({"title":format!("original{i}"),"cwd":format!("D:/worker{i}"),"project":format!("Product{i}")}),"",
            "対象を保管・照合してから削除しますか？\na 削除\nb 保持",&[]).unwrap();
        let tid = s(&result["thread_id"]).to_owned();
        h.fake.borrow_mut().threads.get_mut(&tid).unwrap()["guild_id"] = json!("G");
        latest = h.reply(if i == 2 { "a 3回目です" } else { "a" }, &tid);
        threads.push(tid);
    }
    let mut state = h.state();
    for q in state["questions"].as_array_mut().unwrap() {
        q["open"] = json!(false)
    }
    h.save(&state);
    let before = creates(&h);
    let result=decision_identity::reconcile(&mut h.ctx,&json!({"identity":identity("D:/models/one"),"threads":threads,"evidence":"each exact original body and common scope reviewed"})).unwrap();
    assert_eq!(result["owner_answers"].as_array().unwrap().len(), 4);
    assert_eq!(result["latest_owner_reply"]["id"], latest);
    assert_eq!(result["latest_owner_reply"]["author_id"], "OWN");
    assert_eq!(creates(&h), before);
    assert_eq!(h.state(), state);
    let repeated = decision_identity::lookup(&mut h.ctx, &identity("D:/models/one")).unwrap();
    assert_eq!(repeated["latest_owner_reply"]["id"], latest);
    assert_eq!(repeated["owner_answers"].as_array().unwrap().len(), 4);
    assert_eq!(creates(&h), before);
    for thread in threads {
        assert!(h.fake.borrow().messages[&thread]
            .iter()
            .any(|m| m["author"]["id"] == "OWN"));
    }
}

#[test]
fn changed_attachments_alone_do_not_create_new_decision_or_inherit_the_new_asset() {
    let mut h = Harness::new();
    let a = args(&h, "削除確認", "D:/models/one");
    let first = cmd_ask(&mut h.ctx, &a).unwrap();
    h.reply("a", s(&first["thread_id"]));
    let dir = h.ctx.here.join("question-progress");
    let file = std::fs::read_dir(&dir)
        .unwrap()
        .next()
        .unwrap()
        .unwrap()
        .path();
    let mut record = read_json(&file).unwrap();
    record["attachment_hashes"] = json!(["old-fingerprint"]);
    record["files"] = json!([{"name":"original.png","hash":"old-fingerprint"}]);
    atomic_json(&file, &record, 2).unwrap();
    // The supplied decision stays exact. Even a re-encoded or omitted asset must
    // return the original question/asset rather than emit another owner mention.
    let before = creates(&h);
    let result = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_eq!(creates(&h), before);
    assert_eq!(result["thread_id"], first["thread_id"]);
    assert_eq!(result["latest_owner_reply"]["content"], "a");
    assert_eq!(result["question_attachments"][0]["name"], "original.png");
    let mut new = identity("D:/models/one");
    new["scope"]["asset_hash"] = json!("new-fingerprint");
    atomic_json(a.decision_file.as_ref().unwrap(), &new, 2).unwrap();
    let second = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_ne!(second["thread_id"], first["thread_id"]);
    assert!(second["latest_owner_reply"].is_null());
}

#[test]
fn post_and_direct_producers_share_identity_and_never_publish_internal_json() {
    let mut h = Harness::new();
    h.fake.borrow_mut().threads.get_mut("100").unwrap()["guild_id"] = json!("G");
    let a = args(&h, "公開確認", "D:/models/one");
    let value = identity("D:/models/one");
    let text = format!("公開してよいですか？\na 公開\nb 待つ\n<!-- cc-decision {value} -->");
    std::fs::write(&a.body_file, &text).unwrap();
    let first = cmd_post(&mut h.ctx, "100", &a.body_file, &[], false).unwrap();
    let before = creates(&h);
    assert!(h
        .fake
        .borrow()
        .http_calls
        .iter()
        .filter(|c| c["method"] == "POST")
        .all(|c| !c["body"].to_string().contains("cc-decision")));
    h.reply("a", s(&first["thread_id"]));
    let repeated = cc_ask::question_progress::ask(
        &mut h.ctx,
        &json!({"title":"another worker","cwd":"D:/another"}),
        "100",
        &text,
        &[],
    )
    .unwrap();
    assert_eq!(repeated["thread_id"], first["thread_id"]);
    assert_eq!(repeated["latest_owner_reply"]["content"], "a");
    assert_eq!(creates(&h), before);
    let mut new = value;
    new["scope"]["spec_revision"] = json!(2);
    std::fs::write(
        &a.body_file,
        format!("公開してよいですか？\na 公開\nb 待つ\n<!-- cc-decision {new} -->"),
    )
    .unwrap();
    let second = cmd_post(&mut h.ctx, s(&first["thread_id"]), &a.body_file, &[], false).unwrap();
    assert_ne!(first["thread_id"], second["thread_id"]);
    assert!(second["latest_owner_reply"].is_null());
}

#[test]
fn shared_boundary_binds_todo_from_preparsed_identity_and_rejects_conflicting_context() {
    let mut h = Harness::new();
    let normalized = decision_identity::normalize(&h.ctx, &identity("D:/models/one")).unwrap();
    let context =
        json!({"title":"original","cwd":"D:/cleanup","todo_id":999,"decision_identity":normalized});
    let body = "公開してよいですか？\na 公開\nb 保持";
    assert!(cc_ask::question_progress::ask(&mut h.ctx, &context, "", body, &[]).is_err());
    assert_eq!(creates(&h), 0);
    let mut context = context;
    context.as_object_mut().unwrap().remove("todo_id");
    cc_ask::question_progress::ask(&mut h.ctx, &context, "", body, &[]).unwrap();
    assert_eq!(h.state()["questions"][0]["todo_id"], 2380);
}

#[test]
fn embedded_identity_cannot_overwrite_an_explicit_original_todo() {
    let mut h = Harness::new();
    let text = format!(
        "公開しますか？\na 公開\nb 保持\n<!-- cc-decision {} -->",
        identity("D:/models/one")
    );
    let context = json!({"title":"original","cwd":"D:/cleanup","todo_id":999});
    assert!(cc_ask::question_progress::ask(&mut h.ctx, &context, "", &text, &[]).is_err());
    assert_eq!(creates(&h), 0);
    let mut context = context;
    assert!(
        decision_identity::bind(&h.ctx, &mut context, Some(&identity("D:/models/one"))).is_err()
    );
    assert_eq!(context["todo_id"], 999);
}
