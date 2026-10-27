use super::*;
use cc_scheduler::admission::{self, Observation, Reason, Stage, MAX_BYTES, MAX_ENTRIES, TTL};
use std::os::windows::fs::OpenOptionsExt;

fn snapshot(f: &Fixture) -> Value {
    serde_json::from_slice(&fs::read(f.e.root.join("admission.json")).unwrap()).unwrap()
}
fn entry(f: &Fixture, id: u64) -> Value {
    snapshot(f)["entries"][id.to_string()].clone()
}

#[test]
fn ordinary_order_and_launches_survive_observation_write_failure() {
    for fail_write in [false, true] {
        let mut f = Fixture::new();
        // Same priority, project rotation, then the next owner's priority class.
        for (id, project) in [(1, "other"), (2, "GoDD-AO"), (3, "civ6"), (4, "civ6")] {
            f.put(item(id, project, "todo", &[]));
        }
        if fail_write {
            fs::create_dir(f.e.root.join("admission.json")).unwrap();
        }
        let mut actual = vec![];
        for _ in 0..4 {
            f.dispatch();
            actual.push(f.e.backend.launched.last().unwrap()["item"]["id"].clone());
            f.e.backend.clock += 11.;
        }
        assert_eq!(actual, vec![json!(3), json!(4), json!(2), json!(1)]);
        assert_eq!(f.e.backend.pick_count, 4);
        assert_eq!(f.e.report["admission_recorded"], !fail_write);
        assert!(f.e.backend.cancelled.is_empty());
        assert!(f.e.backend.killed.is_empty());
    }
}

#[test]
fn actual_queue_rank_capacity_and_unreached_stages_are_separate() {
    let mut f = Fixture::new();
    for (id, project) in [(1, "other"), (2, "GoDD-AO"), (3, "civ6"), (4, "civ6")] {
        f.put(item(id, project, "todo", &[]));
    }
    f.e.backend.m["cpu_percent"] = json!(99.);
    let before = fs::read(&f.e.todo_path).unwrap();
    f.dispatch();
    for (id, rank) in [(3, 1), (4, 2), (2, 3), (1, 4)] {
        let e = entry(&f, id);
        assert_eq!(e["eligible"], true);
        assert_eq!(e["candidate"], true);
        assert_eq!(e["queue_rank"], rank);
        assert_eq!(e["stage"], "capacity");
        assert_eq!(e["reason"], "capacity");
        assert!(e["preparation_code"].is_null());
        assert!(e["recheck_capacity"].is_null());
    }
    assert!(snapshot(&f)["capacity"]["reasons"]
        .as_array()
        .unwrap()
        .contains(&json!("cpu_limit")));
    assert_eq!(fs::read(&f.e.todo_path).unwrap(), before);
    assert_eq!(f.e.backend.pick_count, 0);
    assert!(f.e.backend.launched.is_empty());
    let result = admission::explain(&f.e.root, &f.e.todo_path, 3, T);
    assert_eq!(result["validity"], "matching_task_metadata");
}

#[test]
fn filtering_dependency_time_question_reservation_and_related_owner_are_real_branches() {
    let mut f = Fixture::new();
    for id in 1..=9 {
        f.put(item(id, "other", "todo", &[]));
    }
    f.e.edit(&json!(1), |r| {
        r["status"] = json!("block");
        r["owner_stopped"] = json!(true);
    })
    .unwrap();
    f.e.edit(&json!(2), |r| r["dispatchable"] = json!(false))
        .unwrap();
    f.e.edit(&json!(3), |r| r["wait_on"] = json!([99])).unwrap();
    f.e.edit(&json!(4), |r| r["scheduler"] = json!({"not_before":T+100.}))
        .unwrap();
    f.e.edit(&json!(5), |r| {
        r["scheduler"] = json!({"question_close_pending":true})
    })
    .unwrap();
    f.e.edit(&json!(6), |r| {
        r["stall_recovery"] = json!({"pending":{"job":"reserved"}})
    })
    .unwrap();
    f.e.edit(&json!(7), |r| r["jobs"] = json!(["related"]))
        .unwrap();
    f.e.edit(&json!(8), |r| r["spec"] = Value::Null).unwrap();
    f.e.edit(
        &json!(9),
        |r| r["scheduler"] = json!({"last_job":"old","quota_retry_terminal":{"source_job":"old"}}),
    )
    .unwrap();
    f.e.backend.records.push(job("related", 1, "running"));
    f.e.backend.m["cpu_percent"] = json!(99.);
    f.dispatch();
    for (id, reason, stage) in [
        (1, "status_not_todo", "filtering"),
        (2, "not_dispatchable", "filtering"),
        (3, "dependency_wait", "filtering"),
        (4, "not_before", "priority"),
        (5, "question_close_pending", "priority"),
        (6, "recovery_reservation", "filtering"),
        (7, "related_owner", "filtering"),
        (8, "missing_spec_metadata", "filtering"),
        (9, "quota_retry_ended", "filtering"),
    ] {
        let e = entry(&f, id);
        assert_eq!(e["reason"], reason, "id={id}");
        assert_eq!(e["stage"], stage, "id={id}");
        assert_eq!(e["candidate"], false);
        assert!(e["queue_rank"].is_null());
        assert!(e["recheck_capacity"].is_null());
    }
    assert!(f.e.backend.launched.is_empty());
    assert_eq!(f.e.backend.pick_count, 0);
}

