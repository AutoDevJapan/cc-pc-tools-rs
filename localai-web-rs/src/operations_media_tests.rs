//! ToDo3103: the owner's 2026-10-21 instruction supersedes the old separate
//! placement only for youtube-daily. All records below are synthetic.
use super::*;

fn media_board(masked: bool) -> (tempfile::TempDir, Value) {
    let temp = tempfile::tempdir().unwrap();
    let root = temp.path();
    fs::create_dir_all(root.join("todo")).unwrap();
    fs::create_dir_all(root.join("codex-jobs/jobs")).unwrap();
    if !masked {
        fs::write(root.join("client-projects.txt"), "SyntheticPrivate\n").unwrap();
    }
    let db = json!({"projects":{"youtube-daily":"D:/synthetic/youtube-daily",
        "tools":"D:/synthetic/tools","SyntheticPrivate":"D:/synthetic/private"},"items":[
        {"id":1,"project":"youtube-daily","status":"doing","title":"動画を制作する","jobs":["media-run","media-old"]},
        {"id":2,"project":"youtube_daily","status":"doing","title":"動画を検査する","jobs":["media-queue"]},
        {"id":3,"project":"youtube-daily","status":"doing","title":"動画を整理する","jobs":["media-old"]},
        {"id":4,"project":"youtube-daily","status":"done","updated_at":"2020-01-01T00:00:00Z"},
        {"id":5,"project":"youtube-daily","status":"doing","title":"関連作業を確認する","jobs":["media-run"]},
        {"id":6,"project":"youtube-daily","status":"done","updated_at":"2026-10-21T02:00:00Z"},
        {"id":7,"project":"youtube-daily","status":"wait","wait_on":[1],"jobs":["media-run"]},
        {"id":8,"project":"tools","status":"doing","title":"youtube-dailyの監督を行う"},
        {"id":9,"project":"SyntheticPrivate","status":"doing","title":"private-fixture-prose"}
    ]});
    fs::write(root.join("todo/todo.json"), db.to_string()).unwrap();
    for job in [
        json!({"id":"media-run","todo_id":1,"related_todo_ids":[5,7],"status":"running","project":"youtube-daily","created_at":"2026-10-21T02:00:00Z"}),
        json!({"id":"media-queue","todo_id":2,"status":"queued","project":"youtube-daily","created_at":"2026-10-21T01:00:00Z"}),
        json!({"id":"media-old","todo_id":3,"status":"succeeded","project":"youtube-daily","created_at":"2026-10-20T01:00:00Z","finished_at":"2026-10-20T02:00:00Z"}),
    ] {
        let dir = root.join("codex-jobs/jobs").join(text(&job, "id"));
        fs::create_dir_all(&dir).unwrap();
        fs::write(dir.join("job.json"), job.to_string()).unwrap();
    }
    let now = stamp(&json!("2026-10-21T02:01:00Z")).unwrap();
    let board = collect(root, now);
    (temp, board)
}

fn media_product(board: &Value) -> &Value {
    board["departments"]
        .as_array()
        .unwrap()
        .iter()
        .find(|d| d["name"] == "発信部")
        .unwrap()["products"]
        .as_array()
        .unwrap()
        .iter()
        .find(|p| p["name"] == "youtube-daily")
        .expect("the explicitly assigned media product must appear in 発信部")
}

fn assert_media_projection(masked: bool) {
    let (temp, board) = media_board(masked);
    let product = media_product(&board);
    let rows = product["workers"].as_array().unwrap();
    assert_eq!(
        rows.len(),
        6,
        "recent records only; never the historical registration total"
    );
    let row = |id: &str| rows.iter().find(|row| row["id"] == id).unwrap();
    assert_eq!(row("todo-1")["jobStatus"], "running");
    assert_eq!(row("todo-2")["jobStatus"], "queued");
    assert!(
        row("todo-3")["primaryJob"].is_null(),
        "finished job is not current"
    );
    assert!(
        row("todo-5")["primaryJob"].is_null(),
        "related record cannot count the primary job twice"
    );
    assert!(
        row("todo-7")["primaryJob"].is_null(),
        "parent/dependency record cannot count the child's job again"
    );
    assert_eq!(row("todo-6")["recordedStatus"], "done");
    assert!(!board["unassignedProducts"]
        .as_array()
        .unwrap()
        .iter()
        .any(|p| p["name"] == "youtube-daily"));
    assert!(!board["unlistedRegistrations"]
        .as_array()
        .into_iter()
        .flatten()
        .any(|p| p["name"] == "youtube-daily"));
    assert!(!rows.iter().any(|row| row["id"] == "todo-8"));
    assert!(!board.to_string().contains("private-fixture-prose"));
    if masked {
        assert_eq!(board["protectionMode"], "masked_metadata");
        assert_eq!(board["sourceOk"], false);
        assert_eq!(board["metadataOk"], true);
        assert!(!temp.path().join("client-projects.txt").exists());
        assert!(rows.iter().all(|row| row["detailAvailable"] == false));
        assert_eq!(board["metrics"]["activeJobs"], 1);
        assert_eq!(board["metrics"]["jobQueued"], 1);
    } else {
        // Unrelated scheduler/question fixtures are deliberately absent. This
        // is the normal privacy path, not a certificate of whole-board health.
        assert!(board["protectionMode"].is_null());
    }
}

#[test]
fn youtube_daily_normal_department_and_job_ownership() {
    assert_media_projection(false);
}

#[test]
fn youtube_daily_masked_department_and_job_ownership() {
    assert_media_projection(true);
}

