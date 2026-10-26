use cc_scheduler::received_input::{inspect, parse_request, Host, Layout, ReadError};
use serde_json::{json, Value};
use std::{
    collections::BTreeMap,
    path::{Path, PathBuf},
};

struct Fixture {
    files: BTreeMap<PathBuf, Vec<u8>>,
    reads: usize,
    denied: bool,
    fail_read: Option<PathBuf>,
    mutate: Option<(PathBuf, Vec<u8>)>,
}
impl Host for Fixture {
    fn read(&mut self, path: &Path, _: usize) -> Result<Vec<u8>, ReadError> {
        self.reads += 1;
        if self.fail_read.as_deref() == Some(path) {
            return Err(ReadError("permission_denied"));
        }
        // Change the received body after its first successful read.
        let result = self.files.get(path).cloned().ok_or(ReadError("not_found"));
        if path.ends_with("prompt.md") {
            if let Some((p, bytes)) = self.mutate.take() {
                self.files.insert(p, bytes);
            }
        }
        result
    }
    fn guard(&mut self, _: &Path) -> Result<bool, ReadError> {
        Ok(!self.denied)
    }
    fn now(&self) -> String {
        "2026-01-01T00:00:00Z".into()
    }
}
fn layout() -> Layout {
    Layout {
        todo: "D:/fixture/todo/todo.json".into(),
        specs: "D:/fixture/todo/specs".into(),
        prompts: "D:/fixture/scheduler/prompts".into(),
        jobs: "D:/fixture/jobs".into(),
    }
}
fn job_id() -> &'static str {
    "20260101-000000-todo-7"
}
fn fixture() -> (Fixture, Value) {
    let l = layout();
    let spec = l.specs.join("7-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.md");
    let source = l.prompts.join("7-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.md");
    let received = l.jobs.join(job_id()).join("prompt.md");
    let dispatch = b"worker\n\n\xE5\x85\x83\xE3\x81\xAE\xE7\x99\xBB\xE9\x8C\xB2\xE4\xBD\x9C\xE6\xA5\xAD\xE5\xA0\xB4\xE6\x89\x80: D:/work\n\n\xE4\xBE\x9D\xE9\xA0\xBC:\nhello\n".to_vec();
    let body = [
        dispatch.as_slice(),
        cc_job_policy::WRITE_GUIDANCE.as_bytes(),
    ]
    .concat();
    let todo = json!({"projects":{"fixture":"D:/work"},"items":[
        {"id":7,"project":"fixture","jobs":[job_id()],"spec":spec},
        {"id":8,"project":"other","note":"DO_NOT_DISCLOSE_OTHER_TASK"}
    ]});
    let job = json!({"id":job_id(),"todo_id":7,"project":"fixture","cwd":"D:/work-wt/7",
        "registered_cwd":"D:/work","work_owner":{"worktree":"D:/work-wt/7"},
        "scope_source":"launcher","sandbox":"workspace-write","launch_key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
        "prompt_source":source,"prompt":received,"status":"running","secret":"DO_NOT_DISCLOSE_JOB"});
    let files = BTreeMap::from([
        (l.todo.clone(), serde_json::to_vec(&todo).unwrap()),
        (
            l.jobs.join(job_id()).join("job.json"),
            serde_json::to_vec(&job).unwrap(),
        ),
        (spec, b"hello\n".to_vec()),
        (source, dispatch),
        (received, body),
    ]);
    (
        Fixture {
            files,
            reads: 0,
            denied: false,
            fail_read: None,
            mutate: None,
        },
        json!({
            "todo_id":7,"job_id":job_id(),"registered_scope":"D:/work","source_kind":"scheduler_dispatch"
        }),
    )
}

#[test]
fn registered_job_scope_and_output_type_are_checked() {
    for (key, value, code) in [
        ("registered_cwd", json!("D:/another"), "scope_mismatch"),
        (
            "work_owner",
            json!({"worktree":"D:/another"}),
            "scope_mismatch",
        ),
        (
            "scope_source",
            json!({"secret":"DO_NOT_DISCLOSE"}),
            "invalid_record",
        ),
        ("scope_source", json!("DO_NOT_DISCLOSE"), "invalid_record"),
        ("subwork_spec", json!("D:/unknown"), "spec_mismatch"),
    ] {
        let (mut f, r) = fixture();
        update_json(&mut f, layout().jobs.join(job_id()).join("job.json"), |v| {
            v[key] = value
        });
        assert_eq!(query(&mut f, &r).unwrap_err().0, code, "{key}");
    }
}