#[test]
fn existing_root_admission_and_exclusions_are_observed_without_an_extra_guard_call() {
    for allowed in [false, true] {
        let mut f = Fixture::new();
        let mut db = f.e.todo().unwrap();
        db["projects"]["cc-pc-tools-rs"] = json!(f.repo);
        write_json(&f.e.todo_path, &db, 1).unwrap();
        f.put(item(4091, "cc-pc-tools-rs", "todo", &[]));
        fs::remove_file(f.e.root.parent().unwrap().join("client-projects.txt")).unwrap();
        f.e.backend.repair_allowed = allowed;
        f.e.backend.m["cpu_percent"] = json!(99.);
        f.dispatch();
        let e = entry(&f, 4091);
        assert_eq!(e["root_guard_allowed"], allowed);
        assert_eq!(e["root_excluded"], !allowed);
        assert_eq!(
            e["reason"],
            if allowed { "capacity" } else { "root_excluded" }
        );
        assert_eq!(
            f.e.backend.repair_calls,
            vec![f.repo.to_string_lossy().to_string()]
        );
    }
}

#[test]
fn resource_preparation_quota_and_not_reached_are_not_inferred_as_launches() {
    let mut f = Fixture::new();
    f.put(item(1, "civ6", "todo", &["civ6"]));
    f.put(item(2, "other", "todo", &[]));
    f.put(item(3, "other", "todo", &[]));
    f.e.backend.ps.insert(44, proc(44, 0, "CivilizationVI.exe"));
    f.dispatch();
    assert_eq!(entry(&f, 1)["reason"], "resource_busy");
    assert_eq!(entry(&f, 2)["reason"], "launched");
    assert_eq!(entry(&f, 3)["reason"], "not_reached");
    assert_eq!(entry(&f, 3)["stage"], "priority");
    assert_eq!(f.e.backend.launched.len(), 1);

    let mut f = Fixture::new();
    f.put(item(1, "other", "todo", &[]));
    f.e.backend.worktree_failure = Some(1);
    assert_eq!(
        f.e.dispatch(&f.cfg, &f.pri).unwrap_err(),
        Error("OriginMissing")
    );
    assert_eq!(entry(&f, 1)["reason"], "preparation_failed");
    assert_eq!(entry(&f, 1)["preparation_code"], "origin_missing");
    assert!(!snapshot(&f)["complete"].as_bool().unwrap());
    assert!(f.e.backend.launched.is_empty());

    let mut f = Fixture::new();
    let mut row = item(1, "other", "todo", &[]);
    row["model"] = json!("gpt-6.1-sol");
    f.put(row);
    f.e.backend.m["quota_block_model"] = json!("gpt-6.1-sol");
    f.dispatch();
    assert_eq!(entry(&f, 1)["reason"], "quota_exhausted");
    assert_eq!(entry(&f, 1)["stage"], "preparation");
    assert_eq!(f.e.backend.pick_count, 0);
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn exhausted_review_and_capacity_changed_during_preparation_keep_their_stages() {
    let mut f = Fixture::new();
    f.put(item(1, "other", "todo", &[]));
    f.e.edit(&json!(1),|r|{
        r["scheduler"]=json!({"review_preparation_failure":{
            "exhausted":true,"reason":"ReviewModelConflict",
            "inputs":{"spec":r["spec"],"model":r["model"],"effort":r["effort"],
                "model_origin":r["model_origin"],"effort_origin":r["effort_origin"],
                "options":null,"subwork":null,"review_source_jobs":null,"task_type":null,"role":null}}});
    }).unwrap();
    f.dispatch();
    assert_eq!(entry(&f, 1)["reason"], "review_preparation_exhausted");
    assert_eq!(entry(&f, 1)["eligible"], false);
    assert!(entry(&f, 1)["root_excluded"].is_null());
    assert_eq!(f.e.backend.pick_count, 0);

    let mut f = Fixture::new();
    f.put(item(1, "other", "todo", &[]));
    f.e.backend.prepare_pressure = true;
    f.dispatch();
    let e = entry(&f, 1);
    assert_eq!(snapshot(&f)["capacity"]["allowance_now"], 1.);
    assert_eq!(e["reason"], "capacity_changed");
    assert_eq!(e["stage"], "recheck");
    assert_eq!(e["recheck_capacity"]["allowance_now"], 0.);
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn explain_skips_bodies_keeps_files_and_reports_changed_expired_missing_and_corrupt() {
    let mut f = Fixture::new();
    f.put(item(4091, "other", "todo", &[]));
    f.e.edit(&json!(4091), |r| {
        r["note"] = json!({"secret_body":"NOTE_BODY_SENTINEL"});
        r["Source"] = json!({"payload":"SOURCE_BODY_SENTINEL"});
        r["scheduler"] = json!({"owner_instruction":"INSTRUCTION_BODY_SENTINEL"});
    })
    .unwrap();
    f.e.backend.m["cpu_percent"] = json!(99.);
    f.dispatch();
    // An exclusive handle makes even opening the spec fail; explain still succeeds.
    let row = f.get(4091);
    let _spec_lock = fs::OpenOptions::new()
        .read(true)
        .share_mode(0)
        .open(s(&row["spec"]))
        .unwrap();
    let paths = [
        f.e.todo_path.clone(),
        f.e.root.join("admission.json"),
        f.e.root.join("state.json"),
    ];
    let before: Vec<_> = paths.iter().map(|p| fs::read(p).ok()).collect();
    let result = admission::explain(&f.e.root, &f.e.todo_path, 4091, T);
    assert_eq!(result["validity"], "matching_task_metadata");
    assert_eq!(result["observation"]["entry"]["reason"], "capacity");
    for sentinel in [
        "NOTE_BODY_SENTINEL",
        "SOURCE_BODY_SENTINEL",
        "INSTRUCTION_BODY_SENTINEL",
        "spec-4091",
    ] {
        assert!(!result.to_string().contains(sentinel));
        assert!(!String::from_utf8(fs::read(&paths[1]).unwrap())
            .unwrap()
            .contains(sentinel));
    }
    assert_eq!(
        paths.iter().map(|p| fs::read(p).ok()).collect::<Vec<_>>(),
        before
    );
    assert_eq!(
        admission::explain(&f.e.root, &f.e.todo_path, 4091, T + TTL + 1.)["validity"],
        "expired"
    );
    f.e.edit(&json!(4091), |r| r["wait_on"] = json!([999]))
        .unwrap();
    assert_eq!(
        admission::explain(&f.e.root, &f.e.todo_path, 4091, T)["validity"],
        "metadata_changed"
    );
    assert_eq!(
        admission::explain(&f.e.root, &f.e.todo_path, 4091, T - 1.)["validity"],
        "clock_mismatch"
    );
    fs::write(&paths[1], r#"{"schema":1,"note":"CORRUPT_BODY_SENTINEL"}"#).unwrap();
    let result = admission::explain(&f.e.root, &f.e.todo_path, 4091, T);
    assert_eq!(result["validity"], "corrupt_or_unreadable");
    assert!(!result.to_string().contains("CORRUPT_BODY_SENTINEL"));
    let absent = f.temp.path().join("must-not-be-created");
    assert_eq!(
        admission::explain(&absent, &f.e.todo_path, 4091, T)["validity"],
        "missing"
    );
    assert!(!absent.exists());
}

#[test]
fn bounded_atomic_observations_and_targeted_dispatch_never_replace_ordinary_results() {
    let f = Fixture::new();
    let mut observation = Observation::new(T, true, true);
    for id in 1..=(MAX_ENTRIES + 2) {
        observation.item(
            &json!({"id":id,"status":"todo","note":"NOT_RECORDED"}),
            &Value::Null,
        );
    }
    assert_eq!(observation.entries.len(), MAX_ENTRIES);
    assert!(observation.truncated);
    observation.publish(&f.e.root).unwrap();
    let path = f.e.root.join("admission.json");
    assert!(fs::metadata(&path).unwrap().len() <= MAX_BYTES);
    let mut old = fs::File::open(&path).unwrap();
    let first = fs::read(&path).unwrap();
    let mut next = Observation::new(T + 1., true, true);
    next.item(&json!({"id":4091}), &Value::Null);
    next.decide(&json!(4091), Stage::Filtering, Reason::RelatedOwner);
    next.publish(&f.e.root).unwrap();
    use std::io::Read;
    let mut retained = vec![];
    old.read_to_end(&mut retained).unwrap();
    assert_eq!(retained, first);
    assert_ne!(fs::read(&path).unwrap(), first);
    let good = fs::read(&path).unwrap();
    Observation::new(T + 2., false, true)
        .publish(&f.e.root)
        .unwrap();
    assert_eq!(fs::read(&path).unwrap(), good);
    next.entries.get_mut(&4091).unwrap().metadata_fingerprint = "x".repeat(MAX_BYTES as usize);
    assert!(next.publish(&f.e.root).is_err());
    assert_eq!(fs::read(&path).unwrap(), good);
}

#[test]
fn unrelated_recovered_job_references_do_not_hide_a_valid_target_observation() {
    let mut f = Fixture::new();
    f.put(item(4091, "other", "todo", &[]));
    let mut recovered = item(2, "other", "todo", &[]);
    recovered["jobs"] = json!([{"id":"legacy-recovered","Source":"BODY_MUST_BE_SKIPPED"}]);
    f.put(recovered);
    f.e.backend.m["cpu_percent"] = json!(99.);
    f.dispatch();
    let result = admission::explain(&f.e.root, &f.e.todo_path, 4091, T);
    assert_eq!(result["validity"], "matching_task_metadata");
    assert_eq!(result["observation"]["entry"]["reason"], "capacity");
    assert!(!result.to_string().contains("BODY_MUST_BE_SKIPPED"));
    // An unsupported shape on the selected row remains unknown rather than
    // claiming that null and an arbitrary object are identical metadata.
    assert_eq!(
        admission::explain(&f.e.root, &f.e.todo_path, 2, T)["validity"],
        "metadata_unavailable"
    );
}

#[test]
fn reservation_object_shape_changes_invalidate_the_recorded_metadata() {
    let mut f = Fixture::new();
    let mut row = item(4091, "other", "todo", &[]);
    row["stall_recovery"] = json!({"pending":null});
    f.put(row);
    f.e.backend.m["cpu_percent"] = json!(99.);
    f.dispatch();
    let mut db = f.e.todo().unwrap();
    // The normal predicate tests is_object(), including an empty object.
    db["items"][0]["stall_recovery"]["pending"] = json!({});
    write_json(&f.e.todo_path, &db, 1).unwrap();
    assert_eq!(
        admission::explain(&f.e.root, &f.e.todo_path, 4091, T)["validity"],
        "metadata_changed"
    );
}

#[test]
fn normal_edits_publish_matching_result_metadata_and_retain_the_original_input() {
    for gate in ["quota", "failure", "capacity", "launch"] {
        let mut f = Fixture::new();
        let mut row = item(4091, "other", "todo", &[]);
        row["model"] = json!("gpt-6.1-sol");
        f.put(row);
        match gate {
            "quota" => f.e.backend.m["quota_block_model"] = json!("gpt-6.1-sol"),
            "failure" => f.e.backend.worktree_failure = Some(4091),
            "capacity" => f.e.backend.prepare_pressure = true,
            _ => {}
        }
        let _ = f.e.dispatch(&f.cfg, &f.pri);
        let result = admission::explain(&f.e.root, &f.e.todo_path, 4091, T);
        assert_eq!(result["validity"], "matching_task_metadata", "{gate}");
        assert_ne!(
            result["observation"]["entry"]["input_metadata_fingerprint"],
            result["metadata_fingerprint"],
            "{gate}"
        );
        assert_eq!(
            result["current_metadata_fingerprint"],
            result["metadata_fingerprint"]
        );
    }
}

#[test]
fn all_normal_input_fields_invalidate_metadata_while_bodies_are_presence_only() {
    for (path, value) in [
        ("scheduler.last_stop", json!("resource_wait")),
        ("decision_wait", json!(true)),
        ("owner_question_wait", json!(true)),
        ("product_review_allocation", json!({"generation":2})),
        ("sandbox", json!("read-only")),
        ("mcp_profile", json!("none")),
        ("network", json!(true)),
        ("add_dirs", json!(["extra"])),
        ("review_source_jobs", json!(["author"])),
        ("scheduler.workspace_receipt", json!({"head":"changed"})),
        (
            "scheduler.console_request",
            json!({"options":{"model":"changed"}}),
        ),
        (
            "scheduler.console_fresh_request",
            json!({"request_id":"changed"}),
        ),
        ("scheduler.unstarted_review", json!({"generation":2})),
        ("scheduler.initial_review", json!({"generation":2})),
        (
            "scheduler.review_preparation_failure.inputs.options",
            json!({"sandbox":"read-only"}),
        ),
    ] {
        let mut f = Fixture::new();
        f.put(item(4091, "other", "todo", &[]));
        f.e.backend.m["cpu_percent"] = json!(99.);
        f.dispatch();
        let mut db = f.e.todo().unwrap();
        let parts: Vec<_> = path.split('.').collect();
        let mut at = &mut db["items"][0];
        for part in &parts[..parts.len() - 1] {
            if !at[*part].is_object() {
                at[*part] = json!({});
            }
            at = &mut at[*part];
        }
        at[parts[parts.len() - 1]] = value;
        write_json(&f.e.todo_path, &db, 1).unwrap();
        assert_eq!(
            admission::explain(&f.e.root, &f.e.todo_path, 4091, T)["validity"],
            "metadata_changed",
            "{path}"
        );
    }
    let mut f = Fixture::new();
    let mut row = item(4091, "other", "todo", &[]);
    row["title"] = json!("TITLE_A");
    row["scheduler"] = json!({"console_request":{"prompt":"PROMPT_A"}});
    f.put(row);
    f.e.backend.m["cpu_percent"] = json!(99.);
    f.dispatch();
    let mut db = f.e.todo().unwrap();
    db["items"][0]["title"] = json!("TITLE_B");
    db["items"][0]["note"] = json!("NOTE_EDIT");
    db["items"][0]["updated"] = json!("a note-only revision");
    db["items"][0]["scheduler"]["console_request"]["prompt"] = json!("PROMPT_B");
    write_json(&f.e.todo_path, &db, 1).unwrap();
    let result = admission::explain(&f.e.root, &f.e.todo_path, 4091, T);
    assert_eq!(result["validity"], "matching_task_metadata");
    for body in ["TITLE_A", "TITLE_B", "PROMPT_A", "PROMPT_B", "NOTE_EDIT"] {
        assert!(!result.to_string().contains(body));
    }
}

#[test]
fn every_explanation_validity_uses_the_same_top_level_schema() {
    let mut f = Fixture::new();
    f.put(item(4091, "other", "todo", &[]));
    f.e.backend.m["cpu_percent"] = json!(99.);
    f.dispatch();
    let current = admission::explain(&f.e.root, &f.e.todo_path, 4091, T);
    let expected: std::collections::BTreeSet<_> =
        current.as_object().unwrap().keys().cloned().collect();
    let missing = admission::explain(&f.temp.path().join("absent"), &f.e.todo_path, 4091, T);
    let expired = admission::explain(&f.e.root, &f.e.todo_path, 4091, T + TTL + 1.);
    fs::write(f.e.root.join("admission.json"), b"broken").unwrap();
    let corrupt = admission::explain(&f.e.root, &f.e.todo_path, 4091, T);
    for result in [missing, expired, corrupt] {
        assert_eq!(
            result
                .as_object()
                .unwrap()
                .keys()
                .cloned()
                .collect::<std::collections::BTreeSet<_>>(),
            expected,
            "{}",
            result["validity"]
        );
    }
}

#[test]
fn observation_errors_keep_scan_and_launch_results_separate() {
    let mut f = Fixture::new();
    f.put(item(4091, "other", "todo", &[]));
    f.e.backend.m["measure_error"] = json!(true);
    assert!(f.e.dispatch(&f.cfg, &f.pri).is_err());
    let result = admission::explain(&f.e.root, &f.e.todo_path, 4091, T);
    assert_eq!(result["validity"], "incomplete");
    assert_eq!(result["incomplete"], true);
    assert_eq!(result["dispatch_error"], "tool_failed");
    assert!(result["observation"].is_null());

    let mut f = Fixture::new();
    f.put(item(4091, "other", "todo", &[]));
    f.e.backend.m["jobs_error_after_launch"] = json!(true);
    assert!(f.e.dispatch(&f.cfg, &f.pri).is_err());
    assert_eq!(f.e.backend.launched.len(), 1);
    assert_eq!(entry(&f, 4091)["reason"], "launched_unlinked");
    assert_eq!(entry(&f, 4091)["stage"], "launch");
    assert_eq!(entry(&f, 4091)["failure_code"], "tool_failed");
    assert!(entry(&f, 4091)["preparation_code"].is_null());
}

#[test]
fn real_targeted_path_preserves_the_ordinary_snapshot_and_reader_cannot_write_inputs() {
    let mut f = Fixture::new();
    f.put(item(4091, "other", "todo", &[]));
    f.e.backend.m["cpu_percent"] = json!(99.);
    f.dispatch();
    let before = fs::read(f.e.root.join("admission.json")).unwrap();
    f.e.dispatch_for(&f.cfg, &f.pri, Some(4091)).unwrap();
    assert_eq!(fs::read(f.e.root.join("admission.json")).unwrap(), before);
    for name in ["state.json", "status.json", "config.json", "scheduler.log"] {
        fs::write(f.e.root.join(name), b"CONTROL_SENTINEL").unwrap();
    }
    let paths = [
        f.e.todo_path.clone(),
        f.e.root.join("admission.json"),
        f.e.root.join("state.json"),
        f.e.root.join("status.json"),
        f.e.root.join("config.json"),
        f.e.root.join("scheduler.log"),
    ];
    let _locks: Vec<_> = paths
        .iter()
        .map(|p| {
            fs::OpenOptions::new()
                .read(true)
                .share_mode(1)
                .open(p)
                .unwrap()
        })
        .collect();
    let inventory = || {
        let mut names = fs::read_dir(&f.e.root)
            .unwrap()
            .map(|e| e.unwrap().file_name())
            .collect::<Vec<_>>();
        names.sort();
        names
    };
    let before = inventory();
    assert_eq!(
        admission::explain(&f.e.root, &f.e.todo_path, 4091, T)["validity"],
        "matching_task_metadata"
    );
    assert_eq!(inventory(), before);
}

#[test]
fn bom_and_an_unrelated_non_string_project_do_not_change_the_control_projection() {
    let mut f = Fixture::new();
    f.put(item(4091, "other", "todo", &[]));
    let mut db = f.e.todo().unwrap();
    db["projects"]["unrelated"] = json!({"Source":"DO_NOT_KEEP"});
    write_json(&f.e.todo_path, &db, 1).unwrap();
    f.e.backend.m["cpu_percent"] = json!(99.);
    f.dispatch();
    let mut bytes = vec![0xef, 0xbb, 0xbf];
    bytes.extend(fs::read(&f.e.todo_path).unwrap());
    fs::write(&f.e.todo_path, bytes).unwrap();
    let result = admission::explain(&f.e.root, &f.e.todo_path, 4091, T);
    assert_eq!(result["validity"], "matching_task_metadata");
    assert!(!result.to_string().contains("DO_NOT_KEEP"));
}

#[test]
fn saved_console_request_schema_is_metadata_only_and_supported() {
    let mut f = Fixture::new();
    let mut row = item(4091, "other", "todo", &[]);
    row["scheduler"] = json!({"console_request":{"key":"manual:finite","launch_key":"key",
        "prompt":"PROMPT_MUST_NOT_BE_KEPT","read_only":true,
        "options":{"cwd":"worktree","model":"gpt-6.1-sol","effort":"max","sandbox":"read-only",
            "mcp_profile":"none","network":false,"add_dirs":[],"resume_session":null,
            "notes_hint":"NOTE_MUST_NOT_BE_KEPT"}}});
    f.put(row);
    f.e.backend.m["cpu_percent"] = json!(99.);
    f.dispatch();
    let result = admission::explain(&f.e.root, &f.e.todo_path, 4091, T);
    assert_eq!(result["validity"], "matching_task_metadata");
    for text in ["PROMPT_MUST_NOT_BE_KEPT", "NOTE_MUST_NOT_BE_KEPT"] {
        assert!(!result.to_string().contains(text));
        assert!(!snapshot(&f).to_string().contains(text));
    }
}