#[test]
fn youtube_daily_processing_counts_primary_once() {
    {
        let (_temp, board) = media_board(true);
        let now = board["fetchedAt"].as_u64().unwrap();
        let digest = ring::digest::digest(&ring::digest::SHA256, b"media-run");
        let key = format!(
            "job-{}",
            digest
                .as_ref()
                .iter()
                .map(|b| format!("{b:02x}"))
                .collect::<String>()
        );
        let active = json!({"schema_version":1,"processing_contract":"agent-progress-active-v1",
            "captured_at":"2026-10-21T02:00:59Z","heartbeat_at":"2026-10-21T02:01:00Z",
            "status":"ok","truncated":false,"issues":[],"sources":{"todo":{"status":"ok","complete":true},"jobs":{"status":"ok","complete":true}},
            "jobs":[{"job_key":key,"todo_ids":[1,5,7],"processing_verified":true,"recorded_status":"running",
                "execution":"running","worker_presence":"verified_alive","phase":"tool","wait_reasons":[],
                "source":{"status":"ok","complete":true},"last_activity":{"at":"2026-10-21T02:01:00Z","state":"fresh"}}]});
        let counts = crate::workforce::project(&board, Some(&active), now);
        assert_eq!(
            counts["processing"]["verifiedCount"], 1,
            "a waiting related parent cannot stop the actual primary"
        );
        assert_eq!(
            counts["states"]["processing"], 1,
            "one job cannot produce multiple current media workers"
        );
        for id in [5, 7] {
            let task = counts["tasks"]
                .as_array()
                .unwrap()
                .iter()
                .find(|row| row["todoId"] == id)
                .unwrap();
            assert!(task["references"]["sessions"]
                .as_array()
                .unwrap()
                .is_empty());
        }
        let unknown = crate::workforce::project(&board, None, now);
        assert!(unknown["processing"]["total"].is_null());
        assert!(unknown["processing"]["verifiedCount"].is_null());
    }
}

#[test]
fn youtube_daily_primary_fallback_preserves_other_products() {
    let db = json!({"items":[
        {"id":1,"status":"doing","jobs":["shared"]},
        {"id":2,"status":"doing","scheduler":{"job":"shared"}}
    ]});
    let a = &db["items"][0];
    let b = &db["items"][1];
    let mut job = json!({"id":"shared","status":"running"});
    assert!(media_current(a, &[&job], Some(&db), true).is_none());
    assert!(media_current(b, &[&job], Some(&db), true).is_some());
    job["todo_id"] = json!(1);
    assert!(media_current(a, &[&job], Some(&db), true).is_some());
    assert!(media_current(b, &[&job], Some(&db), true).is_none());
    // The scoped repair preserves other products' existing current selection.
    assert!(media_current(b, &[&job], Some(&db), false).is_some());
    job["todo_id"] = json!(99);
    assert!(media_current(b, &[&job], Some(&db), true).is_some());
    job["status"] = json!("succeeded");
    assert!(media_current(a, &[&job], Some(&db), true).is_none());
    assert!(media_current(b, &[&job], Some(&db), false).is_none());
}

#[test]
fn youtube_daily_old_membership_receipt_remains_intact() {
    let (temp, _) = media_board(true);
    let root = temp.path();
    let path = root.join("localai-web-rs/target/release/operations-membership-choice.json");
    fs::create_dir_all(path.parent().unwrap()).unwrap();
    fs::create_dir_all(root.join("discord-ask")).unwrap();
    let id = safe_id("registration", "youtube-daily");
    let receipt = json!({"selection":"separate","source":"discord-owner-reply","todo_id":2473,
        "author_id":"100","question_thread_id":"300","question_message_id":"300","message_id":"301",
        "answered_at":"2026-10-12T14:00:00Z","registration_ids":[id]})
    .to_string();
    let ledger = json!({"domain":"new-owner-recovery-messages","events":[
        {"event":"intent","request":{"request_id":"fixture","destination":{"owner":"100","guild":"200"},
            "operation":{"kind":"ask","todo_id":2473,"decision_original":{"resources":[{"id":id}]}}}},
        {"event":"verified","request_id":"fixture","phase":"question:0","receipt":{"thread_id":"300","message_id":"300"}}
    ]}).to_string();
    fs::write(&path, &receipt).unwrap();
    let ledger_path = root.join("discord-ask/recovery-messages.json");
    fs::write(&ledger_path, &ledger).unwrap();
    let now = stamp(&json!("2026-10-21T02:01:00Z")).unwrap();
    assert!(membership_choice(root, now).is_some());
    let board = collect(root, now);
    assert_eq!(
        media_product(&board)["workers"].as_array().unwrap().len(),
        6
    );
    assert_eq!(board["membershipDecision"]["selection"], "separate");
    assert_eq!(fs::read_to_string(&path).unwrap(), receipt);
    assert_eq!(fs::read_to_string(&ledger_path).unwrap(), ledger);
}

#[test]
fn youtube_daily_unavailable_jobs_remain_unknown() {
    let temp = tempfile::tempdir().unwrap();
    fs::create_dir_all(temp.path().join("todo")).unwrap();
    fs::write(
        temp.path().join("todo/todo.json"),
        json!({"projects":{"youtube-daily":"D:/synthetic/youtube-daily"},
        "items":[{"id":1,"project":"youtube-daily","status":"doing"}]})
        .to_string(),
    )
    .unwrap();
    let board = collect(temp.path(), 1_000_000);
    assert_eq!(media_product(&board)["workers"][0]["status"], "waiting");
    assert!(board["metrics"]["activeJobs"].is_null());
    assert!(board["metrics"]["jobQueued"].is_null());
    assert_eq!(board["metadataOk"], false);
}