#[test]
fn file_permission_utf8_size_and_duplicate_keys_fail_without_echoing_input() {
    let (mut f, r) = fixture();
    f.fail_read = Some(layout().jobs.join(job_id()).join("prompt.md"));
    assert_eq!(query(&mut f, &r).unwrap_err().0, "permission_denied");
    let (mut f, r) = fixture();
    f.files.insert(
        layout()
            .prompts
            .join("7-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.md"),
        vec![0xff],
    );
    assert_eq!(query(&mut f, &r).unwrap_err().0, "invalid_utf8");
    let (mut f, r) = fixture();
    f.files.insert(
        layout().jobs.join(job_id()).join("job.json"),
        vec![b' '; 1024 * 1024 + 1],
    );
    assert_eq!(query(&mut f, &r).unwrap_err().0, "too_large");
    let (mut f, r) = fixture();
    f.files.insert(
        layout()
            .prompts
            .join("7-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.md"),
        vec![b'a'; 2 * 1024 * 1024 + 1],
    );
    assert_eq!(query(&mut f, &r).unwrap_err().0, "too_large");
    let (mut f, r) = fixture();
    f.files.insert(
        layout().todo,
        br#"{"items":[],"items":[{"secret":"DO_NOT_DISCLOSE"}]}"#.to_vec(),
    );
    assert_eq!(query(&mut f, &r).unwrap_err().0, "input_rejected");
    assert!(
        parse_request(br#"{"expected":{"parent_edge":{"kind":"review","kind":"check"}}}"#).is_err()
    );
}

#[test]
fn unrelated_updates_do_not_invalidate_the_selected_body() {
    let (mut f, r) = fixture();
    let mut db: Value = serde_json::from_slice(&f.files[&layout().todo]).unwrap();
    db["items"][1]["note"] = json!("OTHER_UNRELATED_VALUE");
    db["items"][0]["note"] = json!("a progress update");
    f.mutate = Some((layout().todo, serde_json::to_vec(&db).unwrap()));
    assert!(query(&mut f, &r).is_ok());
    let (mut f, r) = fixture();
    let p = layout().jobs.join(job_id()).join("job.json");
    let mut j: Value = serde_json::from_slice(&f.files[&p]).unwrap();
    j["tokens"] = json!({"input":1234});
    f.mutate = Some((p, serde_json::to_vec(&j).unwrap()));
    assert!(query(&mut f, &r).is_ok());
}

#[test]
fn changed_selected_binding_and_duplicate_spec_match_are_rejected() {
    let (mut f, r) = fixture();
    let p = layout().jobs.join(job_id()).join("job.json");
    let mut j: Value = serde_json::from_slice(&f.files[&p]).unwrap();
    j["scope_source"] = json!("owner_request");
    f.mutate = Some((p, serde_json::to_vec(&j).unwrap()));
    assert_eq!(query(&mut f, &r).unwrap_err().0, "changed_during_read");
    let (mut f, r) = fixture();
    let old = layout().specs.join("7-cccccccccccccccccccccccccccccccc.md");
    f.files.insert(old.clone(), b"hello\n".to_vec());
    update_json(&mut f, layout().todo, |v| {
        v["items"][0]["spec_history"] = json!([{"spec":old}])
    });
    assert_eq!(query(&mut f, &r).unwrap_err().0, "spec_mismatch");
}

#[test]
fn crlf_and_bom_saved_bytes_are_hashed_exactly_when_the_dispatch_matches() {
    for text in ["hello\r\n", "\u{feff}hello\n"] {
        let (mut f, r) = fixture();
        let p = layout()
            .prompts
            .join("7-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.md");
        let source = String::from_utf8(f.files[&p].clone()).unwrap();
        let source = source.strip_suffix("hello\n").unwrap().to_owned() + text;
        f.files.insert(p, source.as_bytes().to_vec());
        f.files.insert(
            layout().jobs.join(job_id()).join("prompt.md"),
            (source + cc_job_policy::WRITE_GUIDANCE).as_bytes().to_vec(),
        );
        f.files.insert(
            layout().specs.join("7-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.md"),
            text.as_bytes().to_vec(),
        );
        let report = query(&mut f, &r).unwrap();
        assert_eq!(report["spec_bytes"], text.len());
        assert_ne!(
            report["version"]["spec_sha256"],
            "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03"
        );
        assert_eq!(report["hash_domain"], "saved_file_bytes");
    }
}

#[test]
fn system_host_bounds_regular_files_and_rejects_hardlinks() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("input");
    std::fs::write(&path, b"bytes").unwrap();
    let mut host = cc_scheduler::received_input::SystemHost;
    assert_eq!(host.read(&path, 5).unwrap(), b"bytes");
    assert_eq!(host.read(&path, 4).unwrap_err().0, "too_large");
    let alias = dir.path().join("alias");
    std::fs::hard_link(&path, &alias).unwrap();
    assert_eq!(host.read(&path, 5).unwrap_err().0, "link_rejected");
    assert_eq!(host.read(&alias, 5).unwrap_err().0, "link_rejected");
}

