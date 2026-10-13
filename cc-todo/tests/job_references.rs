use cc_scheduler::policy::Processes;
use cc_todo::{Cli, Command, Paths, execute, job_cwd, recovery, save};
use clap::Parser;
use serde_json::{Value, json};
use std::os::windows::process::CommandExt;
use std::{fs, process::Command as Process};

fn command(args: &[&str]) -> Command {
    Cli::try_parse_from(std::iter::once("todo").chain(args.iter().copied()))
        .unwrap()
        .command
}

fn database(jobs: Value) -> Value {
    json!({"projects":{"demo":"D:/synthetic"},"next_id":2,"stalls":{},"items":[{
        "id":1,"project":"demo","title":"合成の作業","note":"合成のメモ",
        "status":"recovery_unverified","jobs":jobs,"created_s":1,
        "recovery_evidence":{"approved":false,"original":"保持する証拠"}
    }]})
}

fn paths(root: &std::path::Path) -> Paths {
    Paths::at(root, &root.join("jobs"))
}

fn write_job(paths: &Paths, id: &str, cwd: &str) {
    let directory = paths.jobs.join(id);
    fs::create_dir_all(&directory).unwrap();
    fs::write(
        directory.join("job.json"),
        json!({"id":id,"cwd":cwd}).to_string(),
    )
    .unwrap();
}

#[test]
fn list_mixed_recovered_references_uses_latest_valid_string_and_preserves_all_evidence() {
    let temp = tempfile::tempdir().unwrap();
    let p = paths(temp.path());
    write_job(&p, "old-job", "D:/synthetic/old");
    write_job(&p, "new-job", "D:/synthetic/current");
    write_job(&p, "untrusted", "D:/synthetic/object-should-not-be-read");
    let unknown = json!({"id":"untrusted","status":"succeeded","private":"hidden-object"});
    let mut db = database(json!([
        "old-job",
        unknown,
        "new-job",
        null,
        true,
        17,
        ["untrusted"],
        "../escape"
    ]));
    let before = db.clone();
    let lines = execute(&mut db, &command(&["list", "--all"]), &p).unwrap();
    assert_eq!(lines.len(), 1);
    assert!(
        lines[0].contains("[old-job, new-job] @ D:/synthetic/current")
            || (lines[0].contains("[old-job, new-job]")
                && lines[0].contains("@ D:/synthetic/current"))
    );
    assert!(lines[0].contains("未確認のジョブ参照 6件"));
    for hidden in [
        "hidden-object",
        "object-should-not-be-read",
        "escape",
        "untrusted",
    ] {
        assert!(!lines[0].contains(hidden));
    }
    assert_eq!(db, before);
}

#[test]
fn list_unknown_shapes_and_invalid_strings_is_readable_and_does_not_promote_them() {
    let temp = tempfile::tempdir().unwrap();
    let p = paths(temp.path());
    for jobs in [
        json!([{"id":"invented","approved":true}]),
        json!([null, false, 42, ["invented"]]),
        json!([
            "",
            " ",
            "../outside",
            "a/b",
            "a\\b",
            "C:outside",
            "\0",
            "合成"
        ]),
        json!({"id":"invented","approved":true}),
        json!("invented"),
        Value::Null,
    ] {
        let mut db = database(jobs);
        let before = db.clone();
        let lines = execute(&mut db, &command(&["list"]), &p).unwrap();
        assert_eq!(lines.len(), 1);
        assert!(lines[0].contains("未確認のジョブ参照"));
        assert!(lines[0].contains("@ D:/synthetic"));
        assert!(!lines[0].contains("invented"));
        assert_eq!(db, before);
    }
}

#[test]
fn list_ordinary_string_references_keeps_the_existing_display_without_a_warning() {
    let temp = tempfile::tempdir().unwrap();
    let p = paths(temp.path());
    write_job(&p, "job_1-A", "D:/synthetic/current");
    let mut db = database(json!(["job_1-A"]));
    let before = db.clone();
    let line = execute(&mut db, &command(&["list"]), &p).unwrap().remove(0);
    assert_eq!(
        line,
        "  1 recovery_unverified demo     合成の作業 [job_1-A] @ D:/synthetic/current — 合成のメモ"
    );
    assert_eq!(db, before);
}

#[test]
fn job_cwd_refuses_invalid_components_instead_of_reading_outside_the_job_store() {
    let temp = tempfile::tempdir().unwrap();
    let p = paths(temp.path());
    fs::create_dir_all(&p.jobs).unwrap();
    // A traversal would resolve this real receipt. The accepted component must not.
    fs::write(
        temp.path().join("job.json"),
        json!({"cwd":"unexpected-outside-cwd"}).to_string(),
    )
    .unwrap();
    for id in [
        "",
        ".",
        "..",
        "../",
        "..\\",
        "a/b",
        "a\\b",
        "C:outside",
        " ",
        "\0",
    ] {
        assert_eq!(job_cwd(&p.jobs, id), "");
    }
    write_job(&p, "valid-job_1", "D:/synthetic/inside");
    assert_eq!(job_cwd(&p.jobs, "valid-job_1"), "D:/synthetic/inside");
}

