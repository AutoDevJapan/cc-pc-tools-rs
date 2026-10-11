#[test]
fn large_todo_snapshot_and_canonical_admission_are_preserved() {
    let f = Fixture::new();
    f.on();
    let path = f.console.paths.todo.join("todo.json");
    let mut db = f.console.todos().unwrap();
    db["items"][0]["spec"] = json!("saved.md");
    db["items"][0]["needs"] = json!(["cargo"]);
    db["items"][0]["note_history"] = json!(["x".repeat(16 * 1024 * 1024)]);
    db["items"]
        .as_array_mut()
        .unwrap()
        .push(json!({"id":2,"project":"demo","status":"doing",
        "subwork":{"parent_todo_id":1,"kind":"review"},"spec":"review.md"}));
    fs::write(&path, db.to_string()).unwrap();
    assert_eq!(f.console.todos().unwrap(), db);
    let accepted = f.delegate("capacity-review").unwrap();
    assert_eq!(accepted["accepted"], true);
    let queue = f.console.read_state().unwrap();
    assert_eq!(queue.queue.len(), 1);
    assert_eq!(queue.queue[0].key, "manual:capacity-review");
    assert_eq!(f.console.todos().unwrap(), db);
}

#[test]
fn oversized_corrupt_or_unreadable_todo_never_changes_admission() {
    let f = Fixture::new();
    f.on();
    f.delegate("existing").unwrap();
    let path = f.console.paths.todo.join("todo.json");
    let state_before = fs::read(f.console.paths.db()).unwrap();
    let mut bytes = b"{\"projects\":{},\"items\":[]}".to_vec();
    bytes.resize(cc_todo::STORE_MAX_BYTES + 1, b' ');
    fs::write(&path, &bytes).unwrap();
    for (content, code) in [
        (&bytes[..], "todo_store_too_large"),
        (&b"{broken"[..], "todo_store_invalid_json"),
    ] {
        fs::write(&path, content).unwrap();
        assert_eq!(f.console.todos().unwrap_err().code, code);
        assert_eq!(f.delegate("unaccepted").unwrap_err().code, code);
        assert_eq!(fs::read(f.console.paths.db()).unwrap(), state_before);
    }
    fs::remove_file(&path).unwrap();
    fs::create_dir(&path).unwrap();
    assert_eq!(
        f.delegate("unaccepted").unwrap_err().code,
        "state_unavailable"
    );
    assert_eq!(fs::read(f.console.paths.db()).unwrap(), state_before);
}

#[tokio::test]
async fn todo_capacity_reply_redaction_keeps_valid_json_and_canonical_identity() {
    let secret = format!("mfa.{}", "a".repeat(48));
    let response = reply(Ok(
        json!({"canonical":"manual:finite-review","note":format!("quoted \\\" value {secret}\nline"),
        "nested":{"spec":"saved.md","needs":["cargo"],"subwork":{"parent_todo_id":1}}}),
    ));
    assert_eq!(response.status(), StatusCode::OK);
    let body = to_bytes(response.into_body(), 4096).await.unwrap();
    let parsed: Value = serde_json::from_slice(&body).unwrap();
    assert_eq!(parsed["data"]["canonical"], "manual:finite-review");
    assert_eq!(parsed["data"]["nested"]["subwork"]["parent_todo_id"], 1);
    assert!(parsed["data"]["note"].as_str().unwrap().contains("[秘密]"));
    assert!(!String::from_utf8(body.to_vec()).unwrap().contains(&secret));
}