#[test]
fn actual_managed_sandbox_and_fixed_review_guidance_are_preserved() {
    let (mut f, r) = fixture();
    update_json(&mut f, layout().jobs.join(job_id()).join("job.json"), |v| {
        v["sandbox"] = json!("danger-full-access")
    });
    assert!(query(&mut f, &r).is_ok());
    update_json(&mut f, layout().jobs.join(job_id()).join("job.json"), |v| {
        v["fixed_review_requalification"] = json!({"state":"registered"})
    });
    assert_eq!(query(&mut f, &r).unwrap_err().0, "received_mismatch");
    let original = f.files[&layout()
        .prompts
        .join("7-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.md")]
        .clone();
    f.files.insert(
        layout().jobs.join(job_id()).join("prompt.md"),
        [original.as_slice(), cc_job_policy::READ_GUIDANCE.as_bytes()].concat(),
    );
    let report = query(&mut f, &r).unwrap();
    assert_eq!(report["human_identity_attested"], false);
    assert_eq!(report["source_kind"], "scheduler_dispatch");
}
fn update_json(f: &mut Fixture, path: PathBuf, edit: impl FnOnce(&mut Value)) {
    let mut v: Value = serde_json::from_slice(&f.files[&path]).unwrap();
    edit(&mut v);
    f.files.insert(path, serde_json::to_vec(&v).unwrap());
}
fn query(f: &mut Fixture, r: &Value) -> Result<Value, ReadError> {
    inspect(
        f,
        &layout(),
        &parse_request(&serde_json::to_vec(r).unwrap())?,
    )
}

#[test]
fn one_input_can_be_refetched_by_exact_version_without_body_or_author_promotion() {
    let (mut f, mut r) = fixture();
    let report = query(&mut f, &r).unwrap();
    assert_eq!(report["source_kind"], "scheduler_dispatch");
    assert_eq!(report["scope_source"], "launcher");
    assert_eq!(
        report["version"]["spec_sha256"],
        "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03"
    );
    assert_eq!(report["spec_bytes"], 6);
    assert!(report["version"]["parent_edge"].is_null());
    assert_eq!(report["human_identity_attested"], false);
    assert_eq!(report["asset_qualified"], false);
    let text = report.to_string();
    for absent in [
        "hello",
        "DO_NOT_DISCLOSE",
        "WRITE_GUIDANCE",
        "secret",
        "owner_request",
    ] {
        assert!(!text.contains(absent), "{absent} leaked");
    }
    r["expected"] = report["version"].clone();
    assert_eq!(query(&mut f, &r).unwrap()["version"], report["version"]);
}

#[test]
fn mismatched_identity_source_scope_and_version_are_rejected() {
    for (field, value, reason) in [
        ("todo_id", json!(8), "todo_job_mismatch"),
        ("job_id", json!("missing"), "not_found"),
        ("source_kind", json!("scheduler_subwork"), "source_mismatch"),
        ("registered_scope", json!("D:/other"), "scope_mismatch"),
    ] {
        let (mut f, mut r) = fixture();
        r[field] = value;
        assert_eq!(query(&mut f, &r).unwrap_err().0, reason, "{field}");
    }
    for field in [
        "spec_ref",
        "spec_sha256",
        "dispatch_sha256",
        "received_sha256",
        "binding_sha256",
        "parent_edge",
    ] {
        let (mut f, mut r) = fixture();
        let mut version = query(&mut f, &r).unwrap()["version"].clone();
        version[field] = if field == "spec_ref" {
            json!("7-cccccccccccccccccccccccccccccccc.md")
        } else if field == "parent_edge" {
            json!({"todo_id":9,"job_id":"parent","origin_job_id":"parent","kind":"review"})
        } else {
            json!("0".repeat(64))
        };
        r["expected"] = version;
        assert!(query(&mut f, &r).is_err(), "{field}");
    }
}