#[test]
fn start_appends_a_valid_reference_without_removing_mixed_recovery_records() {
    let temp = tempfile::tempdir().unwrap();
    let p = paths(temp.path());
    let evidence = json!({"id":"old","result":{"unverified":true}});
    let mut db = database(json!([evidence, "old", null]));
    // Mixed historical references also survive ordinary, already verified work.
    // Recovered state now requires the receipt-checked resume command.
    db["items"][0]["status"] = json!("todo");
    execute(&mut db, &command(&["start", "1", "--job", "new-job"]), &p).unwrap();
    assert_eq!(
        db["items"][0]["jobs"],
        json!([evidence, "old", null, "new-job"])
    );
    assert_eq!(db["items"][0]["status"], "doing");
    assert_eq!(db["items"][0]["recovery_evidence"]["approved"], false);
}

#[test]
fn start_rejects_an_invalid_reference_before_changing_state_or_worker_ownership() {
    let temp = tempfile::tempdir().unwrap();
    let p = paths(temp.path());
    for job in ["../outside", "a/b", "a\\b", "C:outside", " ", "合成"] {
        let mut db = database(json!([{"id":"old"},"old"]));
        let before = db.clone();
        assert!(execute(&mut db, &command(&["start", "1", "--job", job]), &p).is_err());
        assert_eq!(db, before);
    }
}

#[test]
fn recovery_does_not_match_unknown_reference_shapes_or_invalid_strings_as_owners() {
    for references in [
        json!([null]),
        json!([{"id":"invented"}]),
        json!(["../outside"]),
    ] {
        let db = database(references.clone());
        let jobs = [json!({"id":references[0],"status":"queued","cwd":"D:/synthetic"})];
        let before = db.clone();
        let preview = recovery::preview(
            &db,
            &jobs,
            &Processes::new(),
            &json!({"queue":[],"launches":{}}),
            10000.,
            "",
            &[],
        )
        .unwrap();
        assert_eq!(preview["items"][0]["owned_jobs"], json!([]));
        assert_eq!(preview["would_reserve"], json!([]));
        assert_eq!(preview["items"][0]["status"], "recovery_unverified");
        assert_eq!(db, before);
    }
}

#[test]
fn recovery_uses_the_existing_spec_when_only_unknown_job_evidence_was_recovered() {
    let mut db = database(json!([{"id":"not-a-worker","approved":true}]));
    db["items"][0]["status"] = json!("todo");
    db["items"][0]["dispatchable"] = json!(true);
    db["items"][0]["spec"] = json!("synthetic-spec.md");
    let before = db.clone();
    assert_eq!(
        recovery::plan(&mut db, &[], &Processes::new(), 10000., "").unwrap(),
        vec![1]
    );
    assert_eq!(
        db["items"][0]["stall_recovery"]["pending"]["kind"],
        "scheduler"
    );
    assert_eq!(db["items"][0]["jobs"], before["items"][0]["jobs"]);
    assert_eq!(
        db["items"][0]["recovery_evidence"],
        before["items"][0]["recovery_evidence"]
    );
    assert_eq!(db["items"][0]["status"], "todo");
}

#[test]
fn recovery_keeps_unknown_references_without_a_saved_spec_unassigned() {
    let mut db = database(json!([{"id":"invented","spec":"not-authority"}]));
    db["items"][0]["status"] = json!("todo");
    let before = db.clone();
    assert!(
        recovery::plan(&mut db, &[], &Processes::new(), 10000., "")
            .unwrap()
            .is_empty()
    );
    assert_eq!(db["items"], before["items"]);
}

#[test]
fn native_list_reads_a_mixed_store_without_rewriting_or_backing_up_the_original() {
    let temp = tempfile::tempdir().unwrap();
    let p = paths(temp.path());
    let db = database(json!(["job-1",{"id":"untrusted","private":"hidden-native-object"},null]));
    save(&p.store, &db).unwrap();
    let original = fs::read(&p.store).unwrap();
    let listed = Process::new(env!("CARGO_BIN_EXE_todo"))
        .creation_flags(0x08000000)
        .env("CC_TODO_HOME", temp.path())
        .env("CC_TODO_JOBS", &p.jobs)
        .args(["list", "--all"])
        .output()
        .unwrap();
    assert!(
        listed.status.success(),
        "{:?}",
        String::from_utf8_lossy(&listed.stderr)
    );
    assert!(listed.stderr.is_empty());
    let text = String::from_utf8(listed.stdout).unwrap();
    assert!(text.contains("[job-1]") && text.contains("未確認のジョブ参照 2件"));
    assert!(!text.contains("hidden-native-object") && !text.contains("untrusted"));
    assert_eq!(fs::read(&p.store).unwrap(), original);
    assert_eq!(
        fs::read_dir(temp.path()).unwrap().count(),
        2,
        "only original todo.json and the ordinary read lock may exist"
    );
}