#[test]
fn saved_parent_edges_are_verified_without_guessing_from_text() {
    let (mut f, mut r) = fixture();
    let l = layout();
    let edge = json!({"parent_todo_id":3,"origin_job_id":"parent","kind":"review","owner":{}});
    update_json(&mut f, l.todo.clone(), |v| {
        v["items"][0]["subwork"] = edge.clone();
        v["items"]
            .as_array_mut()
            .unwrap()
            .push(json!({"id":3,"project":"fixture","jobs":["parent"]}));
    });
    update_json(&mut f, l.jobs.join(job_id()).join("job.json"), |v| {
        v["subwork"] = edge;
        v["parent_job_id"] = json!("parent");
        v["subwork_spec"] = json!(l.specs.join("7-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.md"));
    });
    f.files.insert(
        l.jobs.join("parent/job.json"),
        serde_json::to_vec(&json!({"id":"parent","todo_id":3,"project":"fixture","cwd":"D:/work","registered_cwd":"D:/work"}))
            .unwrap(),
    );
    r["source_kind"] = json!("scheduler_subwork");
    let report = query(&mut f, &r).unwrap();
    assert_eq!(report["version"]["parent_edge"]["todo_id"], 3);
    assert_eq!(report["version"]["parent_edge"]["origin_job_id"], "parent");
    update_json(&mut f, l.jobs.join("parent/job.json"), |v| {
        v["todo_id"] = json!(4)
    });
    assert_eq!(query(&mut f, &r).unwrap_err().0, "parent_mismatch");
}

#[test]
fn denied_missing_duplicate_and_changed_records_do_not_succeed() {
    let (mut f, r) = fixture();
    f.denied = true;
    assert_eq!(query(&mut f, &r).unwrap_err().0, "permission_denied");
    let (mut f, r) = fixture();
    f.files
        .remove(&layout().specs.join("7-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.md"));
    assert_eq!(query(&mut f, &r).unwrap_err().0, "not_found");
    let (mut f, r) = fixture();
    update_json(&mut f, layout().todo, |v| {
        let row = v["items"][0].clone();
        v["items"].as_array_mut().unwrap().push(row);
    });
    assert_eq!(query(&mut f, &r).unwrap_err().0, "duplicate_todo");
    let (mut f, r) = fixture();
    f.mutate = Some((
        layout().jobs.join(job_id()).join("prompt.md"),
        b"changed".to_vec(),
    ));
    assert_eq!(query(&mut f, &r).unwrap_err().0, "changed_during_read");
}

#[test]
fn newline_and_bom_are_not_normalized_and_old_spec_can_be_selected() {
    let (mut f, r) = fixture();
    let original = query(&mut f, &r).unwrap()["version"].clone();
    let old = layout().specs.join("7-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.md");
    let new = layout().specs.join("7-cccccccccccccccccccccccccccccccc.md");
    f.files.insert(new.clone(), b"hello\r\n".to_vec());
    update_json(&mut f, layout().todo, |v| {
        v["items"][0]["spec"] = json!(new);
        v["items"][0]["spec_history"] = json!([{"spec":old}]);
    });
    assert_eq!(
        query(&mut f, &r).unwrap()["version"]["spec_sha256"],
        original["spec_sha256"]
    );
    f.files.insert(old, b"\xef\xbb\xbfhello\n".to_vec());
    assert_eq!(query(&mut f, &r).unwrap_err().0, "spec_mismatch");
}

#[test]
fn path_escape_and_unexpected_request_shape_fail_before_reading() {
    for field in ["owner", "host", "raw", "api_key"] {
        let (mut f, mut r) = fixture();
        r[field] = json!("sensitive");
        assert_eq!(query(&mut f, &r).unwrap_err().0, "input_rejected");
        assert_eq!(f.reads, 0);
    }
    for id in ["../other", "a/b", "a\\b", ".", "x".repeat(129).as_str()] {
        let (mut f, mut r) = fixture();
        r["job_id"] = json!(id);
        assert!(query(&mut f, &r).is_err());
        assert_eq!(f.reads, 0);
    }
    assert!(parse_request(br#"{"todo_id":7,"todo_id":7}"#).is_err());
    assert!(parse_request(&vec![b' '; 65537]).is_err());
    let (mut f, r) = fixture();
    update_json(&mut f, layout().jobs.join(job_id()).join("job.json"), |v| {
        v["prompt"] = json!("D:/outside/prompt.md")
    });
    assert_eq!(query(&mut f, &r).unwrap_err().0, "path_mismatch");
}
