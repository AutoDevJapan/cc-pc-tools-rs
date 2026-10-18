//! Offline scheduler checks; no real job launch, cancellation, Discord or credentials.
use cc_scheduler::{backend::*, common::*, engine::*, policy::*};
use serde_json::{json, Value};
use std::{
    fs,
    path::{Path, PathBuf},
    time::Duration,
};
const T: f64 = 100000.;
#[path = "../../cc-job-policy/tests/support/mod.rs"]
mod subwork_support;
#[path = "support/no_action.rs"]
mod no_action_tests;

#[test]
fn console_subwork_recovers_only_proven_idle_doing_retry_through_normal_gates() {
    for gate in [
        "open",
        "cpu",
        "memory",
        "deadline",
        "owner",
        "active",
        "unknown_cancel",
        "waiting",
        "unprepared_cpu",
    ] {
        let mut sub = subwork_support::Fixture::new();
        sub.register();
        let child = &mut sub.db["items"][1];
        child["status"] = json!("doing");
        child["jobs"] = json!(["previous"]);
        child["worker_receipt"] = json!({"job":null,"generation":2});
        child["scheduler"] = json!({"worktree":child["scheduler"]["worktree"],
            "branch":child["scheduler"]["branch"],"phase":"retry","resume":true,"job":null,"key":null,"last_job":"previous"});
        sub.jobs[0]["pid"] = json!(1);
        sub.jobs[0]["created_at"] = json!(T - 1200.);
        sub.jobs[0]["pid_created"] = proc(1, 0, "codex.exe")["created"].clone();
        sub.jobs
            .push(json!({"id":"previous","status":"cancelled","todo_id":2,
            "recovery_cancel":{"key":"repair","committed_at":"2026-10-12T07:00:00+09:00"}}));
        let mut f = Fixture::new();
        match gate {
            "unprepared_cpu" => {
                sub.db["items"][1]["scheduler"]["worktree"] = Value::Null;
                sub.db["items"][1]["scheduler"]["branch"] = Value::Null;
                f.e.backend.m["cpu_percent"] = json!(99);
            }
            "cpu" => f.e.backend.m["cpu_percent"] = json!(99),
            "memory" => f.e.backend.m["available_gb"] = json!(3),
            "deadline" => sub.db["items"][1]["scheduler"]["not_before"] = json!(T + 3600.),
            "owner" => sub.db["items"][1]["stopped"] = json!(true),
            "active" => sub
                .jobs
                .push(json!({"id":"other","status":"running","todo_id":2})),
            "unknown_cancel" => sub.jobs[1]["recovery_cancel"] = Value::Null,
            "waiting" => sub.db["items"][1]["wait_on"] = json!([1]),
            _ => {}
        }
        f.e.backend.records = sub.jobs.clone();
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let request = json!({"key":"manual:existing-review","launch_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            "prompt":"Original fixed review body.","read_only":true,"options":{"model":"gpt-6-luna","effort":"high","sandbox":"read-only"}});
        let result =
            f.e.dispatch_console_subwork(&f.cfg, &f.pri, 2, &request)
                .unwrap();
        assert_eq!(result.is_some(), gate == "open", "{gate}");
        assert_eq!(
            f.e.backend.launched.len(),
            usize::from(gate == "open"),
            "{gate}"
        );
        if let Some(job) = result {
            assert_eq!(f.get(2)["scheduler"]["console_request"], request);
            assert_eq!(f.get(2)["subwork"], sub.db["items"][1]["subwork"]);
            assert!(f.e.backend.launched[0]["text"]
                .as_str()
                .unwrap()
                .contains("最新保存仕様を優先"));
            assert_eq!(
                f.e.dispatch_console_subwork(&f.cfg, &f.pri, 2, &request)
                    .unwrap(),
                Some(job)
            );
            assert_eq!(f.e.backend.launched.len(), 1);
        } else if ["owner", "active", "unknown_cancel", "waiting"].contains(&gate) {
            assert_eq!(f.get(2)["status"], "doing", "{gate}");
        } else if gate == "unprepared_cpu" {
            assert_eq!(f.get(2)["status"], "todo");
            assert!(f.get(2)["scheduler"]["worktree"].is_null());
        }
    }
}

#[test]
fn console_subwork_admission_survives_registered_worker_releasing_reservation() {
    let mut sub = subwork_support::Fixture::new();
    sub.register();
    sub.db["items"][1]["scheduler"]
        .as_object_mut()
        .unwrap()
        .remove("subwork_admission");
    sub.jobs[0]["pid"] = json!(1);
    sub.jobs[0]["created_at"] = json!(T - 1200.);
    sub.jobs[0]["pid_created"] = proc(1, 0, "codex.exe")["created"].clone();
    let mut f = Fixture::new();
    f.e.backend.records = sub.jobs.clone();
    f.e.backend.launch_start = Some(f.e.todo_path.clone());
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    let request = json!({"key":"manual:finite-check","launch_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "prompt":"Inspect the same fixed change."});
    let result =
        f.e.dispatch_console_subwork(&f.cfg, &f.pri, 2, &request)
            .unwrap()
            .unwrap();
    let row = f.get(2);
    assert!(row["scheduler"]["key"].is_null());
    assert_eq!(row["scheduler"]["job"], result);
    assert_eq!(row["worker_receipt"]["job"], result);
    assert!(row["jobs"].as_array().unwrap().contains(&json!(result)));
    let mut candidate = sub.child_job();
    candidate["id"] = json!(result);
    cc_job_policy::subwork::refresh_job(&f.e.todo().unwrap(), &sub.jobs, &mut candidate).unwrap();
    assert_eq!(candidate["parent_job_id"], "parent");
    assert_eq!(
        f.e.dispatch_console_subwork(&f.cfg, &f.pri, 2, &request)
            .unwrap(),
        Some(result)
    );
    assert_eq!(f.e.backend.launched.len(), 1);
}

#[test]
fn console_subwork_uses_normal_admission_and_recovers_lost_response() {
    for gate in [
        "open", "cargo", "cpu", "memory", "interval", "deadline", "owner",
    ] {
        let mut sub = subwork_support::Fixture::new();
        sub.register();
        sub.db["items"][1]["needs"] = json!(["cargo"]);
        sub.db["items"][1]["scheduler"]
            .as_object_mut()
            .unwrap()
            .remove("subwork_admission");
        sub.jobs[0]["pid"] = json!(1);
        sub.jobs[0]["created_at"] = json!(T - 1200.);
        sub.jobs[0]["pid_created"] = proc(1, 0, "codex.exe")["created"].clone();
        let mut f = Fixture::new();
        f.e.backend.records = sub.jobs.clone();
        match gate {
            "cargo" => {
                f.e.backend.ps.insert(2, proc(2, 0, "cargo.exe"));
            }
            "cpu" => f.e.backend.m["cpu_percent"] = json!(99),
            "memory" => f.e.backend.m["available_gb"] = json!(3),
            "interval" => f.e.state["last_launch"] = json!(T),
            "deadline" => sub.db["items"][1]["scheduler"]["not_before"] = json!(T + 3600.),
            "owner" => sub.db["items"][1]["status"] = json!("block"),
            _ => {}
        }
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let request = json!({"key":"manual:finite-check","launch_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            "prompt":"Keep the fixed input and the saved acceptance criteria."});
        let result =
            f.e.dispatch_console_subwork(&f.cfg, &f.pri, 2, &request)
                .unwrap();
        assert_eq!(result.is_some(), gate == "open", "{gate}");
        assert_eq!(
            f.e.backend.launched.len(),
            usize::from(gate == "open"),
            "{gate}"
        );
        if gate != "open" {
            continue;
        }
        let row = f.get(2);
        assert_eq!(row["spec"], sub.db["items"][1]["spec"]);
        assert_eq!(row["scheduler"]["console_request"], request);
        assert_eq!(
            row["scheduler"]["key"],
            sub.db["items"][1]["scheduler"]["key"]
        );
        let mut candidate = sub.child_job();
        candidate["id"] = row["scheduler"]["job"].clone();
        candidate["subwork_spec"] = row["spec"].clone();
        candidate["assigned_needs"] = row["needs"].clone();
        // This is the admission validator used by cc-jobs, not merely an argv assertion.
        cc_job_policy::subwork::refresh_job(&f.e.todo().unwrap(), &sub.jobs, &mut candidate)
            .unwrap();
        assert!(f.e.backend.launched[0]["text"]
            .as_str()
            .unwrap()
            .contains(request["prompt"].as_str().unwrap()));
        assert!(f.e.backend.launched[0]["text"]
            .as_str()
            .unwrap()
            .contains("Inspect the fixed change"));
        let invocation = RealBackend::new(f.e.root.clone()).launch_command(
            &row,
            Path::new(row["scheduler"]["prompt"].as_str().unwrap()),
            &row["scheduler"]["selection"],
        );
        assert!(invocation
            .args
            .windows(2)
            .any(|w| w == ["--assigned-needs", "cargo"]));
        let again =
            f.e.dispatch_console_subwork(&f.cfg, &f.pri, 2, &request)
                .unwrap();
        assert_eq!(again, result);
        assert_eq!(f.e.backend.launched.len(), 1);
        let mut changed = request.clone();
        changed["prompt"] = json!("Different accepted body");
        assert!(f
            .e
            .dispatch_console_subwork(&f.cfg, &f.pri, 2, &changed)
            .is_err());
    }
}

#[test]
fn console_subwork_keeps_distinct_bodies_and_recovers_after_launcher_crash() {
    let mut sub = subwork_support::Fixture::new();
    sub.register();
    sub.jobs[0]["pid"] = json!(1);
    sub.jobs[0]["created_at"] = json!(T - 1200.);
    sub.jobs[0]["pid_created"] = proc(1, 0, "codex.exe")["created"].clone();
    let mut f = Fixture::new();
    f.e.backend.records = sub.jobs.clone();
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    let first = json!({"key":"manual:first","launch_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","prompt":"First body",
        "options":{"model":"gpt-6-luna","effort":"medium"}});
    let second = json!({"key":"manual:second","launch_key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","prompt":"Second body"});
    f.e.backend.m["cpu_percent"] = json!(99);
    assert_eq!(
        f.e.dispatch_console_subwork(&f.cfg, &f.pri, 2, &first)
            .unwrap(),
        None
    );
    assert_eq!(
        f.e.dispatch_console_subwork(&f.cfg, &f.pri, 2, &second)
            .unwrap(),
        None
    );
    assert_eq!(f.get(2)["scheduler"]["console_request"], first);
    f.e.backend.m = measurement();
    f.e.backend.clock += n(&f.cfg["cpu_window_s"]) + 1.;
    f.e.backend.crash_after_launch = true;
    assert!(f
        .e
        .dispatch_console_subwork(&f.cfg, &f.pri, 2, &first)
        .is_err());
    assert_eq!(f.e.backend.launched.len(), 1);
    let recovered =
        f.e.dispatch_console_subwork(&f.cfg, &f.pri, 2, &first)
            .unwrap()
            .unwrap();
    let key = f.get(2)["scheduler"]["key"].clone();
    let job =
        f.e.backend
            .records
            .iter()
            .find(|j| j["id"] == recovered)
            .unwrap()
            .clone();
    f.e.link(&json!(2), &job, &key).unwrap();
    assert_eq!(
        f.e.dispatch_console_subwork(&f.cfg, &f.pri, 2, &second)
            .unwrap(),
        None
    );
    f.e.backend
        .records
        .iter_mut()
        .find(|j| j["id"] == recovered)
        .unwrap()["status"] = json!("succeeded");
    let previous_cwd = f.get(2)["scheduler"]["worktree"].clone();
    let previous =
        f.e.backend
            .records
            .iter_mut()
            .find(|j| j["id"] == recovered)
            .unwrap();
    previous["cwd"] = previous_cwd;
    previous["thread_id"] = json!("11111111-1111-4111-8111-111111111111");
    previous["last_message"] = json!("状態: 継続中\nFirst body");
    f.e.requeue(&json!(2), &json!(recovered), "failed").unwrap();
    f.e.backend.clock += 3600.;
    assert!(f
        .e
        .dispatch_console_subwork(&f.cfg, &f.pri, 2, &second)
        .unwrap()
        .is_some());
    assert_eq!(f.e.backend.launched.len(), 2);
    assert_eq!(
        f.e.backend.launched[1]["item"]["scheduler"]["selection"]["model"],
        "mock-model"
    );
    assert!(f.e.backend.launched[1]["text"]
        .as_str()
        .unwrap()
        .contains("Second body"));
    assert!(!f.e.backend.launched[1]["text"]
        .as_str()
        .unwrap()
        .contains("First body"));
    assert!(f.e.backend.launched[1]["item"]["scheduler"]["resume_session"].is_null());
    // Replaying the first receipt still identifies its own job after later work.
    assert_eq!(
        f.e.dispatch_console_subwork(&f.cfg, &f.pri, 2, &first)
            .unwrap(),
        Some(recovered)
    );
    assert_eq!(f.e.backend.launched.len(), 2);
    assert_eq!(
        f.get(2)["scheduler"]["console_receipts"]
            .as_object()
            .unwrap()
            .len(),
        2
    );
}

#[test]
fn console_subwork_preserves_read_only_and_model_preferences_without_widening_todo_scope() {
    for explicit_todo in [false, true] {
        let mut sub = subwork_support::Fixture::new();
        sub.register();
        sub.jobs[0]["pid"] = json!(1);
        sub.jobs[0]["created_at"] = json!(T - 1200.);
        sub.jobs[0]["pid_created"] = proc(1, 0, "codex.exe")["created"].clone();
        if explicit_todo {
            sub.db["items"][1]["model"] = json!("gpt-6.1-sol");
            sub.db["items"][1]["effort"] = json!("high");
        }
        let mut f = Fixture::new();
        f.e.backend.records = sub.jobs.clone();
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let request = json!({"key":"manual:restricted","launch_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            "prompt":"Read only the fixed change.","read_only":true,
            "options":{"model":"gpt-6-luna","effort":"medium","sandbox":"read-only"}});
        f.e.dispatch_console_subwork(&f.cfg, &f.pri, 2, &request)
            .unwrap()
            .unwrap();
        let prepared = &f.e.backend.launched[0]["item"];
        let selection = &prepared["scheduler"]["selection"];
        assert_eq!(
            selection["model"],
            if explicit_todo {
                "gpt-6.1-sol"
            } else {
                "gpt-6-luna"
            }
        );
        assert_eq!(
            selection["effort"],
            if explicit_todo { "high" } else { "medium" }
        );
        let invocation = RealBackend::new(f.e.root.clone()).launch_command(
            prepared,
            Path::new(prepared["scheduler"]["prompt"].as_str().unwrap()),
            selection,
        );
        assert!(invocation
            .args
            .windows(2)
            .any(|w| w == ["--sandbox", "read-only"]));
        assert!(invocation
            .args
            .windows(2)
            .any(|w| w == ["--model", selection["model"].as_str().unwrap()]));
        assert!(!invocation.args.iter().any(|arg| arg == "--auto-model"));
        assert_eq!(prepared["spec"], sub.db["items"][1]["spec"]);
    }
}

#[test]
fn subwork_dispatch_keeps_cargo_cpu_memory_and_interval_gates() {
    for gate in ["open", "cargo", "cpu", "memory", "interval"] {
        let mut sub = subwork_support::Fixture::new();
        sub.register();
        sub.db["items"][1]["needs"] = json!(["cargo"]);
        sub.db["items"][1]["scheduler"]
            .as_object_mut()
            .unwrap()
            .remove("subwork_admission");
        sub.jobs[0]["pid"] = json!(1);
        sub.jobs[0]["created_at"] = json!(T - 1200.);
        sub.jobs[0]["pid_created"] = proc(1, 0, "codex.exe")["created"].clone();
        let mut f = Fixture::new();
        f.e.backend.records = sub.jobs.clone();
        match gate {
            "cargo" => {
                f.e.backend.ps.insert(2, proc(2, 0, "cargo.exe"));
            }
            "cpu" => f.e.backend.m["cpu_percent"] = json!(99),
            "memory" => f.e.backend.m["available_gb"] = json!(3),
            "interval" => f.e.state["last_launch"] = json!(T),
            _ => {}
        }
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        f.e.dispatch(&f.cfg, &f.pri).unwrap();
        assert_eq!(
            f.e.backend.launched.len(),
            usize::from(gate == "open"),
            "{gate}: {:?}",
            f.e.report
        );
        assert!(f.get(2)["wait_on"].is_null());
        assert_eq!(
            f.get(2)["scheduler"]["subwork_admission"].is_object(),
            gate == "open"
        );
        if gate == "open" {
            assert_eq!(
                f.e.backend.launched[0]["item"]["scheduler"]["subwork_parent_job"],
                "parent"
            );
        }
    }
}

#[test]
fn subwork_launch_arguments_carry_relation_and_resource_allocation() {
    let mut sub = subwork_support::Fixture::new();
    sub.register();
    let mut item = sub.db["items"][1].clone();
    item["needs"] = json!(["cargo"]);
    item["scheduler"]["subwork_parent_job"] = json!("parent");
    let backend = RealBackend::new(sub.dir.path().to_path_buf());
    let c = backend.launch_command(
        &item,
        Path::new("prompt.md"),
        &json!({"task_type":"check","model":"gpt-6-luna","effort":"high"}),
    );
    for pair in [
        ["--subwork-of", "1"],
        ["--parent-job-id", "parent"],
        ["--assigned-needs", "cargo"],
    ] {
        assert!(c.args.windows(2).any(|w| w == pair));
    }
}

#[test]
fn subwork_prepare_does_not_launch_stale_spec_needs_relation_or_owner_stop() {
    for field in ["spec", "needs", "subwork", "status"] {
        let mut sub = subwork_support::Fixture::new();
        sub.register();
        let mut changed = sub.db.clone();
        changed["items"][1][field] = match field {
            "spec" => json!("another-spec.md"),
            "needs" => json!(["cargo"]),
            "subwork" => {
                let mut rel = changed["items"][1]["subwork"].clone();
                rel["kind"] = json!("research");
                rel
            }
            _ => json!("block"),
        };
        let mut f = Fixture::new();
        f.e.backend.prepare_edit = Some((f.e.todo_path.clone(), changed));
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        f.e.dispatch(&f.cfg, &f.pri).unwrap();
        assert!(f.e.backend.launched.is_empty(), "{field}");
    }
}

#[test]
fn reboot_deployment_preserves_installed_subscription_model_validation() {
    for model in [
        "gpt-6.1-sol",
        "anthropic/claude-sonnet-5-5",
        "anthropic/claude-haiku-5-5",
    ] {
        assert!(valid_model(model), "{model}");
    }
    for model in [
        "anthropic/",
        "anthropic/../model",
        "anthropic/a/b",
        "-model",
        "model --flag",
    ] {
        assert!(!valid_model(model), "{model}");
    }
    assert!(!valid_model(&format!("anthropic/{}", "a".repeat(71))));
}

#[test]
fn reboot_launch_reservation_is_not_marked_lost_between_spawn_retries() {
    let mut job = json!({"status":"queued","created_at":1,
        "queue_restart_pending":true,"reboot_launch_pending":true});
    assert!(alive(&job, &Processes::new(), T));
    job["cancel_requested"] = json!(true);
    assert!(!alive(&job, &Processes::new(), T));
    job["cancel_requested"] = json!(false);
    job["status"] = json!("cancelled");
    assert!(!alive(&job, &Processes::new(), T));
    job["status"] = json!("lost");
    assert!(!alive(&job, &Processes::new(), T));
}

fn cargo_gate_fixture() -> (tempfile::TempDir, PathBuf, PathBuf) {
    let temp = tempfile::tempdir().unwrap();
    let home = temp.path().join("civ6-live");
    let gate = temp.path().join("codex-jobs");
    fs::create_dir_all(&home).unwrap();
    fs::create_dir_all(&gate).unwrap();
    fs::write(gate.join("cargo_gate.exe"), b"fixture, never executed").unwrap();
    (temp, home, gate)
}

#[test]
fn cargo_gate_admission_uses_build_slots_not_worker_lifetime() {
    let (_temp, home, gate) = cargo_gate_fixture();
    let mut worker = item(1, "tools", "doing", &["cargo", "civ6"]);
    worker["jobs"] = json!(["j1"]);
    let jobs = vec![job("j1", 1, "queued")];
    let processes = ps(vec![proc(1, 0, "codex.exe")]);
    let busy = resources_busy_with_lock(&[worker], &jobs, &processes, T, &home).unwrap();
    assert!(!busy.contains("cargo"));
    assert!(busy.contains("civ6"));
    // Admission probes release every lock; the real gate can immediately acquire them.
    for slot in 0..3 {
        assert!(ByteLock::acquire(
            &gate.join(format!("cargo-build.lock.{slot}")),
            Duration::ZERO
        )
        .unwrap()
        .is_some());
    }
}

#[test]
fn cargo_gate_admission_allows_three_targets_and_waits_when_full() {
    let (_temp, home, gate) = cargo_gate_fixture();
    let processes = ps(vec![
        proc(10, 0, "cargo_gate.exe"),
        proc(11, 10, "cargo.exe"),
        proc(12, 11, "rustc.exe"),
    ]);
    let mut held = Vec::new();
    for slot in 0..3 {
        held.push(
            ByteLock::acquire(
                &gate.join(format!("cargo-build.lock.{slot}")),
                Duration::ZERO,
            )
            .unwrap()
            .unwrap(),
        );
        let busy = resources_busy_with_lock(&[], &[], &processes, T, &home).unwrap();
        assert_eq!(busy.contains("cargo"), slot == 2);
    }
    held.pop();
    assert!(!resources_busy_with_lock(&[], &[], &processes, T, &home)
        .unwrap()
        .contains("cargo"));
}

#[test]
fn cargo_gate_admission_recognizes_the_outer_cargo_relay() {
    let (_temp, home, gate) = cargo_gate_fixture();
    let processes = ps(vec![
        proc(9, 0, "cargo.exe"),
        proc(10, 9, "cargo_gate.exe"),
        proc(11, 10, "cargo.exe"),
        proc(12, 11, "rustc.exe"),
    ]);
    assert!(!resources_busy_with_lock(&[], &[], &processes, T, &home)
        .unwrap()
        .contains("cargo"));
    let held: Vec<_> = (0..3)
        .map(|slot| {
            ByteLock::acquire(
                &gate.join(format!("cargo-build.lock.{slot}")),
                Duration::ZERO,
            )
            .unwrap()
            .unwrap()
        })
        .collect();
    assert!(resources_busy_with_lock(&[], &[], &processes, T, &home)
        .unwrap()
        .contains("cargo"));
    drop(held);
}

#[test]
fn cargo_gate_relay_requires_a_current_direct_child_and_keeps_ungated_compilers_closed() {
    let (_temp, home, _gate) = cargo_gate_fixture();
    for invalid in [
        "reused",
        "unknown_parent",
        "unknown_child",
        "unrelated",
        "compiler",
        "sibling",
    ] {
        let mut relay = proc(9, 0, "cargo.exe");
        let mut gate = proc(10, 9, "cargo_gate.exe");
        let mut extra = Vec::new();
        match invalid {
            "reused" => gate["created"] = json!(relay["created"].as_u64().unwrap() - 1),
            "unknown_parent" => relay["created"] = Value::Null,
            "unknown_child" => gate["created"] = Value::Null,
            "unrelated" => gate["ppid"] = json!(8),
            "compiler" => relay["name"] = json!("rustc.exe"),
            "sibling" => extra.push(proc(13, 9, "rustc.exe")),
            _ => unreachable!(),
        }
        let mut rows = vec![relay, gate];
        rows.extend(extra);
        assert!(
            resources_busy_with_lock(&[], &[], &ps(rows), T, &home)
                .unwrap()
                .contains("cargo"),
            "{invalid}"
        );
    }
}

#[test]
fn cargo_gate_admission_keeps_unmanaged_builds_and_probe_errors_closed() {
    let (_temp, home, _gate) = cargo_gate_fixture();
    for name in ["cargo.exe", "rustc.exe"] {
        assert!(
            resources_busy_with_lock(&[], &[], &ps(vec![proc(11, 0, name)]), T, &home)
                .unwrap()
                .contains("cargo")
        );
    }
    let (_error_temp, error_home, error_gate) = cargo_gate_fixture();
    fs::create_dir(error_gate.join("cargo-build.lock.0")).unwrap();
    let busy = resources_busy_with_lock(&[], &[], &Processes::new(), T, &error_home).unwrap();
    assert_eq!(busy, ["cargo".to_string()].into_iter().collect());
}
#[test]
fn cargo_gate_probe_rereads_dynamic_ceiling_and_keeps_legacy_installations() {
    let (_temp, home, gate) = cargo_gate_fixture();
    let held: Vec<_> = (0..3)
        .map(|slot| {
            ByteLock::acquire(
                &gate.join(format!("cargo-build.lock.{slot}")),
                Duration::ZERO,
            )
            .unwrap()
            .unwrap()
        })
        .collect();
    let busy = || {
        resources_busy_with_lock(&[], &[], &Processes::new(), T, &home)
            .unwrap()
            .contains("cargo")
    };
    assert!(busy());
    if std::thread::available_parallelism().unwrap().get() >= 4 {
        fs::write(
            gate.join(cc_job_policy::cargo::CONFIG_FILE),
            r#"{"max_slots":4}"#,
        )
        .unwrap();
        assert!(!busy());
    }
    fs::write(
        gate.join(cc_job_policy::cargo::CONFIG_FILE),
        r#"{"max_slots":2}"#,
    )
    .unwrap();
    assert!(busy());
    fs::write(gate.join(cc_job_policy::cargo::CONFIG_FILE), "{invalid").unwrap();
    assert!(busy());
    fs::write(
        gate.join(cc_job_policy::cargo::CONFIG_FILE),
        r#"{"max_slots":4,"pause_at_cpu_percent":50}"#,
    )
    .unwrap();
    assert!(
        busy(),
        "scheduler must reject the same invalid thresholds as the gate"
    );
    drop(held);
}

#[test]
fn cargo_gate_admission_probe_failure_still_dispatches_unrelated_work() {
    let mut fixture = Fixture::new();
    let gate = fixture.e.root.parent().unwrap().join("codex-jobs");
    fs::create_dir_all(&gate).unwrap();
    fs::write(gate.join("cargo_gate.exe"), b"fixture, never executed").unwrap();
    fs::create_dir(gate.join("cargo-build.lock.0")).unwrap();
    fixture.put(item(1, "other", "todo", &["cargo"]));
    fixture.put(item(2, "other", "todo", &[]));
    fixture.e.run("step", false).unwrap();
    assert_eq!(fixture.get(1)["status"], "todo");
    assert_eq!(fixture.get(2)["status"], "doing");
    assert_eq!(fixture.e.backend.launched.len(), 1);
    assert!(fixture.e.failures.is_empty());
    assert!(fixture.e.root.join("state.json").is_file());
}

#[test]
fn shared_worker_result_does_not_finish_parent_or_copy_child_question() {
    for result in [
        "状態: 完了\n子の作業だけ完了しました。",
        "状態: 要判断\n公開してよいですか。\na. 公開\nb. 中止",
    ] {
        let mut f = Fixture::new();
        f.managed(1, 1, "civ6", 0);
        let mut parent = item(2, "civ6", "doing", &[]);
        parent["jobs"] = json!(["j1"]);
        parent["scheduler"] = json!({"job":"j1","phase":"running"});
        f.put(parent);
        f.e.backend.records[0]["status"] = json!("succeeded");
        f.e.backend.records[0]["last_message"] = json!(result);
        f.e.backend.records[0]["todo_id"] = json!(1);
        f.e.backend.ps.clear();
        f.reconcile();
        assert_eq!(
            f.get(1)["status"],
            if result.starts_with("状態: 完了") {
                "done"
            } else {
                "block"
            }
        );
        assert_eq!(f.get(2)["status"], "todo");
    }
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    let mut second = item(2, "civ6", "doing", &[]);
    second["jobs"] = json!(["j1"]);
    second["scheduler"] = json!({"job":"j1","phase":"running"});
    f.put(second);
    f.e.backend.records[0]["status"] = json!("succeeded");
    f.e.backend.ps.clear();
    f.reconcile();
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(f.get(2)["status"], "todo");
}

#[test]
fn explicit_resume_supersedes_cancelled_and_old_owner_result() {
    for old_status in ["cancelled", "succeeded"] {
        for result in ["状態: 継続中\n続けます。", "状態: 完了\n完了しました。"]
        {
            let mut f = Fixture::new();
            f.managed(1, 1, "civ6", 0);
            f.e.backend.records[0]["status"] = json!(old_status);
            f.e.backend.records[0]["last_message"] =
                json!("状態: 要判断\n公開してよいですか。\na. 公開\nb. 中止");
            f.e.edit(&json!(1), |row| {
                row["status"] = json!("block");
                row["scheduler"]["pending_cancel"] = json!("hung");
                row["scheduler"]["answer"] = json!("owner-answer");
            })
            .unwrap();
            let before = f.get(1);
            let paths = cc_todo::Paths::at(f.temp.path(), &f.temp.path().join("jobs"));
            let mut db = f.e.todo().unwrap();
            cc_todo::execute(
                &mut db,
                &cc_todo::Command::Start {
                    id: 1,
                    job: "new-worker".into(),
                    progress: None,
                },
                &paths,
            )
            .unwrap();
            // Repeating the same handoff is idempotent.
            cc_todo::execute(
                &mut db,
                &cc_todo::Command::Start {
                    id: 1,
                    job: "new-worker".into(),
                    progress: None,
                },
                &paths,
            )
            .unwrap();
            write_json(&f.e.todo_path, &db, 1).unwrap();
            f.e.backend.records.push(job("new-worker", 2, "running"));
            f.e.backend.ps.insert(2, proc(2, 0, "codex.exe"));
            f.reconcile();
            assert_eq!(f.get(1)["status"], "doing");
            assert_eq!(f.get(1)["scheduler"]["job"], "new-worker");
            assert_eq!(f.get(1)["jobs"], json!(["j1", "new-worker"]));
            for field in ["spec", "needs"] {
                assert_eq!(f.get(1)[field], before[field]);
            }
            for field in ["worktree", "branch", "answer"] {
                assert_eq!(f.get(1)["scheduler"][field], before["scheduler"][field]);
            }
            assert!(f.get(1)["scheduler"].get("pending_cancel").is_none());
            f.dispatch();
            assert!(f.e.backend.launched.is_empty());
            assert!(f.e.backend.cancelled.is_empty());
            f.e.backend.records[1]["status"] = json!("succeeded");
            f.e.backend.records[1]["last_message"] = json!(result);
            f.e.backend.ps.clear();
            f.reconcile();
            assert_eq!(
                f.get(1)["status"],
                if result.starts_with("状態: 完了") {
                    "done"
                } else {
                    "todo"
                }
            );
        }
    }
}

fn start_fixture(f: &mut Fixture, worker: &str) {
    let paths = cc_todo::Paths::at(f.temp.path(), &f.temp.path().join("jobs"));
    let _lock = cc_todo::Lock::acquire(&paths.lock, Duration::from_secs(2)).unwrap();
    let mut db = f.e.todo().unwrap();
    cc_todo::execute(
        &mut db,
        &cc_todo::Command::Start {
            id: 1,
            job: worker.into(),
            progress: None,
        },
        &paths,
    )
    .unwrap();
    cc_todo::save(&f.e.todo_path, &db).unwrap();
}

#[test]
fn resume2172_finished_result_is_durable_and_never_replayed_by_same_worker_start() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.e.backend.records[0]["status"] = json!("succeeded");
    f.e.backend.records[0]["last_message"] =
        json!("状態: 要判断\n公開してよいですか。\na. 公開\nb. 中止");
    f.reconcile();
    assert_eq!(f.get(1)["status"], "block");
    assert!(f.get(1)["processed_results"]["j1"].is_object());
    start_fixture(&mut f, "j1");
    let resumed = f.get(1);
    start_fixture(&mut f, "j1");
    assert_eq!(f.get(1)["resume_generation"], resumed["resume_generation"]);
    // The ledger lives in todo.json, not the scheduler's in-memory state.
    f.e.state = json!({});
    f.reconcile();
    f.reconcile();
    assert_eq!(f.get(1)["status"], "doing");
    assert_eq!(f.e.backend.outcome_count, 1);
}

#[test]
fn resume2172_legacy_finished_result_can_resume_without_job_or_same_job() {
    for worker in ["", "j1", "new-worker"] {
        let mut f = Fixture::new();
        f.managed(1, 1, "civ6", 0);
        f.e.edit(&json!(1), |row| {
            row["status"] = json!("block");
            row["progress"] = json!(63);
            row["scheduler"]["phase"] = json!("finished");
            row["scheduler"]["answer"] = json!("keep answer");
        })
        .unwrap();
        let before = f.get(1);
        f.e.backend.records[0]["status"] = json!("succeeded");
        f.e.backend.records[0]["last_message"] =
            json!("状態: 要判断\n公開してよいですか。\na. 公開\nb. 中止");
        start_fixture(&mut f, worker);
        if worker == "new-worker" {
            f.e.backend.records.push(job(worker, 2, "running"));
            f.e.backend.ps.insert(2, proc(2, 0, "codex.exe"));
        }
        f.reconcile();
        assert_eq!(f.get(1)["status"], "doing", "worker={worker}");
        assert_eq!(f.e.backend.outcome_count, 0);
        for field in ["spec", "progress"] {
            assert_eq!(f.get(1)[field], before[field]);
        }
        for field in ["answer", "worktree", "branch"] {
            assert_eq!(f.get(1)["scheduler"][field], before["scheduler"][field]);
        }
        assert!(arr(&f.get(1)["jobs"]).contains(&json!("j1")));
        assert!(f.get(1)["processed_results"]["j1"].is_object());
    }
}

#[test]
fn resume2172_outcome_read_race_preserves_start_block_wait_and_aba_resume() {
    for action in ["start", "block", "wait", "aba"] {
        let mut f = Fixture::new();
        f.managed(1, 1, "civ6", 0);
        f.put(item(2, "civ6", "todo", &[]));
        if action == "aba" {
            start_fixture(&mut f, "j1");
        }
        let snapshot = f.get(1);
        let command = match action {
            "start" => cc_todo::Command::Start {
                id: 1,
                job: "".into(),
                progress: None,
            },
            "aba" => cc_todo::Command::Start {
                id: 1,
                job: "j1".into(),
                progress: None,
            },
            "block" => cc_todo::Command::Block {
                id: 1,
                reason: "owner stop".into(),
                progress: None,
            },
            _ => cc_todo::Command::Wait {
                id: 1,
                on: vec![2],
                progress: None,
            },
        };
        // ABA is performed within outcome(): doing -> block -> doing must also be detected.
        f.e.backend.outcome_block_before = action == "aba";
        f.e.backend.outcome_edit = Some((f.e.todo_path.clone(), command));
        let mut records = vec![job("j1", 1, "succeeded")];
        f.e.reconcile_item(&snapshot, &mut records, &Processes::new(), &f.cfg)
            .unwrap();
        let row = f.get(1);
        assert_eq!(
            row["status"],
            match action {
                "block" => "block",
                "wait" => "wait",
                _ => "doing",
            },
            "action={action}"
        );
        assert!(
            row["processed_results"]["j1"].is_null(),
            "stale result was committed: {action}"
        );
        assert_ne!(row["scheduler"]["phase"], "finished");
        assert_eq!(
            f.e.todo().unwrap(),
            f.e.backend.outcome_saved.clone().unwrap(),
            "stale snapshot must not even touch updated"
        );
    }
}

#[test]
fn resume2172_start_creates_explicit_receipt_without_prior_scheduler() {
    let mut f = Fixture::new();
    let mut row = item(1, "civ6", "block", &[]);
    row["jobs"] = json!(["old"]);
    row["progress"] = json!(42);
    f.put(row);
    start_fixture(&mut f, "new-worker");
    let started = f.get(1);
    assert_eq!(started["scheduler"]["job"], "new-worker");
    assert_eq!(started["worker_receipt"]["job"], "new-worker");
    assert_eq!(
        started["worker_receipt"]["generation"],
        started["resume_generation"]
    );
    assert_eq!(started["jobs"], json!(["old", "new-worker"]));
    assert_eq!(started["progress"], 42);
    start_fixture(&mut f, "new-worker");
    assert_eq!(f.get(1)["resume_generation"], started["resume_generation"]);
}

#[test]
fn resume2172_each_shared_job_result_is_received_per_item_and_repeated_tick_is_noop() {
    let mut f = Fixture::new();
    for id in [1, 2] {
        let mut row = item(id, "civ6", "doing", &[]);
        row["scheduler"] = json!({"job":"shared","phase":"running"});
        row["jobs"] = json!(["shared"]);
        f.put(row);
    }
    f.e.backend.records = vec![job("shared", 1, "succeeded")];
    f.e.backend.records[0]["last_message"] = json!("状態: 完了\n終了しました。");
    f.reconcile();
    for id in [1, 2] {
        assert_eq!(f.get(id)["status"], "todo");
        assert_eq!(f.get(id)["processed_results"]["shared"]["reason"], "todo");
    }
    let before = fs::read(&f.e.todo_path).unwrap();
    f.reconcile();
    assert_eq!(fs::read(&f.e.todo_path).unwrap(), before);
    assert_eq!(f.e.backend.outcome_count, 2);
}

#[test]
fn explicit_resume_invalidates_old_reconcile_and_cancel_snapshot() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    let old = f.get(1);
    let mut db = f.e.todo().unwrap();
    let paths = cc_todo::Paths::at(f.temp.path(), &f.temp.path().join("jobs"));
    cc_todo::execute(
        &mut db,
        &cc_todo::Command::Start {
            id: 1,
            job: "new-worker".into(),
            progress: None,
        },
        &paths,
    )
    .unwrap();
    write_json(&f.e.todo_path, &db, 1).unwrap();
    let mut records = vec![job("j1", 1, "cancelled")];
    f.e.reconcile_item(&old, &mut records, &Processes::new(), &f.cfg)
        .unwrap();
    let mut old_job = job("j1", 1, "running");
    f.e.cancel_for(&old, &mut old_job, "hung", &f.cfg).unwrap();
    assert_eq!(f.get(1)["status"], "doing");
    assert_eq!(f.get(1)["scheduler"]["job"], "new-worker");
    assert!(f.e.backend.cancelled.is_empty());
}

#[test]
fn explicit_resume_without_job_preserves_scheduler_and_manual_block_is_untouched() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    let before = f.get(1);
    let paths = cc_todo::Paths::at(f.temp.path(), &f.temp.path().join("jobs"));
    let mut db = f.e.todo().unwrap();
    cc_todo::execute(
        &mut db,
        &cc_todo::Command::Start {
            id: 1,
            job: String::new(),
            progress: None,
        },
        &paths,
    )
    .unwrap();
    assert_eq!(db["items"][0]["scheduler"], before["scheduler"]);
    assert_eq!(db["items"][0]["jobs"], before["jobs"]);
    f.e.edit(&json!(1), |row| {
        row["status"] = json!("block");
        row["note"] = json!("オーナーの回答に従い、停止したままにします。");
        row["scheduler"]["phase"] = json!("blocked");
    })
    .unwrap();
    f.e.backend.records[0]["status"] = json!("cancelled");
    f.reconcile();
    assert_eq!(f.get(1)["status"], "block");
    assert_eq!(
        f.get(1)["note"],
        "オーナーの回答に従い、停止したままにします。"
    );
}

#[test]
fn old_retry_limit_hold_migrates_but_owner_stop_and_client_hold_do_not() {
    let mut f = Fixture::new();
    for (id, project, note) in [
        (
            1,
            "civ6",
            "ジョブが失敗。2回止まったため、オーナーに再開方法を確認します。",
        ),
        (2, "civ6", "オーナーの回答に従い、停止したままにします。"),
        (
            3,
            "client-project",
            "ジョブが失敗。2回止まったため、オーナーに再開方法を確認します。",
        ),
    ] {
        let mut row = item(id, project, "block", &[]);
        row["note"] = json!(note);
        row["scheduler"] = json!({"phase":"blocked","retry_count":1});
        f.put(row);
    }
    fs::write(f.temp.path().join("client-projects.txt"), "client-project").unwrap();
    f.reconcile();
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(n(&f.get(1)["scheduler"]["not_before"]), T + 60.);
    assert_eq!(f.get(2)["status"], "block");
    assert_eq!(f.get(3)["status"], "block");
    assert!(f.e.backend.notices.is_empty());
}
fn proc(pid: u32, ppid: u32, name: &str) -> Value {
    json!({"pid":pid,"ppid":ppid,"name":name,"created":FILETIME_EPOCH+((T-1200.)*1e7) as u64,"rss":GIB as u64,"cpu":10,"metrics_ok":true})
}
fn ps(rows: Vec<Value>) -> Processes {
    rows.into_iter().map(|p| (n(&p["pid"]) as u32, p)).collect()
}
fn job(id: &str, root: u32, status: &str) -> Value {
    json!({"id":id,"pid":root,"pid_created":proc(root,0,"codex.exe")["created"],"codex_pid":null,"codex_pid_created":null,"status":status,"created_at":T-1200.,"started_at":T-1200.})
}
fn measurement() -> Value {
    json!({"available_gb":30,"ram_total_gb":93.5,"cpu_percent":70,"commit_percent":60,"commit_gb":120,"commit_limit_gb":218,"physical_cores":16,"logical_cores":32,"boot_s":1,"disk_free_gb":{"C:":300,"D:":200}})
}
fn item(id: u64, project: &str, status: &str, needs: &[&str]) -> Value {
    json!({"id":id,"project":project,"status":status,"title":format!("Work {id}"),"spec":"spec.md","dispatchable":true,"needs":needs,"jobs":[],"note":"","created_s":id})
}
fn result(m: Value, jobs: Vec<Value>, ps: Processes, mut state: Value, cfg: Value) -> Value {
    let mut c = defaults();
    merge(&mut c, cfg);
    capacity(&m, &c, &mut state, &jobs, &ps, T)
}
#[test]
fn test_admission_ram_and_core_cap() {
    let jobs = (1..10)
        .map(|i| job(&format!("j{i}"), i, "running"))
        .collect();
    let ps = ps((1..10).map(|i| proc(i, 0, "codex.exe")).collect());
    let r = result(measurement(), jobs, ps, json!({}), json!({}));
    assert_eq!(
        (
            n(&r["running_jobs"]),
            n(&r["hard_cap"]),
            n(&r["resource_slots"]),
            n(&r["allowance_now"])
        ),
        (9., 38., 3., 1.)
    );
}
#[test]
fn test_expected_footprint_sums_entire_tree() {
    let rows: Vec<_> = (1..=4)
        .map(|i| {
            let mut p = proc(i, i - 1, "node.exe");
            p["rss"] = json!(GIB * i as f64);
            p["created"] = json!(p["created"].as_u64().unwrap() + i as u64 * 10000000);
            p
        })
        .collect();
    let mut j = job("j1", 1, "running");
    j["pid_created"] = rows[0]["created"].clone();
    let r = result(measurement(), vec![j], ps(rows), json!({}), json!({}));
    assert_eq!(n(&r["job_footprints_gb"]["j1"]), 10.);
    assert_eq!(n(&r["expected_job_gb"]), 12.5);
    assert_eq!(n(&r["resource_slots"]), 1.);
}
#[test]
fn test_p90_nearest_rank_and_expired_samples() {
    let mut points: Vec<_> = (1..=10).map(|i| json!({"at":T-10.,"gb":i})).collect();
    points.push(json!({"at":T-4000.,"gb":900}));
    let r = result(
        measurement(),
        vec![],
        Processes::new(),
        json!({"footprints":points}),
        json!({}),
    );
    assert_eq!(n(&r["job_p90_gb"]), 9.);
    assert_eq!(n(&r["expected_job_gb"]), 11.25);
}
#[test]
fn test_exact_ram_reserve_is_not_admitted() {
    for (ram, admit) in [(18., 0.), (18.01, 1.)] {
        let mut m = measurement();
        m["available_gb"] = json!(ram);
        assert_eq!(
            n(&result(m, vec![], Processes::new(), json!({}), json!({}))["allowance_now"]),
            admit
        );
    }
}
#[test]
fn test_commit_cpu_disk_thresholds() {
    for (field, reason) in [
        ("commit_percent", "commit limit"),
        ("cpu_percent", "CPU rolling limit"),
    ] {
        let mut m = measurement();
        m[field] = json!(if field == "commit_percent" { 85 } else { 80 });
        let r = result(m, vec![], Processes::new(), json!({}), json!({}));
        assert_eq!(n(&r["allowance_now"]), 0.);
        assert!(arr(&r["reasons"]).contains(&json!(reason)));
    }
    let mut m = measurement();
    m["disk_free_gb"]["D:"] = json!(30);
    assert_eq!(
        n(&result(m, vec![], Processes::new(), json!({}), json!({}))["allowance_now"]),
        0.
    );
}
#[test]
fn test_cpu_rolls_over_recent_minutes() {
    let mut m = measurement();
    m["cpu_percent"] = json!(60);
    assert_eq!(
        n(&result(
            m,
            vec![],
            Processes::new(),
            json!({"cpu":[{"at":T-50.,"value":100},{"at":T-500.,"value":100}]}),
            json!({})
        )["cpu_rolling_percent"]),
        80.
    );
}
#[test]
fn test_global_launch_spacing_includes_non_scheduler_jobs() {
    for (age, admit) in [(9., 0.), (10., 1.)] {
        let mut j = job("j1", 1, "running");
        j["created_at"] = json!(T - age);
        assert_eq!(
            n(&result(
                measurement(),
                vec![j],
                Processes::new(),
                json!({}),
                json!({})
            )["allowance_now"]),
            admit
        );
    }
}
#[test]
fn test_memory_pressure_stops_launching() {
    let mut m = measurement();
    m["available_gb"] = json!(7);
    assert_eq!(
        n(&result(m, vec![], Processes::new(), json!({}), json!({}))["allowance_now"]),
        0.
    );
}
#[test]
fn instantaneous_cpu_spike_stops_admission_even_with_a_low_rolling_average() {
    let mut m = measurement();
    m["cpu_percent"] = json!(95);
    let r = result(
        m,
        vec![],
        Processes::new(),
        json!({"cpu":[{"at":T-20.,"value":10}]}),
        json!({}),
    );
    assert_eq!(n(&r["allowance_now"]), 0.);
    assert!(arr(&r["reasons"]).contains(&json!("CPU rolling limit")));
}
#[test]
fn resource_limit_can_exceed_twelve_without_spending_the_memory_reserve() {
    let mut m = measurement();
    m["available_gb"] = json!(50);
    m["cpu_percent"] = json!(25);
    let jobs: Vec<_> = (1..=15)
        .map(|i| job(&format!("j{i}"), i, "running"))
        .collect();
    let processes = ps((1..=15).map(|i| proc(i, 0, "codex.exe")).collect());
    let r = result(m, jobs, processes, json!({}), json!({}));
    assert_eq!(n(&r["running_jobs"]), 15.);
    assert_eq!(n(&r["hard_cap"]), 38.);
    assert_eq!(n(&r["memory_slots"]), 16.);
    assert_eq!(n(&r["resource_slots"]), 16.);
    assert_eq!(n(&r["allowance_now"]), 1.);
}
#[test]
fn continuation_reuses_only_unfinished_successful_conversations_in_the_same_folder() {
    let mut j = job("source", 1, "succeeded");
    j["cwd"] = json!("D:/repo-wt/task");
    j["thread_id"] = json!("12345678-1234-4321-9234-123456789abc");
    j["last_message"] = json!("状態: 継続中\n検査を続けます。");
    assert_eq!(
        continuation_session(&j, "D:\\repo-wt\\task", &[]),
        Some("12345678-1234-4321-9234-123456789abc")
    );
    assert!(continuation_session(&j, "D:/another", &[]).is_none());
    let running = json!({"status":"running","resume_session":j["thread_id"]});
    assert!(continuation_session(&j, "D:/repo-wt/task", &[running]).is_none());
    for result in [
        "状態: 完了\n完了しました。",
        "状態: 要判断\n公開を決めてください。\na. 公開\nb. 非公開",
    ] {
        j["last_message"] = json!(result);
        assert!(continuation_session(&j, "D:/repo-wt/task", &[]).is_none());
    }
}
#[test]
fn test_configurable_cap_and_pid_reuse() {
    let mut p = proc(1, 0, "node.exe");
    p["created"] = json!(p["created"].as_u64().unwrap() + 100);
    let r = result(
        measurement(),
        vec![job("j1", 1, "running")],
        ps(vec![p]),
        json!({}),
        json!({"hard_cap":2}),
    );
    assert_eq!(n(&r["running_jobs"]), 0.);
    assert_eq!(n(&r["hard_cap"]), 2.);
}
#[test]
fn test_access_denied_pid_is_counted_conservatively() {
    assert_eq!(
        n(&result(
            measurement(),
            vec![job("j1", 1, "running")],
            ps(vec![json!({"pid":1,"created":null})]),
            json!({}),
            json!({})
        )["running_jobs"]),
        1.
    );
}
#[test]
fn test_priority_fifo_and_spec_required() {
    let mut rows = vec![
        item(1, "other", "todo", &[]),
        item(4, "civ6", "todo", &[]),
        item(3, "GoDD-AO", "todo", &[]),
        item(2, "civ6", "todo", &[]),
    ];
    let mut no_spec = item(0, "civ6", "todo", &[]);
    remove(&mut no_spec, "spec");
    rows.push(no_spec);
    let mut no_dispatch = item(5, "civ6", "todo", &[]);
    no_dispatch["dispatchable"] = json!(false);
    rows.push(no_dispatch);
    assert_eq!(
        priority_queue(&rows, &json!({"civ6":1,"GoDD-AO":2}), T)
            .iter()
            .map(|i| n(&i["id"]) as u64)
            .collect::<Vec<_>>(),
        [2, 4, 3, 1]
    );
}
#[test]
fn test_civ6_and_cargo_exclusivity_includes_manual_links() {
    let mut i = item(1, "civ6", "todo", &["civ6", "cargo"]);
    i["jobs"] = json!(["j1"]);
    for (status, count) in [("running", 2), ("succeeded", 0)] {
        assert_eq!(
            resources_busy(
                &[i.clone()],
                &[job("j1", 1, status)],
                &ps(vec![proc(1, 0, "node.exe")]),
                T
            )
            .len(),
            count
        );
    }
}
#[test]
fn test_live_game_blocks_live_test_admission() {
    assert_eq!(
        resources_busy(&[], &[], &ps(vec![proc(10, 0, "CivilizationVI.exe")]), T)
            .into_iter()
            .collect::<Vec<_>>(),
        ["civ6"]
    );
}
#[test]
fn test_external_cargo_build_reserves_cargo() {
    assert_eq!(
        resources_busy(&[], &[], &ps(vec![proc(10, 0, "rustc.exe")]), T)
            .into_iter()
            .collect::<Vec<_>>(),
        ["cargo"]
    );
}
#[test]
fn test_memory_victim_lowest_priority_then_latest() {
    let mut rows = vec![];
    let mut jobs = vec![];
    let mut processes = Processes::new();
    for (id, project, start) in [
        (1, "civ6", 3),
        (2, "other", 1),
        (3, "other", 2),
        (4, "other", 20),
    ] {
        let mut i = item(id, "civ6", "doing", &[]);
        i["project"] = json!(project);
        if id != 4 {
            i["scheduler"] = json!({"job":format!("j{id}"),"started_s":start});
        }
        rows.push(i);
        jobs.push(job(&format!("j{id}"), id as u32, "running"));
        processes.insert(id as u32, proc(id as u32, 0, "node.exe"));
    }
    assert_eq!(
        pressure_victim(&rows, &jobs, &processes, &json!({"civ6":1}), T).unwrap()["id"],
        3
    );
}
#[test]
fn test_hang_requires_full_observation_period() {
    let (r, hung) = hang_progress(&Value::Null, 5., Some(10.), "tree", T, 1800.);
    assert!(!hung);
    assert!(!hang_progress(&r, 5., Some(10.), "tree", T + 1799., 1800.).1);
    assert!(hang_progress(&r, 5., Some(10.), "tree", T + 1800., 1800.).1);
}
#[test]
fn test_cpu_event_tree_change_each_reset_hang_timer() {
    let (r, _) = hang_progress(&Value::Null, 5., Some(10.), "tree", T, 1800.);
    for (event, cpu, tree) in [
        (6., Some(10.), "tree"),
        (5., Some(11.), "tree"),
        (5., Some(10.), "new-tree"),
        (5., None, "tree"),
    ] {
        let (new, hung) = hang_progress(&r, event, cpu, tree, T + 1900., 1800.);
        assert!(!hung);
        assert_eq!(n(&new["since"]), T + 1900.);
    }
}
#[test]
fn test_orphan_selector_excludes_interactive_and_protected_trees() {
    let jobs = vec![job("j1", 1, "succeeded"), job("live", 20, "running")];
    let ps = ps(vec![
        proc(1, 0, "python.exe"),
        proc(2, 1, "node.exe"),
        proc(3, 0, "node.exe"),
        proc(4, 1, "Docker.exe"),
        proc(5, 4, "node.exe"),
        proc(6, 1, "bun.exe"),
        proc(7, 6, "node.exe"),
        proc(8, 1, "CivilizationVI.exe"),
        proc(9, 8, "node.exe"),
        proc(20, 0, "codex.exe"),
        proc(21, 20, "node.exe"),
        proc(22, 1, "mcp-server.exe"),
        proc(23, 1, "python.exe"),
    ]);
    let ledger = ownership(&jobs, &ps, &json!({}));
    assert_eq!(
        orphan_candidates(&jobs, &ps, &ledger, T, 600.)
            .iter()
            .map(|p| n(&p["pid"]) as u32)
            .collect::<Vec<_>>(),
        [2, 22]
    );
}
#[test]
fn test_saved_lineage_survives_dead_parent_but_not_pid_reuse() {
    let jobs = vec![job("j1", 1, "running")];
    let processes = ps(vec![proc(1, 0, "codex.exe"), proc(2, 1, "node.exe")]);
    let ledger = ownership(&jobs, &processes, &json!({}));
    let mut gone = ps(vec![processes[&2].clone()]);
    assert_eq!(
        orphan_candidates(&jobs, &gone, &ownership(&jobs, &gone, &ledger), T, 600.).len(),
        1
    );
    gone.get_mut(&2).unwrap()["created"] = json!(processes[&2]["created"].as_u64().unwrap() + 1);
    assert!(orphan_candidates(&jobs, &gone, &ownership(&jobs, &gone, &ledger), T, 600.).is_empty());
}
#[test]
fn test_unknown_dead_numeric_parent_does_not_prove_ownership() {
    let jobs = vec![job("j1", 1, "succeeded")];
    let ps = ps(vec![proc(2, 1, "node.exe")]);
    assert!(orphan_candidates(&jobs, &ps, &ownership(&jobs, &ps, &json!({})), T, 600.).is_empty());
}
#[test]
fn test_orphan_minimum_age_and_missing_job() {
    let jobs = vec![job("j1", 1, "succeeded")];
    let mut young = proc(2, 1, "node.exe");
    young["created"] = json!(FILETIME_EPOCH + ((T - 599.) * 1e7) as u64);
    let ps = ps(vec![proc(1, 0, "codex.exe"), young]);
    let ledger = ownership(&jobs, &ps, &json!({}));
    assert_eq!(
        orphan_candidates(&jobs, &ps, &ledger, T, 600.)
            .iter()
            .map(|p| n(&p["pid"]) as u32)
            .collect::<Vec<_>>(),
        [1]
    );
    assert!(orphan_candidates(&[], &ps, &ledger, T, 600.).is_empty());
}

struct Fake {
    action_path: Option<PathBuf>,
    cancel_edit: Option<(PathBuf, Value)>,
    repair_allowed: bool,
    repair_denied: Option<String>,
    repair_calls: Vec<String>,
    repair_edit: Option<(PathBuf, Value)>,
    launch_start: Option<PathBuf>,
    prepare_edit: Option<(PathBuf, Value)>,
    outcome_edit: Option<(PathBuf, cc_todo::Command)>,
    outcome_block_before: bool,
    outcome_count: usize,
    outcome_saved: Option<Value>,
    records: Vec<Value>,
    launched: Vec<Value>,
    cancelled: Vec<Value>,
    killed: Vec<Value>,
    notices: Vec<Value>,
    event: f64,
    cancel_ok: bool,
    pick_count: usize,
    crash_after_launch: bool,
    owner_reply: Option<Value>,
    closed: Vec<Value>,
    clock: f64,
    m: Value,
    ps: Processes,
    fresh: Option<Processes>,
    prepare_pressure: bool,
    fail_lost: bool,
    close_fails: bool,
    reply_count: usize,
    settings_change: Option<(PathBuf, Value)>,
    measure_count: usize,
    change_during_prepare: bool,
    worktree_failure: Option<u64>,
}
impl Default for Fake {
    fn default() -> Self {
        Self {
            action_path: None,
            cancel_edit: None,
            repair_allowed: false,
            repair_denied: None,
            repair_calls: vec![],
            repair_edit: None,
            launch_start: None,
            prepare_edit: None,
            outcome_edit: None,
            outcome_block_before: false,
            outcome_count: 0,
            outcome_saved: None,
            records: vec![],
            launched: vec![],
            cancelled: vec![],
            killed: vec![],
            notices: vec![],
            event: 1.,
            cancel_ok: true,
            pick_count: 0,
            crash_after_launch: false,
            owner_reply: None,
            closed: vec![],
            clock: T,
            m: measurement(),
            ps: ps(vec![proc(1, 0, "codex.exe")]),
            fresh: None,
            prepare_pressure: false,
            fail_lost: false,
            close_fails: false,
            reply_count: 0,
            settings_change: None,
            measure_count: 0,
            change_during_prepare: false,
            worktree_failure: None,
        }
    }
}
impl Backend for Fake {
    fn action_progress(&mut self, job: &Value, previous: &cc_job_policy::action_progress::Progress) -> Result<Option<cc_job_policy::action_progress::Progress>> {
        Ok(self.action_path.as_ref().and_then(|path| cc_job_policy::action_progress::scan(path, job, previous, self.clock).ok()))
    }
    fn no_action_scope(&mut self, repo: &str, _: &Value, _: &Value) -> Result<bool> {
        Ok(self.repair_scope_allowed(repo))
    }
    fn cancel_no_action(&mut self, _: &Value, job: &mut Value, _: &Value, _: &Path) -> Result<bool> {
        let stopped = self.cancel_for_recovery(job)?;
        if let Some((path,db))=self.cancel_edit.take() { write_json(&path,&db,1)?; }
        Ok(stopped)
    }
    fn repair_scope_allowed(&mut self, repo: &str) -> bool {
        self.repair_calls.push(repo.into());
        if let Some((path, db)) = self.repair_edit.take() {
            write_json(&path, &db, 1).unwrap();
        }
        self.repair_allowed && self.repair_denied.as_deref() != Some(repo)
    }
    fn clock(&self) -> f64 {
        self.clock
    }
    fn measure(&mut self, _: &mut Value) -> Result<(Value, Processes)> {
        self.measure_count += 1;
        if self.measure_count == 2 {
            if let Some((path, settings)) = self.settings_change.take() {
                let mut db = read_json(&path, json!({}))?;
                merge(&mut db["items"][0], settings);
                write_json(&path, &db, 2)?;
            }
        }
        Ok((self.m.clone(), self.ps.clone()))
    }
    fn snapshot(&mut self) -> Result<Processes> {
        Ok(self.fresh.clone().unwrap_or(self.ps.clone()))
    }
    fn jobs(&mut self) -> Result<Vec<Value>> {
        Ok(self.records.clone())
    }
    fn event_mtime(&mut self, _: &Value) -> Result<f64> {
        Ok(self.event)
    }
    fn mark_lost(&mut self, j: &mut Value) -> Result<bool> {
        if self.fail_lost {
            return Err(Error("RuntimeError"));
        }
        j["status"] = json!("lost");
        if let Some(row) = self.records.iter_mut().find(|r| r["id"] == j["id"]) {
            *row = j.clone();
        }
        Ok(true)
    }
    fn launch(&mut self, i: &Value, p: &Path, _: &Value) -> Result<String> {
        if let Some(j) = self
            .records
            .iter()
            .find(|j| j["launch_key"] == i["scheduler"]["key"])
        {
            return Ok(s(&j["id"]).into());
        }
        let jid = format!("launched{}", self.launched.len() + 1);
        self.launched
            .push(json!({"id":jid,"item":i,"text":fs::read_to_string(p)?}));
        let mut j = job(&jid, 99, "running");
        j["created_at"] = json!(T);
        j["launch_key"] = i["scheduler"]["key"].clone();
        j["todo_id"] = i["id"].clone();
        self.records.push(j);
        if let Some(path) = &self.launch_start {
            let mut db = read_json(path, json!({}))?;
            let paths = cc_todo::Paths::at(path.parent().unwrap(), path.parent().unwrap());
            cc_todo::execute(
                &mut db,
                &cc_todo::Command::Start {
                    id: i["id"].as_i64().unwrap(),
                    job: jid.clone(),
                    progress: None,
                },
                &paths,
            )
            .unwrap();
            write_json(path, &db, 1)?;
        }
        if self.crash_after_launch {
            self.crash_after_launch = false;
            return Err(Error("KeyboardInterrupt"));
        }
        Ok(jid)
    }
    fn cancel(&mut self, j: &mut Value) -> Result<bool> {
        self.cancelled.push(j["id"].clone());
        if self.cancel_ok {
            j["status"] = json!("cancelled");
            if let Some(r) = self.records.iter_mut().find(|r| r["id"] == j["id"]) {
                *r = j.clone();
            }
        }
        Ok(self.cancel_ok)
    }
    fn cancel_for_recovery(&mut self, j: &mut Value) -> Result<bool> {
        j["recovery_cancel"] =
            json!({"key":"scheduler-intent","committed_at":"2026-10-11T02:01:36+09:00"});
        self.cancel(j)
    }
    fn kill_orphan(&mut self, c: &Value) -> Result<bool> {
        self.killed.push(c["pid"].clone());
        Ok(true)
    }
    fn pick(&mut self, _: &str) -> Result<Value> {
        self.pick_count += 1;
        Ok(json!({"model":"mock-model","effort":"high","task_type":"implementation"}))
    }
    fn model_exists(&mut self, m: &str) -> bool {
        m != "gpt-6.1-luna"
    }
    fn notify(&mut self, i: &Value) -> Result<Value> {
        self.notices.push(i["id"].clone());
        Ok(json!({"thread_id":"mock-thread"}))
    }
    fn reply(&mut self, _: &Value) -> Result<Option<Value>> {
        self.reply_count += 1;
        Ok(self.owner_reply.clone())
    }
    fn close_question(&mut self, r: &Value) -> Result<()> {
        if self.close_fails {
            return Err(Error("RuntimeError"));
        }
        self.closed.push(r["thread_id"].clone());
        Ok(())
    }
    fn outcome(&mut self, j: &Value) -> Result<String> {
        self.outcome_count += 1;
        if let Some((store, command)) = self.outcome_edit.take() {
            let paths = cc_todo::Paths::at(store.parent().unwrap(), &store.with_file_name("jobs"));
            let _lock = cc_todo::Lock::acquire(&paths.lock, Duration::from_secs(2)).unwrap();
            let mut db = cc_todo::load(&store).unwrap();
            if self.outcome_block_before {
                cc_todo::execute(
                    &mut db,
                    &cc_todo::Command::Block {
                        id: 1,
                        reason: "owner stop".into(),
                        progress: None,
                    },
                    &paths,
                )
                .unwrap();
            }
            cc_todo::execute(&mut db, &command, &paths).unwrap();
            cc_todo::save(&store, &db).unwrap();
            self.outcome_saved = Some(db);
        }
        Ok(j["last_message"]
            .as_str()
            .map(cc_scheduler::backend::result_outcome)
            .unwrap_or("done")
            .into())
    }
    fn worktree(&mut self, _: &str, i: &Value) -> Result<()> {
        if i["id"].as_u64() == self.worktree_failure && self.worktree_failure.is_some() {
            return Err(Error("OriginMissing"));
        }
        if let Some((path, db)) = self.prepare_edit.take() {
            write_json(&path, &db, 1)?;
        }
        if self.change_during_prepare {
            if let Some((path, settings)) = self.settings_change.take() {
                let mut db = read_json(&path, json!({}))?;
                merge(&mut db["items"][0], settings);
                write_json(&path, &db, 2)?;
            }
        }
        if self.prepare_pressure {
            self.m["available_gb"] = json!(3);
        }
        fs::create_dir_all(s(&i["scheduler"]["worktree"]))?;
        Ok(())
    }
}
#[test]
fn missing_private_registry_only_dispatches_verified_explicit_pc_repair() {
    for project in [
        "cc-pc-tools-rs",
        "pc-capacity-audit",
        "localai-web-rs",
        "other",
    ] {
        for allowed in [false, true] {
            let mut f = Fixture::new();
            fs::remove_file(f.e.root.parent().unwrap().join("client-projects.txt")).unwrap();
            let mut db = f.e.todo().unwrap();
            db["projects"][project] = json!(f.repo);
            write_json(&f.e.todo_path, &db, 1).unwrap();
            f.e.backend.repair_allowed = allowed;
            f.put(item(1, project, "todo", &[]));
            f.dispatch();
            assert_eq!(
                f.e.backend.launched.len(),
                usize::from(allowed && project != "other")
            );
        }
    }
}
#[test]
fn repair_step_never_replays_legacy_questions_or_unknown_work() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 1);
    let before = f.get(1);
    fs::remove_file(f.e.root.parent().unwrap().join("client-projects.txt")).unwrap();
    f.e.backend.repair_allowed = true;
    let report = f.e.run("repair-step", false).unwrap();
    assert_eq!(report["repair_mode"], true);
    assert_eq!(f.get(1), before);
    assert!(f.e.backend.notices.is_empty());
    assert!(f.e.backend.cancelled.is_empty());
    assert!(f.e.backend.closed.is_empty());
    assert_eq!(f.e.backend.reply_count, 0);
    assert!(f.e.backend.launched.is_empty());
    assert_eq!(report["step_failures"], json!([]));
}

fn completed_dependency_repair() -> Fixture {
    let mut f = Fixture::new();
    fs::remove_file(f.e.root.parent().unwrap().join("client-projects.txt")).unwrap();
    let mut db = f.e.todo().unwrap();
    db["projects"]["cc-pc-tools-rs"] = json!(f.repo);
    write_json(&f.e.todo_path, &db, 1).unwrap();
    f.e.backend.repair_allowed = true;
    f.managed(1, 2, "cc-pc-tools-rs", 0);
    f.managed(2, 3, "cc-pc-tools-rs", 0);
    wait_row(&mut f, 1, json!([2]));
    for record in &mut f.e.backend.records {
        record["status"] = json!("succeeded");
    }
    f.e.backend.ps.clear();
    f
}

#[test]
fn repair_step_releases_scheduler_completion_and_catches_up_without_duplicate_launch() {
    for already_done in [false, true] {
        let mut f = completed_dependency_repair();
        if already_done {
            // A previous tick recorded completion but died before releasing its parent.
            f.e.edit(&json!(2), |row| row["status"] = json!("done"))
                .unwrap();
        }
        let before = f.get(1);
        let report = f.e.run("repair-step", false).unwrap();
        assert_eq!(f.get(2)["status"], "done");
        assert_eq!(report["dependencies_released"], json!([1]));
        assert_eq!(f.get(1)["status"], "doing");
        assert!(f.get(1)["wait_on"].is_null());
        assert_eq!(f.get(1)["dependency_generation"], 1);
        assert_eq!(f.get(1)["spec"], before["spec"]);
        assert_eq!(
            f.get(1)["scheduler"]["worktree"],
            before["scheduler"]["worktree"]
        );
        assert_eq!(f.e.backend.launched.len(), 1);
        assert_eq!(f.e.backend.launched[0]["item"]["id"], 1);
        f.e.backend.clock += n(&f.cfg["launch_interval_s"]);
        f.e.backend.ps.insert(99, proc(99, 0, "codex.exe"));
        f.e.run("repair-step", false).unwrap();
        assert_eq!(f.e.backend.launched.len(), 1);
        assert!(f.e.backend.notices.is_empty());
        assert!(f.e.backend.cancelled.is_empty());
        assert_eq!(f.e.backend.reply_count, 0);
    }
}

#[test]
fn repair_dependency_release_preserves_prerequisites_scope_and_ownership() {
    for gate in [
        "unfinished",
        "missing",
        "malformed",
        "owner",
        "external",
        "question",
        "dispatch",
        "scope",
        "unknown",
        "protected_worktree",
        "invalid_worktree",
        "pending",
        "running",
        "terminal_live",
    ] {
        let mut f = completed_dependency_repair();
        f.e.edit(&json!(2), |row| row["status"] = json!("done"))
            .unwrap();
        match gate {
            "unfinished" => {
                f.e.edit(&json!(2), |row| row["status"] = json!("block"))
                    .unwrap();
            }
            "missing" => wait_row(&mut f, 1, json!([2, 99])),
            "malformed" => wait_row(&mut f, 1, json!([2, "unknown"])),
            "scope" => f.e.backend.repair_allowed = false,
            "protected_worktree" => {
                f.e.backend.repair_denied = f.get(1)["scheduler"]["worktree"]
                    .as_str()
                    .map(str::to_string);
            }
            "running" => f.e.backend.records[0]["status"] = json!("running"),
            "terminal_live" => {
                f.e.backend.ps.insert(2, proc(2, 0, "codex.exe"));
            }
            _ => {
                f.e.edit(&json!(1), |row| match gate {
                    "owner" => row["status"] = json!("block"),
                    "external" => row["wait_ready"] = json!({"condition":"chatgpt-plus"}),
                    "question" => row["scheduler"]["question"] = json!({"thread_id":"old"}),
                    "dispatch" => row["dispatchable"] = json!(false),
                    "unknown" => row["project"] = json!("other"),
                    "invalid_worktree" => row["scheduler"]["worktree"] = json!({"invalid":true}),
                    "pending" => row["stall_recovery"] = json!({"pending":{"key":"reserved"}}),
                    _ => unreachable!(),
                })
                .unwrap();
            }
        }
        let before = f.get(1);
        f.e.run("repair-step", false).unwrap();
        assert_eq!(f.get(1), before, "{gate}");
        assert!(f.e.backend.launched.is_empty(), "{gate}");
        assert!(f.e.backend.notices.is_empty(), "{gate}");
        assert!(f.e.backend.cancelled.is_empty(), "{gate}");
        assert_eq!(f.e.backend.reply_count, 0, "{gate}");
        if matches!(gate, "running" | "terminal_live") {
            assert!(f.e.backend.repair_calls.is_empty(), "{gate}");
        }
    }
}

#[test]
fn repair_dependency_release_keeps_normal_capacity_gate() {
    let mut f = completed_dependency_repair();
    f.e.backend.m["cpu_percent"] = json!(95);
    let report = f.e.run("repair-step", false).unwrap();
    assert_eq!(report["dependencies_released"], json!([1]));
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(f.get(1)["dependency_ready"], true);
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn repair_dependency_release_admits_unstarted_waits_and_caches_paths_per_tick() {
    let mut f = completed_dependency_repair();
    f.e.edit(&json!(2), |row| row["status"] = json!("done"))
        .unwrap();
    f.e.edit(&json!(1), |row| {
        row["scheduler"] = json!({});
        row["jobs"] = json!([]);
    })
    .unwrap();
    let mut second = item(3, "cc-pc-tools-rs", "wait", &[]);
    second["wait_on"] = json!([2]);
    f.put(second);
    let before = f.get(1);
    f.e.release_repair_waits().unwrap();
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(f.get(3)["status"], "todo");
    assert_eq!(f.get(1)["spec"], before["spec"]);
    assert!(f.get(1)["scheduler"]["worktree"].is_null());
    assert_eq!(f.e.backend.repair_calls.len(), 1);
    f.dispatch();
    assert_eq!(f.get(1)["status"], "doing");
    assert_eq!(f.e.backend.launched.len(), 1);
    // Rechecking an already assigned task never creates a second worker.
    f.e.backend.clock += n(&f.cfg["launch_interval_s"]);
    f.e.backend.ps.insert(99, proc(99, 0, "codex.exe"));
    f.e.release_repair_waits().unwrap();
    f.dispatch();
    for id in [1, 3] {
        assert_eq!(
            f.e.backend
                .launched
                .iter()
                .filter(|launch| launch["item"]["id"] == id)
                .count(),
            1
        );
    }
}

#[test]
fn repair_dependency_release_rechecks_concurrent_stop_dependency_and_project_changes() {
    for change in ["stop", "dependency", "project"] {
        let mut f = completed_dependency_repair();
        f.e.edit(&json!(2), |row| row["status"] = json!("done"))
            .unwrap();
        let mut updated = f.e.todo().unwrap();
        match change {
            "stop" => updated["items"][0]["status"] = json!("block"),
            "dependency" => updated["items"][1]["status"] = json!("block"),
            "project" => updated["projects"]["cc-pc-tools-rs"] = json!("unverified-source"),
            _ => unreachable!(),
        }
        f.e.backend.repair_edit = Some((f.e.todo_path.clone(), updated.clone()));
        f.e.release_repair_waits().unwrap();
        assert_eq!(f.e.todo().unwrap(), updated, "{change}");
        assert!(f.e.backend.launched.is_empty());
    }
}
struct Fixture {
    temp: tempfile::TempDir,
    e: Scheduler<Fake>,
    cfg: Value,
    pri: Value,
    repo: PathBuf,
}
impl Fixture {
    fn new() -> Self {
        let temp = tempfile::tempdir().unwrap();
        let canonical = temp.path().canonicalize().unwrap();
        let root = PathBuf::from(canonical.to_string_lossy().trim_start_matches(r"\\?\"));
        fs::write(root.join("client-projects.txt"), "").unwrap();
        let repo = root.join("repo");
        fs::create_dir(&repo).unwrap();
        let e = Scheduler::new(
            root.join("scheduler"),
            root.join("todo.json"),
            Fake::default(),
        )
        .unwrap();
        write_json(&e.todo_path,&json!({"projects":{"civ6":repo,"GoDD-AO":repo,"other":repo},"items":[],"next_id":1,"stalls":{}}),1).unwrap();
        let mut cfg = defaults();
        cfg["kill_enabled"] = json!(true);
        Self {
            temp,
            e,
            cfg,
            pri: json!({"civ6":1,"GoDD-AO":2}),
            repo,
        }
    }
    fn put(&mut self, mut i: Value) {
        let spec = self.temp.path().join(format!("spec-{}.md", i["id"]));
        fs::write(&spec, "指定された機能を実装してください。").unwrap();
        i["spec"] = json!(spec);
        let mut db = self.e.todo().unwrap();
        push(&mut db["items"], i);
        write_json(&self.e.todo_path, &db, 1).unwrap();
    }
    fn get(&self, id: u64) -> Value {
        arr(&self.e.todo().unwrap()["items"])
            .into_iter()
            .find(|i| i["id"] == id)
            .unwrap()
    }
    fn managed(&mut self, id: u64, root: u32, project: &str, retry: u64) {
        let mut i = item(id, project, "doing", &[]);
        let jid = format!("j{id}");
        i["scheduler"] = json!({"job":jid,"key":id.to_string().repeat(32),"retry_count":retry,"started_s":T-1000.+id as f64,"worktree":self.temp.path().join(format!("repo-wt/todo-{id}-work-{id}")),"branch":format!("scheduler/todo-{id}-work-{id}"),"phase":"running"});
        i["jobs"] = json!([jid]);
        self.put(i);
        self.e.backend.records.push(job(&jid, root, "running"));
        self.e.backend.ps.insert(root, proc(root, 0, "codex.exe"));
    }
    fn reconcile(&mut self) {
        let mut records = self.e.backend.records.clone();
        let ps = self.e.backend.ps.clone();
        self.e.reconcile(&mut records, &ps, &self.cfg).unwrap();
        self.e.backend.records = records;
    }
    fn dispatch(&mut self) {
        self.e.dispatch(&self.cfg, &self.pri).unwrap();
    }
    fn question(&mut self) {
        self.managed(1, 1, "civ6", 1);
        self.e.backend.records[0]["status"] = json!("lost");
        self.e.backend.ps.clear();
        // A genuine owner decision, rather than an automatic retry limit.
        self.e
            .edit(&json!(1), |row| {
                row["status"] = json!("block");
                row["note"] = json!("公開範囲の回答を待っています。");
                row["scheduler"]["notice_pending"] = json!(true);
            })
            .unwrap();
        self.e.notifications().unwrap();
    }
}
#[test]
fn test_dispatch_uses_priority_and_marks_doing() {
    let mut f = Fixture::new();
    for (id, project) in [(1, "other"), (2, "GoDD-AO"), (3, "civ6")] {
        f.put(item(id, project, "todo", &[]));
    }
    f.dispatch();
    assert_eq!(f.e.backend.launched[0]["item"]["id"], 3);
    assert_eq!(f.get(3)["status"], "doing");
    assert_eq!(f.get(3)["jobs"], json!(["launched1"]));
    assert_eq!(n(&f.e.report["allowance_now"]), 0.);
    assert_eq!(f.e.backend.pick_count, 1);
}
#[test]
fn rotation_never_overtakes_higher_priority_products() {
    let mut f = Fixture::new();
    for (id, project) in [(1, "civ6"), (2, "civ6"), (3, "GoDD-AO"), (4, "other")] {
        f.put(item(id, project, "todo", &[]));
    }
    for _ in 0..3 {
        f.dispatch();
        f.e.backend.clock += 11.;
    }
    let ids: Vec<_> =
        f.e.backend
            .launched
            .iter()
            .map(|j| j["item"]["id"].as_u64().unwrap())
            .collect();
    assert_eq!(ids, vec![1, 2, 3]);
}
#[test]
fn recorded_prerequisite_inherits_rank_and_resource_exclusion_still_applies() {
    let mut f = Fixture::new();
    f.pri = json!({"civ6":20,"GoDD-AO":62,"other":100});
    let mut parent = item(1, "civ6", "wait", &[]);
    parent["wait_on"] = json!([3]);
    f.put(parent);
    f.put(item(2, "GoDD-AO", "todo", &[]));
    f.put(item(3, "other", "todo", &[]));
    f.dispatch();
    assert_eq!(f.e.backend.launched[0]["item"]["id"], 3);
}
#[test]
fn targeted_recovery_cannot_overtake_a_ready_higher_department() {
    for blocked in [false, true] {
        let mut f = Fixture::new();
        f.pri = json!({"civ6":20,"other":100});
        f.put(item(1, "civ6", "todo", &["cargo"]));
        f.put(item(3, "other", "todo", &[]));
        if blocked {
            f.e.backend.ps.insert(1, proc(1, 0, "cargo.exe"));
        }
        f.e.dispatch_for(&f.cfg, &f.pri, Some(3)).unwrap();
        assert_eq!(f.e.backend.launched.len(), usize::from(blocked));
        if blocked {
            assert_eq!(f.e.backend.launched[0]["item"]["id"], 3);
        }
    }
}
#[test]
fn preparation_retains_previous_result_session_and_redesigns_repeated_continuations() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    let cwd = s(&f.get(1)["scheduler"]["worktree"]).to_owned();
    f.e.backend.records[0]["status"] = json!("succeeded");
    f.e.backend.records[0]["last_message"] = json!("状態: 継続中\n回帰検査だけ残っています。");
    f.e.backend.records[0]["thread_id"] = json!("12345678-1234-4321-9234-123456789abc");
    f.e.backend.records[0]["cwd"] = json!(cwd);
    f.e.edit(&json!(1), |i| {
        i["scheduler"]["continuation_count"] = json!(2)
    })
    .unwrap();
    f.e.backend.ps.clear();
    f.reconcile();
    f.e.backend.clock += 61.;
    let current = f.get(1);
    let prepared =
        f.e.prepare(&current, &f.repo.to_string_lossy())
            .unwrap()
            .unwrap();
    assert_eq!(
        prepared["scheduler"]["resume_session"],
        "12345678-1234-4321-9234-123456789abc"
    );
    let prompt = fs::read_to_string(s(&prepared["scheduler"]["prompt"])).unwrap();
    assert!(prompt.contains("回帰検査だけ残っています。"));
    assert!(prompt.contains("進め方を設計から見直し"));
    let command = RealBackend::new(f.temp.path().to_path_buf()).launch_command(
        &prepared,
        Path::new("prompt.md"),
        &json!({"model":"gpt-6.1-sol","effort":"high"}),
    );
    assert!(command
        .args
        .windows(2)
        .any(|p| p == ["--resume-session", "12345678-1234-4321-9234-123456789abc"]));
}
#[test]
fn legacy_retry_question_is_retired_even_if_an_agent_already_changed_block_to_doing() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.e.backend.records[0]["status"] = json!("failed");
    f.e.backend.ps.clear();
    f.e.edit(&json!(1), |row| {
        row["note"] = json!("ジョブが失敗。2回止まったため、オーナーに再開方法を確認します。");
        row["scheduler"]["phase"] = json!("blocked");
        row["scheduler"]["question"] = json!({"thread_id":"old-retry-question"});
    })
    .unwrap();
    f.reconcile();
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(f.get(1)["scheduler"]["question_close_pending"], true);
    f.e.answers().unwrap();
    assert_eq!(f.e.backend.closed, vec![json!("old-retry-question")]);
    assert!(f.get(1)["scheduler"]["question"].is_null());
    assert!(f.e.backend.notices.is_empty());
}

#[test]
fn scheduler_implementation_and_readonly_launches_have_distinct_permissions() {
    let f = Fixture::new();
    let backend = RealBackend::new(f.temp.path().to_path_buf());
    for (task, sandbox, expected) in [
        ("implementation", "", "danger-full-access"),
        ("code_review", "", "danger-full-access"),
        ("research", "read-only", "read-only"),
    ] {
        let command = backend.launch_command(
            &json!({"id":1,"title":"作業","project":"demo",
            "scheduler":{"worktree":"D:/work","key":"key"}}),
            Path::new("prompt.md"),
            &json!({"model":"gpt-6.1-sol","effort":"high","task_type":task,"sandbox":sandbox}),
        );
        assert!(command
            .args
            .windows(2)
            .any(|pair| pair == ["--sandbox", expected]));
    }
}
#[test]
fn retiring_legacy_question_preserves_live_worker_and_consumes_its_completion() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.e.edit(&json!(1), |row| {
        row["note"] = json!("ジョブが失敗。2回止まったため、オーナーに再開方法を確認します。");
        row["scheduler"]["phase"] = json!("blocked");
        row["scheduler"]["question"] = json!({"thread_id":"old-retry-question"});
    })
    .unwrap();
    f.reconcile();
    assert_eq!(f.get(1)["status"], "doing");
    assert_eq!(f.get(1)["scheduler"]["job"], "j1");
    f.e.answers().unwrap();
    f.e.backend.records[0]["status"] = json!("succeeded");
    f.e.backend.records[0]["last_message"] = json!("状態: 完了\n元の依頼は完了しました。");
    f.e.backend.ps.clear();
    f.reconcile();
    assert_eq!(f.get(1)["status"], "done");
    f.dispatch();
    assert!(f.e.backend.launched.is_empty());
}
#[test]
fn unread_owner_stop_is_saved_before_retiring_legacy_question_and_stops_its_worker() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.e.edit(&json!(1), |row| {
        row["status"] = json!("block");
        row["note"] = json!("ジョブが失敗。2回止まったため、オーナーに再開方法を確認します。");
        row["scheduler"]["phase"] = json!("blocked");
        row["scheduler"]["question"] = json!({"thread_id":"old-retry-question"});
    })
    .unwrap();
    f.e.backend.owner_reply =
        Some(json!({"id":"owner-stop","text":"停止したままにしてください","stop":true}));
    f.reconcile();
    assert_eq!(f.get(1)["status"], "block");
    assert_eq!(f.get(1)["scheduler"]["answer"], "owner-stop");
    assert_eq!(f.e.backend.cancelled, vec![json!("j1")]);
    f.e.answers().unwrap();
    assert!(f.get(1)["scheduler"]["question"].is_null());
    f.dispatch();
    assert!(f.e.backend.launched.is_empty());
}
#[test]
fn owner_stop_does_not_cancel_a_worker_claimed_only_by_another_items_scheduler() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    let mut other = item(2, "civ6", "doing", &[]);
    other["scheduler"] = json!({"job":"j1","phase":"running"});
    f.put(other);
    f.e.edit(&json!(1), |row| {
        row["status"] = json!("block");
        row["note"] = json!("ジョブが失敗。2回止まったため、オーナーに再開方法を確認します。");
        row["scheduler"]["phase"] = json!("blocked");
        row["scheduler"]["question"] = json!({"thread_id":"old"});
    })
    .unwrap();
    f.e.backend.owner_reply = Some(json!({"id":"stop","text":"停止したまま","stop":true}));
    f.reconcile();
    assert_eq!(f.get(1)["status"], "block");
    assert_eq!(f.get(2)["status"], "doing");
    assert!(f.e.backend.cancelled.is_empty());
}

#[test]
fn revoked_civ6_lock_frees_resource_even_if_old_job_and_game_are_alive() {
    let root = tempfile::tempdir().unwrap();
    let home = root.path();
    let mut old = job("holder", 10, "running");
    old["needs"] = json!(["civ6", "cargo"]);
    let processes = ps(vec![
        proc(10, 0, "codex.exe"),
        proc(11, 0, "CivilizationVI.exe"),
    ]);
    write_json(&home.join("civ6.lock"), &json!({"job":"holder"}), 0).unwrap();
    assert!(
        resources_busy_with_lock(&[], &[old.clone()], &processes, T, home)
            .unwrap()
            .contains("civ6")
    );
    write_json(
        &home.join("civ6-revoked.json"),
        &json!({"owner":{"job":"holder"},"revoked_at":T}),
        0,
    )
    .unwrap();
    fs::remove_file(home.join("civ6.lock")).unwrap();
    let busy = resources_busy_with_lock(&[], &[old.clone()], &processes, T, home).unwrap();
    assert!(!busy.contains("civ6"));
    assert!(busy.contains("cargo"));
    write_json(&home.join("civ6.lock"), &json!({"job":"new-holder"}), 0).unwrap();
    assert!(resources_busy_with_lock(&[], &[old], &processes, T, home)
        .unwrap()
        .contains("civ6"));
}

#[test]
fn dispatch_starts_ready_civ6_item_after_revocation_and_reserves_next_job() {
    let mut f = Fixture::new();
    fs::create_dir_all(f.e.civ6_home()).unwrap();
    write_json(
        &f.e.civ6_home().join("civ6-revoked.json"),
        &json!({"owner":{"job":"holder"},"revoked_at":T}),
        0,
    )
    .unwrap();
    f.e.backend.ps = ps(vec![
        proc(10, 0, "codex.exe"),
        proc(11, 0, "CivilizationVI.exe"),
    ]);
    f.e.backend.records = vec![job("holder", 10, "running")];
    f.e.backend.records.push(job("historical", 20, "running"));
    f.e.backend.ps.insert(20, proc(20, 0, "codex.exe"));
    let mut previous = item(86, "civ6", "doing", &["civ6"]);
    previous["scheduler"] = json!({"job":"holder"});
    previous["jobs"] = json!(["holder"]);
    let ready = item(437, "civ6", "todo", &["civ6"]);
    f.put(previous);
    let mut finished = item(116, "civ6", "block", &["civ6", "cargo"]);
    finished["scheduler"] = json!({"job":"old-job","phase":"finished"});
    finished["jobs"] = json!(["old-job", "historical"]);
    f.put(finished);
    f.put(ready);
    f.e.dispatch(&f.cfg, &f.pri).unwrap();
    assert_eq!(f.e.backend.launched.len(), 1);
    assert!(arr(&f.e.report["busy_resources"]).contains(&json!("cargo")));
    let db = f.e.todo().unwrap();
    let row = arr(&db["items"])
        .into_iter()
        .find(|i| i["id"] == 437)
        .unwrap();
    assert_eq!(row["status"], "doing");
}

#[test]
fn unrelated_current_civ6_reservation_survives_another_owners_revocation() {
    let root = tempfile::tempdir().unwrap();
    write_json(
        &root.path().join("civ6-revoked.json"),
        &json!({"owner":{"job":"revoked"},"revoked_at":T}),
        0,
    )
    .unwrap();
    for (status, phase) in [
        ("block", "question"),
        ("todo", "queued"),
        ("block", "finished"),
    ] {
        let mut current = item(116, "civ6", status, &["civ6"]);
        current["scheduler"] = json!({"job":"current","phase":phase});
        current["jobs"] = json!(["current"]);
        let busy = resources_busy_with_lock(
            &[current],
            &[job("current", 20, "running")],
            &ps(vec![proc(20, 0, "codex.exe")]),
            T,
            root.path(),
        )
        .unwrap();
        assert!(
            busy.contains("civ6"),
            "{status}/{phase} must retain its live reservation"
        );
    }
}

#[test]
fn revoked_current_allocation_is_not_kept_by_its_historical_job_list() {
    let root = tempfile::tempdir().unwrap();
    write_json(
        &root.path().join("civ6-revoked.json"),
        &json!({"owner":{"job":"revoked"},"revoked_at":T}),
        0,
    )
    .unwrap();
    let mut current = item(86, "civ6", "doing", &["civ6", "cargo"]);
    current["scheduler"] = json!({"job":"revoked","phase":"running"});
    current["jobs"] = json!(["revoked", "historical"]);
    let busy = resources_busy_with_lock(
        &[current],
        &[
            job("revoked", 10, "running"),
            job("historical", 20, "running"),
        ],
        &ps(vec![proc(10, 0, "codex.exe"), proc(20, 0, "codex.exe")]),
        T,
        root.path(),
    )
    .unwrap();
    assert!(!busy.contains("civ6"));
    assert!(busy.contains("cargo"));
}
#[test]
fn test_civ6_busy_skips_to_independent_project() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.e.edit(&json!(1), |r| r["needs"] = json!(["civ6"]))
        .unwrap();
    f.put(item(2, "civ6", "todo", &["civ6"]));
    f.put(item(3, "GoDD-AO", "todo", &[]));
    f.dispatch();
    assert_eq!(f.e.backend.launched[0]["item"]["id"], 3);
}
#[test]
fn test_hang_twice_retries_slowly_without_owner_notification() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.reconcile();
    f.e.backend.clock += 1800.;
    f.reconcile();
    assert_eq!(
        f.e.backend.cancelled,
        json!(["j1"]).as_array().unwrap().clone()
    );
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(n(&f.get(1)["scheduler"]["retry_count"]), 1.);
    assert_eq!(f.get(1)["scheduler"]["resume"], true);
    f.e.edit(&json!(1), |r| {
        r["status"] = json!("doing");
        r["scheduler"]["job"] = json!("j1");
    })
    .unwrap();
    f.e.backend.records[0]["status"] = json!("running");
    f.reconcile();
    f.e.backend.clock += 1800.;
    f.reconcile();
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(
        n(&f.get(1)["scheduler"]["not_before"]),
        f.e.backend.clock + 3600.
    );
    f.e.notifications().unwrap();
    f.e.notifications().unwrap();
    assert!(f.e.backend.notices.is_empty());
}
#[test]
fn test_lost_after_reboot_requeues_and_resumes_same_worktree() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    let original = f.get(1)["scheduler"]["worktree"].clone();
    f.e.backend.ps.clear();
    f.reconcile();
    assert_eq!(f.e.backend.records[0]["status"], "lost");
    assert_eq!(f.get(1)["status"], "todo");
    f.e.backend.clock += 60.;
    f.dispatch();
    assert_eq!(
        f.e.backend.launched[0]["item"]["scheduler"]["worktree"],
        original
    );
    let text = s(&f.e.backend.launched[0]["text"]);
    assert!(text.contains(RESUME_INSTRUCTION));
    assert!(text.contains("コミット"));
}
#[test]
fn test_second_lost_uses_same_retry_budget_as_hang() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 1);
    f.e.backend.ps.clear();
    f.reconcile();
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(n(&f.get(1)["scheduler"]["not_before"]), T + 3600.);
    f.e.notifications().unwrap();
    assert!(f.e.backend.notices.is_empty());
}
#[test]
fn test_incomplete_cancel_never_requeues_or_duplicates() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.e.backend.cancel_ok = false;
    let mut j = f.e.backend.records[0].clone();
    f.e.cancel_for(&f.get(1), &mut j, "hung", &f.cfg).unwrap();
    assert_eq!(f.get(1)["status"], "doing");
    assert_eq!(f.get(1)["scheduler"]["pending_cancel"], "hung");
    f.dispatch();
    assert!(f.e.backend.launched.is_empty());
}
#[test]
fn test_cancel_disabled_is_only_a_report() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.cfg["kill_enabled"] = json!(false);
    let mut j = f.e.backend.records[0].clone();
    f.e.cancel_for(&f.get(1), &mut j, "memory", &f.cfg).unwrap();
    assert!(f.e.backend.cancelled.is_empty());
    assert_eq!(f.get(1)["status"], "doing");
    assert_eq!(f.e.report["cancel_dry_run"][0]["job"], "j1");
}
#[test]
fn test_memory_pressure_cancel_low_priority_latest_and_no_hang_budget() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.managed(2, 2, "other", 0);
    f.managed(3, 3, "other", 0);
    let mut m = measurement();
    m["available_gb"] = json!(3);
    let mut jobs = f.e.backend.records.clone();
    f.e.memory_pressure(&m, &mut jobs, &f.e.backend.ps.clone(), &f.pri, &f.cfg)
        .unwrap();
    assert_eq!(f.e.backend.cancelled, vec![json!("j3")]);
    assert_eq!(f.get(3)["status"], "todo");
    assert_eq!(n(&f.get(3)["scheduler"]["retry_count"]), 0.);
    assert_eq!(n(&f.get(3)["scheduler"]["not_before"]), T + 180.);
}
#[test]
fn test_crash_after_launch_is_adopted_without_second_launch() {
    let mut f = Fixture::new();
    f.put(item(1, "civ6", "todo", &[]));
    f.e.backend.crash_after_launch = true;
    assert_eq!(
        f.e.dispatch(&f.cfg, &f.pri),
        Err(Error("KeyboardInterrupt"))
    );
    let key = f.get(1)["scheduler"]["key"].clone();
    assert_eq!(f.e.backend.records[0]["launch_key"], key);
    f.reconcile();
    assert_eq!(f.get(1)["scheduler"]["job"], "launched1");
    f.dispatch();
    assert_eq!(f.e.backend.launched.len(), 1);
}
#[test]
fn test_crash_before_launch_reuses_saved_key_and_selection() {
    let mut f = Fixture::new();
    f.put(item(1, "civ6", "todo", &[]));
    let prepared =
        f.e.prepare(&f.get(1), f.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    f.dispatch();
    assert_eq!(
        f.e.backend.launched[0]["item"]["scheduler"]["key"],
        prepared["scheduler"]["key"]
    );
    assert_eq!(f.e.backend.pick_count, 1);
}
#[test]
fn test_cpu_increases_during_prepare_blocks_launch() {
    let mut f = Fixture::new();
    f.put(item(1, "civ6", "todo", &[]));
    f.e.backend.prepare_pressure = true;
    f.dispatch();
    assert!(f.e.backend.launched.is_empty());
    assert_eq!(f.get(1)["status"], "todo");
}
#[test]
fn test_completion_and_manual_done_are_not_overwritten() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.e.backend.records[0]["status"] = json!("succeeded");
    f.reconcile();
    assert_eq!(f.get(1)["status"], "done");
    f.e.backend.records[0]["status"] = json!("lost");
    f.e.backend.ps.clear();
    f.reconcile();
    assert_eq!(f.get(1)["status"], "done");
}

#[test]
fn unfinished_results_requeue_but_real_owner_questions_block() {
    for (result, expected) in [
        ("状態: 完了\n作業を終えました。", "done"),
        ("状態: 継続中\ncargoの空き待ちです。", "todo"),
        ("状態: 要判断\ncargoの空き待ちです。", "todo"),
        (
            "状態: 要判断\n月額1000円で公開してよいですか。\na. 公開\nb. 停止",
            "block",
        ),
    ] {
        let mut f = Fixture::new();
        f.managed(1, 1, "civ6", 0);
        f.e.backend.records[0]["status"] = json!("succeeded");
        f.e.backend.records[0]["last_message"] = json!(result);
        f.reconcile();
        assert_eq!(f.get(1)["status"], expected);
        if expected == "todo" {
            assert_eq!(f.get(1)["scheduler"]["job"], Value::Null);
            assert_eq!(f.get(1)["scheduler"]["resume"], true);
            f.e.backend.ps.clear();
            f.dispatch();
            assert!(f.e.backend.launched.is_empty());
            f.e.backend.clock += 61.;
            f.dispatch();
            assert_eq!(f.get(1)["status"], "doing");
            assert_eq!(f.e.backend.launched.len(), 1);
            assert!(s(&f.e.backend.launched[0]["text"]).contains("前回のジョブが止まった"));
            assert!(f.e.backend.notices.is_empty());
        }
    }
}
#[test]
fn test_orphan_dry_run_does_not_kill_and_reports_counts() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.e.backend.records[0]["status"] = json!("succeeded");
    f.e.backend.ps.insert(2, proc(2, 1, "node.exe"));
    f.e.backend.ps.insert(3, proc(3, 0, "node.exe"));
    let report =
        f.e.orphans(
            &f.e.backend.records.clone(),
            &f.e.backend.ps.clone(),
            &f.cfg,
            false,
        )
        .unwrap();
    assert_eq!(report["node_before"], 2);
    assert_eq!(report["node_after"], 2);
    assert_eq!(
        arr(&report["candidates"])
            .iter()
            .map(|p| n(&p["pid"]) as u32)
            .collect::<Vec<_>>(),
        [1, 2]
    );
    assert!(f.e.backend.killed.is_empty());
}
#[test]
fn test_detached_worker_delays_relaunch_even_with_dead_roots() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.e.backend.ps.insert(2, proc(2, 1, "codex.exe"));
    f.e.state["observed"] = ownership(&f.e.backend.records, &f.e.backend.ps, &json!({}));
    f.e.backend.ps.remove(&1);
    f.reconcile();
    assert_eq!(f.e.backend.records[0]["status"], "lost");
    assert_eq!(f.get(1)["status"], "doing");
    assert_eq!(f.e.report["recovery_waiting_for_worker"], json!(["j1"]));
}
#[test]
fn test_failed_recovery_step_logs_and_prevents_admission() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.put(item(2, "other", "todo", &[]));
    f.e.backend.ps.clear();
    f.e.backend.fail_lost = true;
    let report = f.e.run("step", false).unwrap();
    assert_eq!(report["step_failures"], json!(["lost j1"]));
    assert!(f.e.backend.launched.is_empty());
    let log = fs::read_to_string(f.e.root.join("scheduler.log")).unwrap();
    assert!(log.contains("RuntimeError"));
    assert!(!log.contains("secret-shaped"));
}
#[test]
fn test_unmanaged_missing_job_is_marked_lost_without_dispatch() {
    let mut f = Fixture::new();
    f.e.backend.records.push(job("j1", 1, "running"));
    f.e.backend.ps.clear();
    f.reconcile();
    assert_eq!(f.e.backend.records[0]["status"], "lost");
    assert!(f.e.backend.launched.is_empty());
}
#[test]
fn test_bad_config_does_not_default_to_killing_or_dispatching() {
    let f = Fixture::new();
    write_json(
        &f.e.root.join("config.json"),
        &json!({"kill_enabled":"false"}),
        2,
    )
    .unwrap();
    assert_eq!(f.e.settings(), Err(Error("ValueError")));
    assert!(f.e.backend.cancelled.is_empty());
}
#[test]
fn test_todo_add_copies_spec_and_does_not_dispatch_plain_item() {
    let f = Fixture::new();
    let original = f.temp.path().join("owner-prompt.md");
    fs::write(&original, "元の指示").unwrap();
    let paths = cc_todo::Paths::at(f.temp.path(), &f.temp.path().join("jobs"));
    let mut db = f.e.todo().unwrap();
    let args = cc_todo::Command::Add {
        subwork: Default::default(),
        project: "civ6".into(),
        title: "指示つきの作業".into(),
        note: "".into(),
        spec: Some(original.clone()),
        model: Some("mock-model".into()),
        effort: Some("high".into()),
        needs: vec!["civ6".into(), "cargo".into()],
        progress: None,
    };
    cc_todo::add_item(&mut db, &args, &paths).unwrap();
    fs::write(&original, "あとで変更").unwrap();
    let copied = &db["items"][0];
    assert_eq!(fs::read_to_string(s(&copied["spec"])).unwrap(), "元の指示");
    assert_eq!(copied["needs"], json!(["civ6", "cargo"]));
    let args = cc_todo::Command::Add {
        subwork: Default::default(),
        project: "civ6".into(),
        title: "指示なし".into(),
        note: "".into(),
        spec: None,
        model: None,
        effort: None,
        needs: vec![],
        progress: None,
    };
    cc_todo::add_item(&mut db, &args, &paths).unwrap();
    assert!(db["items"][1].get("dispatchable").is_none());
}
#[test]
fn test_explicit_model_effort_skip_picker() {
    let mut f = Fixture::new();
    let mut i = item(1, "civ6", "todo", &[]);
    i["model"] = json!("mock-model");
    i["effort"] = json!("high");
    f.put(i);
    f.dispatch();
    assert_eq!(f.e.backend.pick_count, 0);
    assert_eq!(f.e.backend.launched.len(), 1);
}
#[test]
fn todo1842_explicit_model_overrides_saved_selection() {
    let mut f = Fixture::new();
    let mut row = item(1, "civ6", "todo", &[]);
    row["model"] = json!("gpt-6-astra");
    row["effort"] = json!("high");
    row["scheduler"] = json!({"selection":{"model":"gpt-6.1-sol","effort":"max"}});
    f.put(row);
    f.dispatch();
    assert_eq!(f.e.backend.launched.len(), 1);
    let selection = &f.e.backend.launched[0]["item"]["scheduler"]["selection"];
    assert_eq!(selection["model"], "gpt-6-astra");
    assert_eq!(selection["effort"], "high");
    assert_eq!(f.e.backend.pick_count, 0);
}

#[test]
fn durable_automatic_cancel_reconciles_and_prepares_original_work_without_pending() {
    for status in ["cancelled", "canceled"] {
        let mut f = Fixture::new();
        f.managed(1, 1, "civ6", 0);
        let original = f.get(1);
        let cwd = s(&original["scheduler"]["worktree"]).to_owned();
        fs::create_dir_all(&cwd).unwrap();
        let uncommitted = Path::new(&cwd).join("uncommitted.txt");
        fs::write(&uncommitted, "saved work").unwrap();
        let source = &mut f.e.backend.records[0];
        source["status"] = json!(status);
        source["recovery_cancel"] =
            json!({"key":"original-intent","committed_at":"2026-10-11T02:01:36+09:00"});
        source["cwd"] = json!(cwd);
        source["thread_id"] = json!("12345678-1234-4321-9234-123456789abc");
        source["model"] = json!("gpt-6.1-sol");
        source["effort"] = json!("high");
        source["sandbox"] = json!("read-only");
        let saved_source = source.clone();
        f.e.backend.ps.clear();
        // The same job is eligible in the watchdog after item-side intent vanished.
        let mut copy = f.e.todo().unwrap();
        cc_todo::recovery::plan(
            &mut copy,
            std::slice::from_ref(&saved_source),
            &Processes::new(),
            T + 2000.,
            "",
        )
        .unwrap();
        assert!(copy["items"][0]["stall_recovery"]["pending"].is_object());
        f.reconcile();
        let row = f.get(1);
        assert_eq!(row["status"], "todo");
        assert_eq!(row["scheduler"]["last_job"], "j1");
        assert_eq!(
            row["scheduler"]["worktree"],
            original["scheduler"]["worktree"]
        );
        assert_eq!(row["spec"], original["spec"]);
        assert_eq!(f.e.backend.records[0], saved_source);
        f.e.backend.clock += 301.;
        let prepared =
            f.e.prepare(&f.get(1), &f.repo.to_string_lossy())
                .unwrap()
                .unwrap();
        assert_eq!(
            prepared["scheduler"]["resume_session"],
            saved_source["thread_id"]
        );
        assert_eq!(prepared["scheduler"]["selection"]["model"], "gpt-6.1-sol");
        assert_eq!(prepared["scheduler"]["selection"]["effort"], "high");
        assert_eq!(prepared["scheduler"]["selection"]["sandbox"], "read-only");
        assert_eq!(fs::read_to_string(&uncommitted).unwrap(), "saved work");
    }
}

#[test]
fn automatic_cancel_never_overrides_explicit_stop_unknown_cancel_or_question() {
    for hold in [
        "manual",
        "person",
        "cancel_requested",
        "item_stopped",
        "question",
        "unknown",
        "owner_result",
    ] {
        let mut f = Fixture::new();
        f.managed(1, 1, "civ6", 0);
        let source = &mut f.e.backend.records[0];
        source["status"] = json!("cancelled");
        if hold != "unknown" {
            source["recovery_cancel"] =
                json!({"key":"original-intent","committed_at":"2026-10-11T02:01:36+09:00"});
        }
        match hold {
            "manual" | "person" => source["cancel_reason"] = json!(hold),
            "cancel_requested" => source["cancel_requested"] = json!(true),
            "owner_result" => {
                source["last_message"] =
                    json!("状態: 要判断\n公開してよいですか。\na. 公開\nb. 停止")
            }
            _ => {}
        }
        f.e.edit(&json!(1), |row| {
            if hold == "item_stopped" {
                row["stopped"] = json!(true);
            }
            if hold == "question" {
                row["scheduler"]["question"] = json!("saved-question");
                row["note"] = json!("公開先の回答を待っています");
            }
        })
        .unwrap();
        let mut copy = f.e.todo().unwrap();
        assert!(
            cc_todo::recovery::plan(
                &mut copy,
                &f.e.backend.records,
                &Processes::new(),
                T + 2000.,
                ""
            )
            .unwrap()
            .is_empty(),
            "{hold}"
        );
        f.e.backend.ps.clear();
        f.reconcile();
        assert_eq!(f.get(1)["status"], "block", "{hold}");
        f.e.backend.clock += 3601.;
        f.dispatch();
        assert!(f.e.backend.launched.is_empty(), "{hold}");
        if hold == "question" {
            assert_eq!(f.get(1)["scheduler"]["question"], "saved-question");
            assert_eq!(f.get(1)["note"], "公開先の回答を待っています");
        }
    }
}

#[test]
fn automatic_cancel_keeps_dependency_and_explicit_launch_deadline() {
    for dependency in [false, true] {
        let mut f = Fixture::new();
        f.managed(1, 1, "civ6", 0);
        f.put(item(2, "other", "todo", &[]));
        f.e.backend.records[0]["status"] = json!("cancelled");
        f.e.backend.records[0]["recovery_cancel"] =
            json!({"key":"intent","committed_at":"2026-10-11T02:01:36+09:00"});
        f.e.edit(&json!(1), |row| {
            row["scheduler"]["not_before"] = json!(T + 10000.);
            if dependency {
                row["wait_on"] = json!([2]);
            }
        })
        .unwrap();
        let mut copy = f.e.todo().unwrap();
        let selected = cc_todo::recovery::plan(
            &mut copy,
            &f.e.backend.records,
            &Processes::new(),
            T + 2000.,
            "",
        )
        .unwrap();
        assert!(!selected.contains(&1));
        f.e.backend.ps.clear();
        f.reconcile();
        assert_eq!(f.get(1)["status"], if dependency { "wait" } else { "todo" });
        assert_eq!(f.get(1)["scheduler"]["not_before"], T + 10000.);
        if dependency {
            assert_eq!(f.get(1)["wait_on"], json!([2]));
        }
    }
}

#[test]
fn scheduler_automatic_stop_uses_recovery_cancellation_and_persists_its_intent() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    let mut source = f.e.backend.records[0].clone();
    f.e.cancel_for(&f.get(1), &mut source, "memory", &f.cfg)
        .unwrap();
    assert_eq!(source["status"], "cancelled");
    assert_ne!(source["cancel_requested"], true);
    assert!(cc_job_policy::cancellation::recovery_cancel(
        &json!({}),
        &source
    ));
    assert_eq!(f.get(1)["status"], "todo");
    assert!(f.get(1)["scheduler"].get("pending_cancel").is_none());
}

#[test]
fn todo1842_changed_model_does_not_resume_previous_conversation() {
    for (requested, retained) in [("gpt-6-astra", false), ("gpt-6.1-sol", true)] {
        let mut f = Fixture::new();
        f.managed(1, 1, "civ6", 0);
        let cwd = s(&f.get(1)["scheduler"]["worktree"]).to_owned();
        f.e.backend.records[0]["status"] = json!("succeeded");
        f.e.backend.records[0]["last_message"] = json!("状態: 継続中\n指定モデルで再開する");
        f.e.backend.records[0]["thread_id"] = json!("12345678-1234-4321-9234-123456789abc");
        f.e.backend.records[0]["cwd"] = json!(cwd);
        f.e.backend.records[0]["model"] = json!("gpt-6.1-sol");
        f.e.backend.records[0]["effort"] = json!("high");
        f.e.backend.ps.clear();
        f.reconcile();
        f.e.backend.clock += 61.;
        f.e.edit(&json!(1), |row| {
            row["model"] = json!(requested);
            row["effort"] = json!("high");
        })
        .unwrap();
        let prepared =
            f.e.prepare(&f.get(1), &f.repo.to_string_lossy())
                .unwrap()
                .unwrap();
        assert_eq!(
            prepared["scheduler"]["resume_session"].is_string(),
            retained
        );
        assert_eq!(prepared["scheduler"]["selection"]["model"], requested);
    }
}

#[test]
fn todo1842_setting_change_during_last_sample_defers_and_reprepares() {
    for during_prepare in [false, true] {
        let mut f = Fixture::new();
        f.managed(1, 1, "civ6", 0);
        let cwd = s(&f.get(1)["scheduler"]["worktree"]).to_owned();
        f.e.backend.records[0]["status"] = json!("succeeded");
        f.e.backend.records[0]["last_message"] = json!("状態: 継続中\n検証する");
        f.e.backend.records[0]["thread_id"] = json!("12345678-1234-4321-9234-123456789abc");
        f.e.backend.records[0]["cwd"] = json!(cwd);
        f.e.backend.records[0]["model"] = json!("gpt-6.1-sol");
        f.e.backend.records[0]["effort"] = json!("high");
        f.e.backend.ps.clear();
        f.reconcile();
        f.e.edit(&json!(1), |row| {
            row["model"] = json!("gpt-6.1-sol");
            row["effort"] = json!("high");
        })
        .unwrap();
        f.e.backend.measure_count = 0;
        f.e.backend.change_during_prepare = during_prepare;
        f.e.backend.clock += 61.;
        f.e.backend.settings_change = Some((
            f.e.todo_path.clone(),
            json!({"model":"gpt-6-astra","effort":"max"}),
        ));
        f.dispatch();
        assert!(f.e.backend.launched.is_empty());
        f.dispatch();
        assert_eq!(f.e.backend.launched.len(), 1);
        let launched = &f.e.backend.launched[0]["item"]["scheduler"];
        assert_eq!(launched["selection"]["model"], "gpt-6-astra");
        assert_eq!(launched["selection"]["effort"], "max");
        assert!(launched["resume_session"].is_null());
    }
}

#[test]
fn todo1842_unavailable_explicit_model_is_retained_without_launch() {
    let mut f = Fixture::new();
    let mut row = item(1, "civ6", "todo", &[]);
    row["model"] = json!("gpt-6.1-luna");
    row["effort"] = json!("high");
    f.put(row);
    assert_eq!(
        f.e.dispatch(&f.cfg, &f.pri).unwrap_err().0,
        "ModelUnavailable"
    );
    assert!(f.e.backend.launched.is_empty());
    assert_eq!(f.e.backend.pick_count, 0);
    assert_eq!(f.get(1)["model"], "gpt-6.1-luna");
    assert_eq!(f.get(1)["status"], "todo");
}
struct GitFake {
    calls: Vec<Invocation>,
    existing: Option<(PathBuf, String, PathBuf)>,
}

#[test]
fn preparation2164_non_git_documents_keep_source_and_dedicated_workspace() {
    let mut f = Fixture::new();
    f.put(item(1, "other", "todo", &[]));
    fs::write(f.repo.join("reference.txt"), "original").unwrap();
    let row =
        f.e.prepare(&f.get(1), f.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    let target = PathBuf::from(s(&row["scheduler"]["worktree"]));
    fs::remove_dir(&target).unwrap();
    ensure_worktree(&mut SystemRunner, f.repo.to_str().unwrap(), &row).unwrap();
    assert!(target.is_dir());
    assert!(!target.join(".git").exists());
    assert_eq!(
        fs::read_to_string(f.repo.join("reference.txt")).unwrap(),
        "original"
    );
}

#[test]
fn preparation2164_explicit_resume_retains_resource_deadline() {
    let mut row = json!({"status":"todo","scheduler":{"job":"cancelled","phase":"finished","not_before":1791820800.},"processed_results":{"cancelled":{"reason":"superseded"}}});
    resume_without_worker(&mut row);
    assert_eq!(row["scheduler"]["not_before"], 1791820800.);
    assert_eq!(
        row["processed_results"]["cancelled"]["reason"],
        "superseded"
    );
}

#[test]
fn preparation2164_owner_answer_keeps_known_resource_wait_deadline() {
    let mut f = Fixture::new();
    let mut row = item(1, "other", "block", &[]);
    row["scheduler"] =
        json!({"question":{"thread_id":"saved-question"},"phase":"blocked","not_before":T+86400.});
    f.put(row);
    f.e.backend.owner_reply = Some(json!({"id":"answer","text":"原因を直して再開","stop":false}));
    f.e.answers().unwrap();
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(f.get(1)["scheduler"]["not_before"], T + 86400.);
    assert!(f
        .e
        .prepare(&f.get(1), f.repo.to_str().unwrap())
        .unwrap()
        .is_none());
}

#[test]
fn preparation2164_prepare_respects_wait_even_after_selection_change() {
    let mut f = Fixture::new();
    let mut row = item(1, "other", "todo", &[]);
    row["model"] = json!("anthropic/claude-haiku-5-5");
    row["scheduler"] = json!({"not_before":T+3600.,"selection_input":{"model":"claude-haiku-5-5"}});
    f.put(row);
    assert!(f
        .e
        .prepare(&f.get(1), f.repo.to_str().unwrap())
        .unwrap()
        .is_none());
    assert_eq!(f.get(1)["scheduler"]["not_before"], T + 3600.);
    assert_eq!(f.e.backend.pick_count, 0);
}

#[test]
fn preparation2164_failure_does_not_stop_independent_dispatch() {
    let mut f = Fixture::new();
    f.put(item(1, "civ6", "todo", &[]));
    f.put(item(2, "other", "todo", &[]));
    f.e.backend.worktree_failure = Some(1);
    f.e.dispatch(&f.cfg, &f.pri).unwrap();
    assert_eq!(f.e.backend.launched.len(), 1);
    assert_eq!(f.e.backend.launched[0]["item"]["id"], 2);
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(
        f.get(1)["scheduler"]["preparation_failure"]["reason"],
        "origin_missing"
    );
}

#[test]
fn preparation2164_http429_parks_worker_despite_supervisor_cpu_and_replays_once() {
    let mut f = Fixture::new();
    f.managed(1, 1, "other", 0);
    f.e.backend.records[0]["last_error"] =
        json!("HTTP 429 Too Many Requests private/path TOKEN=secret");
    f.e.backend.records[0]["retry_not_before"] = json!(T + 86400.);
    f.e.backend.ps.get_mut(&1).unwrap()["cpu"] = json!(0.01);
    f.reconcile();
    let saved = f.get(1);
    assert_eq!(f.e.backend.cancelled, vec![json!("j1")]);
    assert_eq!(saved["status"], "todo");
    assert_eq!(saved["scheduler"]["not_before"], T + 86400.);
    assert_eq!(saved["scheduler"]["retry_count"], 0);
    assert_eq!(saved["processed_results"]["j1"]["reason"], "resource_wait");
    assert!(!saved["note"].as_str().unwrap().contains("secret"));
    f.e.edit(&json!(1), |row| {
        row["status"] = json!("doing");
        row["scheduler"]["job"] = json!("j1");
    })
    .unwrap();
    f.reconcile();
    assert_eq!(f.get(1)["scheduler"]["not_before"], T + 86400.);
    assert_ne!(f.get(1)["status"], "block");
    assert_eq!(f.e.backend.cancelled.len(), 1);
}

#[test]
fn preparation2164_quota_evidence_waits_until_reset_without_model_substitution() {
    use cc_scheduler::preparation::*;
    let quota = json!({"anthropic":{"complete":true,"accounts":[{"used_percent":100.,"reset_at":1791820800.}]}});
    assert_eq!(
        cached_exhausted(&quota, "anthropic/claude-haiku-5-5", T),
        Some(1791820800.)
    );
    assert_eq!(
        cached_exhausted(&quota, "anthropic/claude-haiku-5-5", 1791820800.),
        None
    );
    assert_eq!(cached_exhausted(&quota, "mock-model", T), None);
    let several = json!({"anthropic":{"complete":true,"accounts":[{"used_percent":100.,"reset_at":T+3600.},{"used_percent":100.,"reset_at":T+7200.}]}});
    assert_eq!(
        cached_exhausted(&several, "anthropic/claude-haiku-5-5", T),
        Some(T + 3600.)
    );
    assert_eq!(
        cached_exhausted(&several, "anthropic/claude-haiku-5-5", T + 3600.),
        None
    );
    for (stderr, code, reason) in [
        ("not a git repository private/path", 1, "NonGit"),
        ("No such remote 'origin'", 1, "OriginMissing"),
        ("HTTP 429 private-token", 1, "ResourceWait"),
        ("secret command arguments", 2, "InputRejected"),
    ] {
        assert_eq!(
            classify_output(&Output {
                code,
                stdout: String::new(),
                stderr: stderr.into()
            })
            .0,
            reason
        );
    }
}

#[test]
fn preparation2164_exact_ownership_excludes_prompt_and_pid_matches() {
    use cc_scheduler::preparation::*;
    let row = json!({"id":1,"jobs":["other","owned"]});
    let jobs = vec![
        json!({"id":"other","cwd":"elsewhere","prompt":"todo 1","pid":1}),
        json!({"id":"owned","cwd":"original","related_todo_ids":[1]}),
    ];
    assert_eq!(workspace_source(&row, &jobs).unwrap()["id"], "owned");
    assert!(!owns_workspace(&row, &jobs[0]));
    assert!(workspace_source(&row, &jobs[..1]).is_none());
}

#[test]
fn preparation2164_reuses_owned_terminal_session_only_without_other_worker() {
    use cc_scheduler::preparation::*;
    let row = json!({"id":1,"scheduler":{"resume":true}});
    let job = json!({"id":"original","todo_id":1,"status":"lost","cwd":"D:/fixture-original","thread_id":"12345678-1234-4321-9234-123456789abc"});
    assert_eq!(
        recovery_session(&row, &job, "D:/fixture-original", &[]),
        job["thread_id"].as_str()
    );
    let mut wrong = job.clone();
    wrong["todo_id"] = json!(2);
    assert!(recovery_session(&row, &wrong, "D:/fixture-original", &[]).is_none());
    assert!(recovery_session(&row, &job, "D:/elsewhere", &[]).is_none());
    let concurrent = json!({"status":"running","resume_session":job["thread_id"]});
    assert!(recovery_session(&row, &job, "D:/fixture-original", &[concurrent]).is_none());
}

#[test]
fn preparation2164_bare_claude_routes_to_subscription_provider_with_original_scope() {
    let mut f = Fixture::new();
    let mut row = item(1, "other", "todo", &[]);
    row["model"] = json!("claude-haiku-5-5");
    row["effort"] = json!("high");
    f.put(row);
    f.dispatch();
    assert_eq!(
        f.e.backend.launched[0]["item"]["scheduler"]["selection"]["model"],
        "anthropic/claude-haiku-5-5"
    );
    assert!(f.e.backend.launched[0]["text"]
        .as_str()
        .unwrap()
        .contains(f.repo.to_str().unwrap()));
    assert_eq!(f.get(1)["model"], "claude-haiku-5-5");
}

#[test]
fn preparation2164_preserves_dirty_original_git_workspace_and_intermediate_commit() {
    use cc_scheduler::preparation::*;
    let f = Fixture::new();
    let mut runner = SystemRunner;
    run_git(&mut runner, &f.repo, &["init", "--quiet"]).unwrap();
    fs::write(f.repo.join("original.txt"), "committed").unwrap();
    run_git(&mut runner, &f.repo, &["add", "original.txt"]).unwrap();
    run_git(
        &mut runner,
        &f.repo,
        &[
            "-c",
            "user.name=fixture",
            "-c",
            "user.email=fixture@example.invalid",
            "commit",
            "--quiet",
            "-m",
            "original",
        ],
    )
    .unwrap();
    let original = f.temp.path().join("agent-workspaces").join("agent-todo-1");
    run_git(
        &mut runner,
        &f.repo,
        &[
            "worktree",
            "add",
            "-b",
            "agent/original",
            original.to_str().unwrap(),
        ],
    )
    .unwrap();
    fs::write(original.join("original.txt"), "intermediate").unwrap();
    run_git(&mut runner, &original, &["add", "original.txt"]).unwrap();
    run_git(
        &mut runner,
        &original,
        &[
            "-c",
            "user.name=fixture",
            "-c",
            "user.email=fixture@example.invalid",
            "commit",
            "--quiet",
            "-m",
            "intermediate",
        ],
    )
    .unwrap();
    fs::write(original.join("unfinished.txt"), "dirty").unwrap();
    let head = run_git(&mut runner, &original, &["rev-parse", "HEAD"]).unwrap();
    let status = run_git(&mut runner, &original, &["status", "--porcelain"]).unwrap();
    let mut row = json!({"id":1,"jobs":["owned"],"scheduler":{"worktree":f.temp.path().join("missing"),"branch":"scheduler/todo-1-stale"}});
    let jobs = vec![json!({"id":"owned","todo_id":1,"cwd":original})];
    let recovered = recover_workspace(&mut runner, f.repo.to_str().unwrap(), &row, &jobs)
        .unwrap()
        .unwrap();
    merge(&mut row["scheduler"], recovered);
    ensure_workspace(&mut runner, f.repo.to_str().unwrap(), &row, &jobs).unwrap();
    assert_eq!(
        run_git(&mut runner, &original, &["rev-parse", "HEAD"]).unwrap(),
        head
    );
    assert_eq!(
        run_git(&mut runner, &original, &["status", "--porcelain"]).unwrap(),
        status
    );
    assert_eq!(
        fs::read_to_string(original.join("unfinished.txt")).unwrap(),
        "dirty"
    );
    let mut wrong = jobs.clone();
    wrong[0]["todo_id"] = json!(2);
    assert!(ensure_workspace(&mut runner, f.repo.to_str().unwrap(), &row, &wrong).is_err());
    row["scheduler"]["branch"] = json!("wrong");
    assert_eq!(
        ensure_workspace(&mut runner, f.repo.to_str().unwrap(), &row, &jobs)
            .unwrap_err()
            .0,
        "BranchMismatch"
    );
}
fn reserved_branch_fixture() -> (Fixture, Value, String) {
    let f = Fixture::new();
    let mut runner = SystemRunner;
    run_git(&mut runner, &f.repo, &["init", "--quiet"]).unwrap();
    fs::write(f.repo.join("source.txt"), "reserved input").unwrap();
    run_git(&mut runner, &f.repo, &["add", "source.txt"]).unwrap();
    let commit = |runner: &mut SystemRunner, args: &[&str]| {
        let mut command = vec![
            "-c",
            "user.name=fixture",
            "-c",
            "user.email=fixture@example.invalid",
            "commit",
            "--quiet",
        ];
        command.extend_from_slice(args);
        run_git(runner, &f.repo, &command).unwrap();
    };
    commit(&mut runner, &["-m", "reserved input"]);
    let head = run_git(&mut runner, &f.repo, &["rev-parse", "HEAD"]).unwrap();
    let branch = "scheduler/todo-1-reserved";
    run_git(&mut runner, &f.repo, &["branch", branch]).unwrap();
    fs::write(f.repo.join("source.txt"), "main advanced").unwrap();
    commit(&mut runner, &["-am", "advance base"]);
    let target = PathBuf::from(format!("{}-wt", f.repo.display())).join("todo-1-reserved");
    let row = json!({"id":1,"scheduler":{"worktree":target,"branch":branch}});
    (f, row, head)
}

fn checkpoint_fixture() -> (
    Fixture,
    cc_scheduler::workspace_checkpoint::Request,
    PathBuf,
) {
    let (mut f, workspace, _) = reserved_branch_fixture();
    let mut runner = SystemRunner;
    cc_scheduler::preparation::ensure_workspace(
        &mut runner,
        f.repo.to_str().unwrap(),
        &workspace,
        &[],
    )
    .unwrap();
    let target = PathBuf::from(s(&workspace["scheduler"]["worktree"]));
    run_git(
        &mut runner,
        &target,
        &["switch", "-c", "scheduler/todo-1-followup"],
    )
    .unwrap();
    fs::write(target.join("source.txt"), "checkpointed work").unwrap();
    run_git(&mut runner, &target, &["add", "source.txt"]).unwrap();
    run_git(
        &mut runner,
        &target,
        &[
            "-c",
            "user.name=fixture",
            "-c",
            "user.email=fixture@example.invalid",
            "commit",
            "--quiet",
            "-m",
            "followup",
        ],
    )
    .unwrap();
    fs::write(target.join("unfinished.txt"), "keep this pending work").unwrap();
    let head = run_git(&mut runner, &target, &["rev-parse", "HEAD"]).unwrap();
    let mut row = item(1, "other", "wait", &["cargo"]);
    row["scheduler"] = workspace["scheduler"].clone();
    row["scheduler"]["last_job"] = json!("owned");
    row["scheduler"]["resume_session"] = json!("12345678-1234-4321-9234-123456789abc");
    row["scheduler"]["not_before"] = json!(T + 3600.);
    row["jobs"] = json!(["owned"]);
    row["wait_on"] = json!([2]);
    row["wait_ready"] = json!({"condition":"retry-time","not_before":T+7200.});
    row["note"] = json!("Keep previous implementation and answers.");
    row["progress"] = json!(40);
    f.put(row);
    f.e.backend.repair_allowed = true;
    f.e.backend.records = vec![json!({"id":"owned","todo_id":1,"status":"succeeded","cwd":target})];
    let request = cc_scheduler::workspace_checkpoint::Request {
        id: 1,
        job: "owned".into(),
        expected_branch: "scheduler/todo-1-reserved".into(),
        branch: "scheduler/todo-1-followup".into(),
        head,
    };
    (f, request, target)
}

#[test]
fn workspace_checkpoint_restores_resume_without_changing_work_or_waits() {
    use cc_scheduler::{preparation::*, workspace_checkpoint::checkpoint};
    let (mut f, request, target) = checkpoint_fixture();
    let mut runner = SystemRunner;
    let before = f.get(1);
    let status = run_git(&mut runner, &target, &["status", "--porcelain"]).unwrap();
    assert_eq!(
        recover_workspace(
            &mut runner,
            f.repo.to_str().unwrap(),
            &before,
            &f.e.backend.records
        )
        .unwrap_err()
        .0,
        "BranchMismatch"
    );
    let report = checkpoint(&mut f.e, &mut runner, &request).unwrap();
    assert_eq!(report["saved"], true);
    assert_eq!(report["launched"], 0);
    let mut after = f.get(1);
    ensure_workspace(
        &mut runner,
        f.repo.to_str().unwrap(),
        &after,
        &f.e.backend.records,
    )
    .unwrap();
    assert_eq!(
        run_git(&mut runner, &target, &["rev-parse", "HEAD"]).unwrap(),
        request.head
    );
    assert_eq!(
        run_git(&mut runner, &target, &["status", "--porcelain"]).unwrap(),
        status
    );
    assert_eq!(
        fs::read_to_string(target.join("unfinished.txt")).unwrap(),
        "keep this pending work"
    );
    assert!(!run_git(
        &mut runner,
        &target,
        &["rev-parse", "refs/heads/scheduler/todo-1-reserved"]
    )
    .unwrap()
    .is_empty());
    after["scheduler"]["branch"] = before["scheduler"]["branch"].clone();
    remove(&mut after["scheduler"], "workspace_checkpoint");
    remove(&mut after, "updated");
    assert_eq!(after, before);
    let saved = f.get(1);
    assert_eq!(
        checkpoint(&mut f.e, &mut runner, &request).unwrap()["saved"],
        false
    );
    assert_eq!(f.get(1), saved);
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn workspace_checkpoint_rejects_unverified_identity_scope_or_git_state() {
    for gate in [
        "job",
        "ownership",
        "cwd",
        "scope",
        "worktree_scope",
        "head",
        "branch",
        "expected",
        "other_task_branch",
        "detached",
        "other_worker",
        "terminal_but_alive",
        "done",
        "unknown_state",
    ] {
        let (mut f, mut request, target) = checkpoint_fixture();
        match gate {
            "job" => request.job = "missing".into(),
            "ownership" => f.e.backend.records[0]["todo_id"] = json!(2),
            "cwd" => f.e.backend.records[0]["cwd"] = json!(f.repo),
            "scope" => f.e.backend.repair_allowed = false,
            "worktree_scope" => f.e.backend.repair_denied = Some(target.to_str().unwrap().into()),
            "head" => request.head = "f".repeat(40),
            "branch" => request.branch = "scheduler/todo-1-missing".into(),
            "expected" => request.expected_branch = "scheduler/todo-1-stale".into(),
            "other_task_branch" => request.branch = "scheduler/todo-2-followup".into(),
            "detached" => {
                run_git(&mut SystemRunner, &target, &["checkout", "--detach"]).unwrap();
            }
            "other_worker" | "terminal_but_alive" => {
                let mut other = job(
                    "other",
                    9,
                    if gate == "other_worker" {
                        "running"
                    } else {
                        "succeeded"
                    },
                );
                other["todo_id"] = json!(1);
                f.e.backend.records.push(other);
                f.e.backend.ps.insert(9, proc(9, 0, "codex.exe"));
            }
            _ => {
                f.e.edit(&json!(1), |row| {
                    row["status"] = json!(if gate == "done" {
                        "done"
                    } else {
                        "recovery_unverified"
                    })
                })
                .unwrap();
            }
        }
        let before = f.get(1);
        assert!(
            cc_scheduler::workspace_checkpoint::checkpoint(&mut f.e, &mut SystemRunner, &request)
                .is_err(),
            "{gate}"
        );
        assert_eq!(f.get(1), before, "{gate}");
        assert!(f.e.backend.launched.is_empty(), "{gate}");
    }
}

#[test]
fn workspace_checkpoint_accepts_only_the_current_live_source() {
    for current in [false, true] {
        let (mut f, request, _) = checkpoint_fixture();
        f.e.backend.records[0]["status"] = json!("running");
        f.e.backend.records[0]["pid"] = json!(9);
        f.e.backend.records[0]["pid_created"] = proc(9, 0, "codex.exe")["created"].clone();
        f.e.backend.ps.insert(9, proc(9, 0, "codex.exe"));
        if current {
            f.e.edit(&json!(1), |row| row["scheduler"]["job"] = json!("owned"))
                .unwrap();
        }
        let before = f.get(1);
        let result =
            cc_scheduler::workspace_checkpoint::checkpoint(&mut f.e, &mut SystemRunner, &request);
        assert_eq!(result.is_ok(), current);
        if current {
            assert_eq!(f.get(1)["scheduler"]["branch"], request.branch);
            assert_eq!(f.get(1)["scheduler"]["job"], "owned");
        } else {
            assert_eq!(f.get(1), before);
        }
        assert!(f.e.backend.launched.is_empty());
    }
}

#[test]
fn workspace_checkpoint_preserves_intervening_owner_stop() {
    for project_change in [false, true] {
        let (mut f, request, _) = checkpoint_fixture();
        let mut changed = f.e.todo().unwrap();
        if project_change {
            changed["projects"]["other"] = json!(f.temp.path().join("another-project"));
        } else {
            changed["items"][0]["status"] = json!("block");
            changed["items"][0]["stopped"] = json!(true);
        }
        f.e.backend.repair_edit = Some((f.e.todo_path.clone(), changed.clone()));
        assert!(cc_scheduler::workspace_checkpoint::checkpoint(
            &mut f.e,
            &mut SystemRunner,
            &request
        )
        .is_err());
        assert_eq!(f.e.todo().unwrap(), changed);
        assert!(f.e.backend.launched.is_empty());
    }
}

#[test]
fn preparation2164_restores_reserved_branch_without_replacing_its_commit() {
    let (f, row, head) = reserved_branch_fixture();
    let target = Path::new(s(&row["scheduler"]["worktree"]));
    let mut runner = SystemRunner;
    cc_scheduler::preparation::ensure_workspace(&mut runner, f.repo.to_str().unwrap(), &row, &[])
        .unwrap();
    assert_eq!(
        run_git(&mut runner, target, &["rev-parse", "HEAD"]).unwrap(),
        head
    );
    assert_eq!(
        fs::read_to_string(target.join("source.txt")).unwrap(),
        "reserved input"
    );
    assert_eq!(
        fs::read_to_string(f.repo.join("source.txt")).unwrap(),
        "main advanced"
    );
    assert_ne!(
        run_git(&mut runner, &f.repo, &["rev-parse", "HEAD"]).unwrap(),
        head
    );
}

#[test]
fn preparation2164_missing_worker_receipt_never_reconstructs_only_committed_bytes() {
    let (f, mut row, head) = reserved_branch_fixture();
    let target = row["scheduler"]["worktree"].clone();
    row["scheduler"]["workspace_receipt"] = json!({"job":"source","todo_id":1,"cwd":target});
    let jobs = [json!({"id":"source","todo_id":1,"cwd":target})];
    let mut runner = SystemRunner;
    let error = cc_scheduler::preparation::ensure_workspace(
        &mut runner,
        f.repo.to_str().unwrap(),
        &row,
        &jobs,
    )
    .unwrap_err();
    assert_eq!(error.0, "CwdMissing");
    assert!(!Path::new(target.as_str().unwrap()).exists());
    assert_eq!(
        run_git(
            &mut runner,
            &f.repo,
            &["rev-parse", "refs/heads/scheduler/todo-1-reserved"]
        )
        .unwrap(),
        head
    );
}

#[test]
fn preparation2164_reserved_ref_lookup_failure_is_not_a_new_branch() {
    struct RefFailure {
        observed: bool,
    }
    impl Runner for RefFailure {
        fn run(&mut self, command: &Invocation) -> Result<Output> {
            if command.args.iter().any(|arg| arg == "show-ref") {
                self.observed = true;
                return Ok(Output {
                    code: 1,
                    stdout: String::new(),
                    stderr: "ref lookup failed".into(),
                });
            }
            SystemRunner.run(command)
        }
    }
    let (f, row, head) = reserved_branch_fixture();
    let mut runner = RefFailure { observed: false };
    let error = cc_scheduler::preparation::ensure_workspace(
        &mut runner,
        f.repo.to_str().unwrap(),
        &row,
        &[],
    )
    .unwrap_err();
    assert!(runner.observed);
    assert_eq!(error.0, "ToolFailed");
    assert!(!Path::new(s(&row["scheduler"]["worktree"])).exists());
    assert_eq!(
        run_git(
            &mut SystemRunner,
            &f.repo,
            &["rev-parse", "refs/heads/scheduler/todo-1-reserved"]
        )
        .unwrap(),
        head
    );
}

#[test]
fn preparation2164_reserved_branch_in_another_checkout_keeps_its_dirty_files() {
    let (f, row, _) = reserved_branch_fixture();
    let original = f.temp.path().join("existing-reservation");
    let mut runner = SystemRunner;
    run_git(
        &mut runner,
        &f.repo,
        &[
            "worktree",
            "add",
            original.to_str().unwrap(),
            "scheduler/todo-1-reserved",
        ],
    )
    .unwrap();
    fs::write(original.join("pending.txt"), "uncommitted work").unwrap();
    assert!(cc_scheduler::preparation::ensure_workspace(
        &mut runner,
        f.repo.to_str().unwrap(),
        &row,
        &[]
    )
    .is_err());
    assert!(!Path::new(s(&row["scheduler"]["worktree"])).exists());
    assert_eq!(
        fs::read_to_string(original.join("pending.txt")).unwrap(),
        "uncommitted work"
    );
}

impl Runner for GitFake {
    fn run(&mut self, c: &Invocation) -> Result<Output> {
        self.calls.push(c.clone());
        if c.args.iter().any(|arg| arg == "show-ref") {
            return Ok(Output {
                code: 2,
                stdout: String::new(),
                stderr: String::new(),
            });
        }
        let output = if let Some((_target, branch, git)) = &self.existing {
            match c.args.last().map(String::as_str) {
                Some("--show-toplevel") => c.args[1].clone(),
                Some("--show-current") => branch.clone(),
                _ => git.to_string_lossy().into_owned(),
            }
        } else if c.args.last().map(String::as_str) == Some("--show-toplevel") {
            c.args[1].clone()
        } else {
            "".into()
        };
        Ok(Output {
            code: 0,
            stdout: output,
            stderr: String::new(),
        })
    }
}
#[test]
fn test_worktree_creation_fetches_origin_main_and_reuses_branch() {
    let mut f = Fixture::new();
    f.put(item(1, "civ6", "todo", &[]));
    let row =
        f.e.prepare(&f.get(1), f.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    let target = PathBuf::from(s(&row["scheduler"]["worktree"]));
    fs::remove_dir(&target).unwrap();
    let mut git = GitFake {
        calls: vec![],
        existing: None,
    };
    ensure_worktree(&mut git, f.repo.to_str().unwrap(), &row).unwrap();
    assert_eq!(git.calls.len(), 6);
    assert_eq!(
        &git.calls[3].args[2..],
        ["fetch", "--quiet", "origin", "main"]
    );
    assert_eq!(
        &git.calls[4].args[2..],
        ["rev-parse", "--verify", "refs/remotes/origin/main"]
    );
    assert_eq!(git.calls[5].args[2], "worktree");
    assert_eq!(git.calls[5].args[3], "add");
    assert_eq!(git.calls[5].args[5], row["scheduler"]["branch"]);
    assert_eq!(git.calls[5].args.last().unwrap(), "origin/main");
    fs::create_dir(&target).unwrap();
    git.existing = Some((
        target,
        s(&row["scheduler"]["branch"]).into(),
        f.repo.join(".git"),
    ));
    git.calls.clear();
    ensure_worktree(&mut git, f.repo.to_str().unwrap(), &row).unwrap();
    assert_eq!(git.calls.len(), 5);
    assert!(git
        .calls
        .iter()
        .all(|c| !c.args.iter().any(|a| a == "fetch" || a == "worktree")));
}
#[test]
fn test_step_entrypoint_never_raises() {
    assert_eq!(
        step::<Fake>(Err(Error("RuntimeError"))),
        json!({"error":"RuntimeError"})
    );
}
#[test]
fn test_recover_mode_requeues_dead_job_without_launch_or_cancel() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.e.backend.ps.clear();
    write_json(
        &f.e.root.join("config.json"),
        &json!({"kill_enabled":true}),
        2,
    )
    .unwrap();
    f.e.run("recover", false).unwrap();
    assert_eq!(f.get(1)["status"], "todo");
    assert!(f.e.backend.launched.is_empty());
    assert!(f.e.backend.cancelled.is_empty());
    assert!(f.e.backend.notices.is_empty());
    assert!(f.e.backend.killed.is_empty());
}
#[test]
fn test_durable_memory_cancel_intent_survives_before_requeue() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.e.backend.records[0]["status"] = json!("cancelled");
    f.e.backend.ps.clear();
    f.e.edit(&json!(1), |r| {
        r["scheduler"]["pending_cancel"] = json!("memory")
    })
    .unwrap();
    f.reconcile();
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(n(&f.get(1)["scheduler"]["retry_count"]), 0.);
    assert_eq!(f.get(1)["scheduler"]["last_stop"], "memory");
}
#[test]
fn test_operator_cancel_is_not_automatically_undone() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.e.backend.records[0]["status"] = json!("cancelled");
    f.e.backend.ps.clear();
    f.reconcile();
    assert_eq!(f.get(1)["status"], "block");
    f.dispatch();
    assert!(f.e.backend.launched.is_empty());
}
#[test]
fn test_manual_lost_item_without_spec_requeues_for_its_agent() {
    let mut f = Fixture::new();
    let mut i = item(1, "civ6", "doing", &[]);
    i["jobs"] = json!(["j1"]);
    f.put(i);
    f.e.edit(&json!(1), |r| {
        remove(r, "spec");
        remove(r, "dispatchable");
    })
    .unwrap();
    f.e.backend.records.push(job("j1", 1, "running"));
    f.e.backend.ps.clear();
    f.reconcile();
    assert_eq!(f.get(1)["status"], "todo");
    f.dispatch();
    assert!(f.e.backend.launched.is_empty());
}
#[test]
fn test_orphan_apply_rechecks_pid_reuse_and_never_kills_replacement() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.e.backend.records[0]["status"] = json!("succeeded");
    f.e.backend.ps.insert(2, proc(2, 1, "node.exe"));
    let mut replacement = f.e.backend.ps[&2].clone();
    replacement["created"] = json!(replacement["created"].as_u64().unwrap() + 100);
    replacement["ppid"] = json!(0);
    f.e.backend.fresh = Some(ps(vec![f.e.backend.ps[&1].clone(), replacement]));
    let report =
        f.e.orphans(
            &f.e.backend.records.clone(),
            &f.e.backend.ps.clone(),
            &f.cfg,
            true,
        )
        .unwrap();
    assert_eq!(f.e.backend.killed, vec![json!(1)]);
    assert_eq!(report["killed"], json!([1]));
}
#[test]
fn test_todo_live_lock_is_not_stolen_by_old_timestamp() {
    let f = Fixture::new();
    let lockpath = f.e.todo_path.with_file_name("todo.lock");
    let lock = ByteLock::acquire(&lockpath, Duration::ZERO)
        .unwrap()
        .unwrap();
    assert!(ByteLock::acquire(&lockpath, Duration::ZERO)
        .unwrap()
        .is_none());
    drop(lock);
    assert!(ByteLock::acquire(&lockpath, Duration::ZERO)
        .unwrap()
        .is_some());
    assert!(lockpath.exists());
}
#[test]
fn test_owner_reply_requeues_through_capacity_and_preserves_resource_claim() {
    let mut f = Fixture::new();
    f.question();
    f.e.edit(&json!(1), |r| r["needs"] = json!(["civ6"]))
        .unwrap();
    f.e.backend.owner_reply = Some(json!({"id":"200","text":"a","stop":false}));
    f.e.answers().unwrap();
    f.e.answers().unwrap();
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(n(&f.get(1)["scheduler"]["retry_count"]), 0.);
    assert_eq!(f.e.backend.closed, vec![json!("mock-thread")]);
    f.e.backend.ps.insert(10, proc(10, 0, "CivilizationVI.exe"));
    f.dispatch();
    assert!(f.e.backend.launched.is_empty());
    f.e.backend.ps.clear();
    f.dispatch();
    assert_eq!(f.e.backend.launched.len(), 1);
    let text = s(&f.e.backend.launched[0]["text"]);
    assert!(text.contains("原因を調べて"));
    assert!(text.contains(RESUME_INSTRUCTION));
}
#[test]
fn test_owner_stop_reply_keeps_item_blocked() {
    let mut f = Fixture::new();
    f.question();
    f.e.backend.owner_reply = Some(json!({"id":"200","text":"b","stop":true}));
    f.e.answers().unwrap();
    f.dispatch();
    assert_eq!(f.get(1)["status"], "block");
    assert_eq!(f.e.backend.closed, vec![json!("mock-thread")]);
    assert!(f.e.backend.launched.is_empty());
}
#[test]
fn test_question_close_failure_retries_close_without_reprocessing_reply() {
    let mut f = Fixture::new();
    f.question();
    f.e.backend.owner_reply = Some(json!({"id":"200","text":"a","stop":false}));
    f.e.backend.close_fails = true;
    f.e.answers().unwrap();
    assert_eq!(f.get(1)["scheduler"]["answer"], "200");
    assert_eq!(f.get(1)["scheduler"]["question_close_pending"], true);
    f.put(item(2, "GoDD-AO", "todo", &[]));
    f.dispatch();
    assert_eq!(f.e.backend.launched[0]["item"]["id"], 2);
    f.e.backend.close_fails = false;
    let count = f.e.backend.reply_count;
    f.e.answers().unwrap();
    assert_eq!(f.e.backend.reply_count, count);
    assert!(f.get(1)["scheduler"]
        .get("question_close_pending")
        .is_none());
}
#[test]
fn test_new_hang_does_not_create_another_owner_question() {
    let mut f = Fixture::new();
    f.question();
    f.e.backend.owner_reply = Some(json!({"id":"200","text":"a","stop":false}));
    f.e.answers().unwrap();
    f.e.edit(&json!(1), |r| {
        r["status"] = json!("doing");
        merge(&mut r["scheduler"], json!({"job":"j2","retry_count":1}));
    })
    .unwrap();
    f.e.requeue(&json!(1), &json!("j2"), "hung").unwrap();
    assert_eq!(f.get(1)["status"], "todo");
    f.e.notifications().unwrap();
    assert_eq!(f.e.backend.notices, vec![json!(1)]);
    assert_eq!(f.get(1)["scheduler"]["answer"], "200");
}
#[derive(Clone, Default)]
struct AskFake {
    calls: std::sync::Arc<std::sync::Mutex<Vec<Value>>>,
}
impl cc_ask::core::Effects for AskFake {
    fn http(
        &mut self,
        m: &str,
        url: &str,
        body: Option<&Value>,
        _: bool,
    ) -> cc_ask::core::Result<Value> {
        self.calls
            .lock()
            .unwrap()
            .push(json!({"method":m,"url":url,"body":body}));
        if url.ends_with("/channels/FORUM") {
            return Ok(json!({"type":15}));
        }
        if m == "GET" && url.ends_with("/users/@me") {
            return Ok(json!({"id":"BOT","bot":true}));
        }
        if m == "GET"
            && (url.ends_with("/threads/active")
                || url.ends_with("/threads/archived/public?limit=25"))
        {
            return Ok(json!({"threads":[]}));
        }
        if m == "POST" {
            let count = self
                .calls
                .lock()
                .unwrap()
                .iter()
                .filter(|c| c["method"] == "POST")
                .count();
            let id = (299 + count).to_string();
            return Ok(json!({"id":id,"last_message_id":id}));
        }
        Err("RuntimeError: secret-shaped mock error".into())
    }
    fn run(&mut self, argv: &[String], _: u64) -> cc_ask::core::Result<cc_ask::core::Output> {
        self.calls.lock().unwrap().push(json!({"argv":argv}));
        Ok(cc_ask::core::Output {
            code: 0,
            stdout: "".into(),
            stderr: "".into(),
        })
    }
    fn now(&self) -> f64 {
        T
    }
}
fn ask_context(root: &Path, fake: AskFake) -> cc_ask::core::Context {
    // cc-ask no longer converts a missing ledger into a new empty store.
    cc_ask::core::atomic_json(&root.join("state.json"), &json!({"questions":[]}), 2).unwrap();
    fs::write(root.join("client-projects.txt"), "").unwrap();
    cc_ask::core::Context {
        client_projects: root.join("client-projects.txt"),
        civ6_home: root.join("civ6-live"),
        here: root.to_owned(),
        jobs: root.join("jobs"),
        discord_tasks: root.join("discord-tasks"),
        todo: root.join("todo.json"),
        cfg: json!({"owner_user_id":"OWNER","task_channel_id":"FORUM","guild_id":"GUILD"}),
        effects: Box::new(fake),
        codex_command: root.join("codex_jobs.exe"),
        scheduler_command: root.join("scheduler.exe"),
        shepherd_command: root.join("shepherd.exe"),
        todo_command: root.join("todo.exe"),
    }
}
#[test]
fn test_owner_question_records_original_worktree_for_continuation() {
    let temp = tempfile::tempdir().unwrap();
    let fake = AskFake::default();
    let calls = fake.calls.clone();
    let mut ctx = ask_context(temp.path(), fake);
    let body = temp.path().join("question.md");
    fs::write(&body, "停止した作業の再開方法を決めてください。").unwrap();
    let original = temp
        .path()
        .join("repo-wt/todo-1-work")
        .to_string_lossy()
        .into_owned();
    let args = cc_ask::commands::AskArgs {
        title: "作業が停止しました".into(),
        body_file: body,
        agent: "Scheduler".into(),
        cwd: Some(original.clone()),
        source: Some("scheduler".into()),
        scheduler_item: Some(1),
        scheduler_job: Some("stopped-job".into()),
        ..Default::default()
    };
    cc_ask::commands::cmd_ask(&mut ctx, &args).unwrap();
    let state = ctx.load().unwrap();
    let q = &state["questions"][0];
    assert_eq!(q["cwd"], original);
    assert_eq!(q["agent"], "Scheduler");
    assert_eq!(q["source"], "scheduler");
    assert_eq!(q["scheduler_item"], 1);
    assert_eq!(q["scheduler_job"], "stopped-job");
    assert_eq!(
        calls
            .lock()
            .unwrap()
            .iter()
            .find(|call| call["method"] == "POST")
            .unwrap()["body"]["message"]["allowed_mentions"],
        json!({"parse":[],"users":["OWNER"],"replied_user":false})
    );
    assert_eq!(q["thread_id"], "300");
    assert_eq!(q["progress_thread_id"], "301");
    let guard = calls.lock().unwrap();
    let posts: Vec<_> = guard.iter().filter(|c| c["method"] == "POST").collect();
    assert_eq!(posts.len(), 2);
    assert_eq!(posts[1]["body"]["message"]["flags"], cc_ask::SILENT | 4);
    assert_eq!(
        posts[1]["body"]["message"]["allowed_mentions"],
        json!({"parse":[],"users":[],"replied_user":false})
    );
}
#[test]
fn test_discord_generic_continuation_does_not_launch_scheduler_questions() {
    let temp = tempfile::tempdir().unwrap();
    let fake = AskFake::default();
    let calls = fake.calls.clone();
    let mut ctx = ask_context(temp.path(), fake);
    ctx.save(&json!({"questions":[{"source":"scheduler","open":true}]}))
        .unwrap();
    cc_ask::resume::cmd_resume(&mut ctx, &mut false).unwrap();
    // Answer receipts now inspect scheduler questions too; the generic path still must never launch.
    assert!(!calls
        .lock()
        .unwrap()
        .iter()
        .any(|call| call["argv"].is_array()));
}
#[test]
fn test_discord_tick_calls_scheduler_before_intake_even_if_intake_fails() {
    let temp = tempfile::tempdir().unwrap();
    let fake = AskFake::default();
    let calls = fake.calls.clone();
    let mut ctx = ask_context(temp.path(), fake);
    cc_ask::commands::cmd_tick(&mut ctx).unwrap();
    let calls = calls.lock().unwrap();
    let api = calls.iter().position(|v| truth(&v["url"])).unwrap();
    let scheduler = calls
        .iter()
        .position(|v| {
            arr(&v["argv"])
                .iter()
                .any(|v| s(v).contains("scheduler.exe"))
                && arr(&v["argv"]).last().is_some_and(|v| s(v) == "step")
        })
        .unwrap();
    assert!(scheduler < api);
    let launched: Vec<_> = calls
        .iter()
        .filter(|v| truth(&v["argv"]))
        .map(|v| {
            arr(&v["argv"])
                .iter()
                .map(|v| s(v).to_owned())
                .collect::<Vec<_>>()
        })
        .collect();
    assert_eq!(launched.len(), 7);
    assert!(launched[0].iter().any(|v| v == "recover-reboot"));
    assert_eq!(launched[0].last().unwrap(), "--apply");
    assert!(launched[1].iter().any(|v| v.ends_with("scheduler.exe")));
    assert_eq!(launched[1].last().unwrap(), "recover");
    assert!(launched[2].iter().any(|v| v.ends_with("todo.exe")));
    assert!(launched[3].iter().any(|v| v.ends_with("scheduler.exe")));
    assert_eq!(launched[3].last().unwrap(), "step");
    assert!(launched[4].iter().any(|v| v.ends_with("shepherd.exe")));
    assert_eq!(launched[5][0], "tasklist");
    assert_eq!(launched[6][0], "tasklist");
}
#[test]
fn test_hidden_wrapper_runs_rust_tick_with_window_style_zero() {
    let text = include_str!("../deployment/discord-ask-tick-hidden.vbs");
    let lines: Vec<_> = text
        .lines()
        .filter(|l| l.starts_with("objShell.Run"))
        .collect();
    assert_eq!(lines.len(), 1);
    assert!(lines[0].contains("discord_ask.exe"));
    assert!(lines[0].contains("tick"));
    // Recovery is now inside the measured Rust tick (covered by cc-ask/tick_bounds).
    assert!(!text.contains("scheduler.py"));
    assert!(lines.iter().all(|l| l.ends_with(", 0, True")));
}
struct Capture {
    calls: std::sync::Arc<std::sync::Mutex<Vec<Invocation>>>,
}
#[test]
fn real_backend_automatic_cancel_passes_the_recovery_flag_to_jobs_cli() {
    let f = Fixture::new();
    let calls = std::sync::Arc::new(std::sync::Mutex::new(vec![]));
    let mut backend = RealBackend::new(f.temp.path().join("scheduler"));
    backend.runner = Box::new(Capture {
        calls: calls.clone(),
    });
    let source = json!({"id":"source","status":"cancelled","pid":null,"codex_pid":null,
        "recovery_cancel":{"key":"persisted-cli-intent","committed_at":"2026-10-11T02:01:36+09:00"}});
    let dir = f.temp.path().join("codex-jobs/jobs/source");
    fs::create_dir_all(&dir).unwrap();
    write_json(&dir.join("job.json"), &source, 0).unwrap();
    let mut observed = json!({"id":"source","status":"running"});
    assert!(backend.cancel_for_recovery(&mut observed).unwrap());
    let calls = calls.lock().unwrap();
    assert_eq!(calls.len(), 1);
    assert!(calls[0]
        .args
        .ends_with(&["cancel".into(), "source".into(), "--recovery".into()]));
    assert_eq!(
        observed, source,
        "backend re-reads the CLI's durable job receipt"
    );
    assert!(!cc_job_policy::cancellation::blocks_recovery(
        &json!({}),
        &observed
    ));
}
impl Runner for Capture {
    fn run(&mut self, c: &Invocation) -> Result<Output> {
        self.calls.lock().unwrap().push(c.clone());
        Ok(Output {
            code: 0,
            stdout: "fixture-job\n".into(),
            stderr: String::new(),
        })
    }
}
#[test]
fn test_worker_launch_uses_windowless_existing_launcher() {
    let f = Fixture::new();
    let calls = std::sync::Arc::new(std::sync::Mutex::new(vec![]));
    let mut b = RealBackend::new(f.temp.path().join("scheduler"));
    b.runner = Box::new(Capture {
        calls: calls.clone(),
    });
    let mut row = item(1, "civ6", "todo", &[]);
    row["scheduler"] = json!({"key":"a".repeat(32),"worktree":"D:/fixture-wt/todo-1"});
    assert_eq!(
        b.launch(
            &row,
            Path::new("D:/fixture-prompt.md"),
            &json!({"model":"mock-model","effort":"high","task_type":"implementation"})
        )
        .unwrap(),
        "fixture-job"
    );
    let calls = calls.lock().unwrap();
    assert!(calls[0].args.iter().any(|v| v.ends_with("codex_jobs.py")));
    let args = &calls[0].args;
    assert!(args.iter().any(|v| v == "--auto-model"));
    assert!(!args.iter().any(|v| v == "--model"));
    assert_eq!(
        args[args.iter().position(|v| v == "--task-title").unwrap() + 1],
        row["title"]
    );
    assert_eq!(
        args[args.iter().position(|v| v == "--project").unwrap() + 1],
        "civ6"
    );
    assert_eq!(
        args[args.iter().position(|v| v == "--todo-id").unwrap() + 1],
        "1"
    );
    assert_eq!(
        args[args.iter().position(|v| v == "--cwd").unwrap() + 1],
        row["scheduler"]["worktree"]
    );
    assert_eq!(
        args[args.iter().position(|v| v == "--launch-key").unwrap() + 1],
        "a".repeat(32)
    );
    assert_eq!(NO_WINDOW, 0x08000000);
}
#[test]
fn test_unknown_explicit_model_is_not_launched() {
    let mut f = Fixture::new();
    assert!(!f.e.backend.model_exists("gpt-6.1-luna"));
    assert!(f.e.backend.model_exists("gpt-6-luna"));
    let mut row = item(1, "civ6", "todo", &[]);
    row["model"] = json!("gpt-6.1-luna");
    row["effort"] = json!("high");
    f.put(row);
    assert_eq!(
        f.e.dispatch(&f.cfg, &f.pri).unwrap_err().0,
        "ModelUnavailable"
    );
    assert!(f.e.backend.launched.is_empty());
    assert_eq!(f.e.backend.pick_count, 0);
    assert_eq!(f.get(1)["model"], "gpt-6.1-luna");
}

#[test]
fn explicitly_selected_todo_model_is_not_marked_automatic() {
    let f = Fixture::new();
    let b = RealBackend::new(f.temp.path().join("scheduler"));
    let mut row = item(1, "tools", "todo", &[]);
    row["model"] = json!("anthropic/claude-opus-5-5");
    row["scheduler"] = json!({"key":"a".repeat(32),"worktree":"D:/fixture-wt/todo-1"});
    let c = b.launch_command(
        &row,
        Path::new("D:/fixture-prompt.md"),
        &json!({"model":"anthropic/claude-opus-5-5","effort":"high","task_type":"implementation"}),
    );
    assert!(c.args.iter().any(|v| v == "--model"));
    assert!(!c.args.iter().any(|v| v == "--auto-model"));
}

struct HttpFake {
    responses: Vec<Result<Value>>,
    calls: usize,
}
impl Http for HttpFake {
    fn request(&mut self, _: &str, _: &str, _: Option<&Value>, _: bool, _: u64) -> Result<Value> {
        self.calls += 1;
        self.responses.remove(0)
    }
}
#[test]
fn models_cache_bare_provider_auto_expiry_and_unavailable() {
    let mut http = HttpFake {
        responses: vec![
            Ok(json!({"data":[{"id":"provider/gpt-6-luna"},{"id":"gpt-6.1-sol"}]})),
            Err(Error("OSError")),
        ],
        calls: 0,
    };
    let mut cache = ModelsCache::default();
    assert!(cache.exists(&mut http, "gpt-6-luna", T));
    assert!(cache.exists(&mut http, "provider/gpt-6-luna", T));
    assert!(cache.exists(&mut http, "auto", T));
    assert!(!cache.exists(&mut http, "gpt-6.1-luna", T + 599.));
    assert_eq!(http.calls, 1);
    assert!(cache.exists(&mut http, "anything", T + 601.));
    assert_eq!(http.calls, 2);
}
#[test]
fn corrupted_state_is_never_replaced() {
    let mut f = Fixture::new();
    fs::write(f.e.root.join("state.json"), "{broken").unwrap();
    assert_eq!(f.e.run("recover", false), Err(Error("JSONDecodeError")));
    assert_eq!(
        fs::read_to_string(f.e.root.join("state.json")).unwrap(),
        "{broken"
    );
}
#[test]
fn todo_edits_preserve_unknown_fields() {
    let mut f = Fixture::new();
    f.put(item(1, "civ6", "todo", &[]));
    let mut db = f.e.todo().unwrap();
    db["future_field"] = json!({"original":[1,"値",null]});
    db["items"][0]["future_item_field"] = json!([true, false]);
    write_json(&f.e.todo_path, &db, 1).unwrap();
    f.e.edit(&json!(1), |r| r["note"] = json!("更新")).unwrap();
    let updated = f.e.todo().unwrap();
    assert_eq!(updated["future_field"], db["future_field"]);
    assert_eq!(
        updated["items"][0]["future_item_field"],
        db["items"][0]["future_item_field"]
    );
}
#[test]
fn real_child_process_has_no_console_window() {
    let code = "Add-Type -TypeDefinition 'using System; using System.Runtime.InteropServices; public class Win { [DllImport(\"kernel32.dll\")] public static extern IntPtr GetConsoleWindow(); }'; [Win]::GetConsoleWindow().ToInt64()";
    let out = SystemRunner
        .run(&invocation(
            &[
                "powershell.exe",
                "-NoProfile",
                "-NonInteractive",
                "-Command",
                code,
            ],
            Some(30),
        ))
        .unwrap();
    assert_eq!(out.code, 0);
    assert_eq!(out.stdout.trim(), "0");
}
#[test]
fn executable_preferred_and_python_fallback() {
    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("tool.py");
    let first = tool_command(&path, &["step"], Some(45));
    assert!(first.program.ends_with("python.exe"));
    assert_eq!(first.args.last().unwrap(), "step");
    fs::write(path.with_extension("exe"), b"fixture").unwrap();
    let second = tool_command(&path, &["recover"], None);
    assert_eq!(second.program, path.with_extension("exe").to_str().unwrap());
    assert_eq!(second.args, ["recover"]);
}
#[test]
fn numeric_owner_id_accepts_latest_owner_reply_without_acknowledging() {
    let f = Fixture::new();
    let root = f.temp.path().join("discord-ask");
    fs::create_dir(&root).unwrap();
    write_json(&root.join("state.json"), &json!({"questions":[{"thread_id":"thread","after":"100","open":true,"source":"scheduler"}]}), 2).unwrap();
    let mut backend = RealBackend::new(f.temp.path().join("scheduler"));
    backend.config_path = f.temp.path().join("discord-config.json");
    write_json(&backend.config_path, &json!({"owner_user_id":123}), 2).unwrap();
    backend.http = Box::new(HttpFake {
        responses: vec![Ok(json!([
            {"id":"99","author":{"id":"123"},"content":"old"},
            {"id":"200","author":{"id":"123"},"content":"a"},
            {"id":"202","author":{"id":"other"},"content":"ignored"},
            {"id":"201","author":{"id":"123"},"content":"1 b. 停止したまま"}
        ]))],
        calls: 0,
    });
    assert_eq!(
        backend.reply(&json!({"thread_id":"thread"})).unwrap(),
        Some(json!({"id":"201","text":"1 b. 停止したまま","stop":true}))
    );
    assert_eq!(
        read_json(&root.join("state.json"), Value::Null).unwrap()["questions"][0]["after"],
        "100"
    );
}

#[test]
fn scheduler_postponement_is_saved_and_not_treated_as_a_restart_answer() {
    let f = Fixture::new();
    let root = f.temp.path().join("discord-ask");
    fs::create_dir(&root).unwrap();
    let original = json!({"questions":[{"thread_id":"thread","after":"100","open":true,"source":"scheduler","future":"keep"}],"other":"keep"});
    write_json(&root.join("state.json"), &original, 2).unwrap();
    let mut backend = RealBackend::new(f.temp.path().join("scheduler"));
    backend.config_path = f.temp.path().join("discord-config.json");
    write_json(&backend.config_path, &json!({"owner_user_id":123}), 2).unwrap();
    let sent = now();
    let id = (((sent * 1000.) as u64 - 1420070400000) << 22).to_string();
    let page = json!([
        {"id":"200","author":{"id":"123"},"content":"a"},
        {"id":id,"author":{"id":"123"},"content":"再通知は2時間後にして"}
    ]);
    backend.http = Box::new(HttpFake {
        responses: vec![Ok(page.clone()), Ok(page)],
        calls: 0,
    });
    assert_eq!(backend.reply(&json!({"thread_id":"thread"})).unwrap(), None);
    let saved = read_json(&root.join("state.json"), Value::Null).unwrap();
    assert_eq!(saved["questions"][0]["remind_reply"], id);
    assert!(saved["questions"][0]["remind_not_before"].as_f64().unwrap() > sent + 7199.);
    assert_eq!(saved["questions"][0]["open"], true);
    assert_eq!(saved["questions"][0]["after"], "100");
    assert_eq!(saved["questions"][0]["future"], "keep");
    assert_eq!(saved["other"], "keep");
    assert_eq!(backend.reply(&json!({"thread_id":"thread"})).unwrap(), None);
    assert_eq!(
        read_json(&root.join("state.json"), Value::Null).unwrap(),
        saved
    );
}
#[test]
fn junction_repo_resolves_to_original_worktree_container() {
    let mut f = Fixture::new();
    let alias = f.temp.path().join("repo-alias");
    let out = SystemRunner
        .run(&invocation(
            &[
                "cmd.exe",
                "/d",
                "/c",
                "mklink",
                "/J",
                alias.to_str().unwrap(),
                f.repo.to_str().unwrap(),
            ],
            Some(15),
        ))
        .unwrap();
    assert_eq!(out.code, 0);
    f.put(item(1, "civ6", "todo", &[]));
    let row =
        f.e.prepare(&f.get(1), alias.to_str().unwrap())
            .unwrap()
            .unwrap();
    let target = PathBuf::from(s(&row["scheduler"]["worktree"]));
    let repo = f.repo.canonicalize().unwrap();
    let repo = repo.to_string_lossy();
    assert_eq!(
        norm(target.parent().unwrap().to_str().unwrap()),
        norm(&format!("{}-wt", repo.trim_start_matches(r"\\?\")))
    );
    let mut git = GitFake {
        calls: vec![],
        existing: Some((
            target,
            s(&row["scheduler"]["branch"]).into(),
            f.repo.join(".git"),
        )),
    };
    fs::create_dir(f.repo.join(".git")).unwrap();
    ensure_worktree(&mut git, alias.to_str().unwrap(), &row).unwrap();
    assert!(git
        .calls
        .iter()
        .all(|c| !c.args.iter().any(|a| a == "worktree" || a == "fetch")));
}
#[test]
fn attempts_and_retries_remain_json_integers() {
    let mut f = Fixture::new();
    f.put(item(1, "civ6", "todo", &[]));
    let row =
        f.e.prepare(&f.get(1), f.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    assert_eq!(row["scheduler"]["attempt"].as_u64(), Some(1));
    f.e.edit(&json!(1), |row| {
        row["status"] = json!("doing");
        row["scheduler"]["job"] = json!("j1");
        row["scheduler"]["retry_count"] = json!(0);
    })
    .unwrap();
    f.e.requeue(&json!(1), &json!("j1"), "lost").unwrap();
    assert_eq!(f.get(1)["scheduler"]["retry_count"].as_u64(), Some(1));
    f.e.backend.clock += 61.;
    let row =
        f.e.prepare(&f.get(1), f.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    assert_eq!(row["scheduler"]["attempt"].as_u64(), Some(2));
    assert_eq!(add_count(&json!(0.5), 1), json!(1.5));
}
#[test]
fn help_uses_stdout_and_exit_zero_for_every_subcommand() {
    for args in [
        &["--help"][..],
        &["status", "--help"],
        &["step", "-h"],
        &["recover", "-h"],
        &["orphans", "-h"],
        &["checkpoint-workspace", "--help"],
    ] {
        let mut c = invocation(&[env!("CARGO_BIN_EXE_scheduler")], Some(10));
        c.args = args.iter().map(|a| a.to_string()).collect();
        let out = SystemRunner.run(&c).unwrap();
        assert_eq!(out.code, 0);
        assert!(out.stdout.starts_with("usage:"));
        assert!(out.stdout.contains("--help"));
    }
    let out = SystemRunner
        .run(&invocation(&[env!("CARGO_BIN_EXE_scheduler")], Some(10)))
        .unwrap();
    assert_eq!(out.code, 2);
    assert!(out.stdout.is_empty());
    assert!(!out.stderr.is_empty());
}
#[test]
fn scheduler_lock_child() {
    let Some(path) = std::env::var_os("CC_SCHED_LOCK_PROBE") else {
        return;
    };
    let _guard = ByteLock::acquire(Path::new(&path), Duration::ZERO)
        .unwrap()
        .unwrap();
    fs::write(std::env::var_os("CC_SCHED_LOCK_READY").unwrap(), "ready").unwrap();
    let mut line = String::new();
    std::io::stdin().read_line(&mut line).unwrap();
}
#[test]
fn rust_byte_lock_interoperates_with_other_process_and_releases() {
    use std::{
        io::Write,
        os::windows::process::CommandExt,
        process::{Command, Stdio},
    };
    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("shared.lock");
    let ready = temp.path().join("ready");
    let mut child = Command::new(std::env::current_exe().unwrap())
        .args(["--exact", "scheduler_lock_child", "--nocapture"])
        .env("CC_SCHED_LOCK_PROBE", &path)
        .env("CC_SCHED_LOCK_READY", &ready)
        .creation_flags(NO_WINDOW)
        .stdin(Stdio::piped())
        .stdout(Stdio::null())
        .spawn()
        .unwrap();
    let deadline = std::time::Instant::now() + Duration::from_secs(5);
    while !ready.exists() {
        assert!(std::time::Instant::now() < deadline);
        std::thread::sleep(Duration::from_millis(10));
    }
    assert!(ByteLock::acquire(&path, Duration::ZERO).unwrap().is_none());
    child.stdin.take().unwrap().write_all(b"done\n").unwrap();
    assert!(child.wait().unwrap().success());
    assert!(ByteLock::acquire(&path, Duration::ZERO).unwrap().is_some());
}
#[test]
fn router_failure_preserves_review_fallback_model() {
    let mut http = HttpFake {
        responses: vec![Err(Error("OSError"))],
        calls: 0,
    };
    let picked = pick_model(&mut http, "レビューしてください").unwrap();
    assert_eq!(picked["model"], "gpt-6-luna");
    assert_eq!(picked["task_type"], "review");
    assert_eq!(picked["effort"], "high");
    assert_eq!(http.calls, 1);
}

#[test]
fn review_word_in_implementation_prompt_does_not_select_light_profile() {
    let mut http = HttpFake {
        responses: vec![Err(Error("OSError"))],
        calls: 0,
    };
    let picked = pick_model(&mut http, "実装して、画面にレビューという文字を表示する").unwrap();
    assert_eq!(picked["model"], "gpt-6.1-sol");
    assert_eq!(picked["task_type"], "implementation");
    assert_eq!(picked["effort"], "high");
}
#[test]
fn invalid_settings_shape_returns_error_and_never_rewrites_state() {
    let mut f = Fixture::new();
    write_json(&f.e.root.join("config.json"), &json!([]), 2).unwrap();
    assert_eq!(f.e.run("recover", false), Err(Error("TypeError")));
    assert!(!f.e.root.join("state.json").exists());
}

fn wait_row(f: &mut Fixture, id: u64, on: Value) {
    f.e.edit(&json!(id), |row| {
        row["status"] = json!("wait");
        row["wait_on"] = on;
    })
    .unwrap();
}
#[test]
fn retry_deadline_wait_does_not_dispatch_until_watchdog_releases_it() {
    let mut f = Fixture::new();
    f.put(item(1, "civ6", "todo", &[]));
    let mut db = f.e.todo().unwrap();
    cc_todo::ready::register_retry(&mut db, 1, 3600, "操作環境の接続不足", 100).unwrap();
    write_json(&f.e.todo_path, &db, 2).unwrap();
    f.dispatch();
    assert!(f.e.backend.launched.is_empty());
    assert!(f.e.report["waiting"][0]["message"]
        .as_str()
        .unwrap()
        .contains("次の再確認"));
    assert!(cc_todo::ready::release_retries(&mut db, 3699)
        .unwrap()
        .is_empty());
    assert_eq!(
        cc_todo::ready::release_retries(&mut db, 3700).unwrap(),
        vec![1]
    );
    write_json(&f.e.todo_path, &db, 2).unwrap();
    f.dispatch();
    assert_eq!(f.e.backend.launched.len(), 1);
    assert_eq!(f.e.backend.launched[0]["item"]["id"], 1);
}
#[test]
fn readiness_wait_parks_only_its_task_and_fresh_check_releases_same_task() {
    let mut f = Fixture::new();
    f.put(item(1, "civ6", "wait", &[]));
    f.put(item(2, "civ6", "todo", &[]));
    f.e.edit(&json!(1), |row| {
        row["wait_ready"] = json!({"condition":"chatgpt-plus","since":100})
    })
    .unwrap();
    f.dispatch();
    assert_eq!(f.e.backend.launched.len(), 1);
    assert_eq!(f.e.backend.launched[0]["item"]["id"], 2);
    assert_eq!(
        f.e.report["waiting"][0]["message"],
        "ChatGPT Plusの復旧確認待ち"
    );
    let mut db = f.e.todo().unwrap();
    let state = json!({"backend":"owner_chrome","login":"logged_in","checked_at":100});
    assert!(cc_todo::ready::resume(&mut db, 1, "chatgpt-plus", &state, 100).unwrap());
    write_json(&f.e.todo_path, &db, 2).unwrap();
    // Normal admission still honours the launch interval after another task started.
    f.e.backend.clock += n(&f.cfg["launch_interval_s"]);
    f.dispatch();
    assert!(f
        .e
        .backend
        .launched
        .iter()
        .any(|launch| launch["item"]["id"] == 1));
}
#[test]
fn waiting_item_is_never_dispatched_and_launches_after_target_done() {
    let mut f = Fixture::new();
    f.put(item(1, "civ6", "todo", &[]));
    f.put(item(2, "civ6", "todo", &[]));
    // Waiting is honoured whatever the status says (a writer may only set wait_on).
    f.e.edit(&json!(1), |row| row["wait_on"] = json!([2]))
        .unwrap();
    f.dispatch();
    assert_eq!(f.e.backend.launched[0]["item"]["id"], 2);
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(f.e.report["waiting"][0]["message"], "ToDo 2 待ち");
    assert_eq!(f.e.report["waiting"][0]["item"], 1);
    // Missing target keeps it waiting.
    let mut g = Fixture::new();
    g.put(item(1, "civ6", "todo", &[]));
    g.e.edit(&json!(1), |row| row["wait_on"] = json!([99]))
        .unwrap();
    g.dispatch();
    assert!(g.e.backend.launched.is_empty());
    // Target done: released item (as the shared helper leaves it) dispatches and drops the ready marker.
    g.e.edit(&json!(1), |row| {
        row.as_object_mut().unwrap().remove("wait_on");
        row["dependency_ready"] = json!(true);
    })
    .unwrap();
    g.dispatch();
    assert_eq!(g.e.backend.launched[0]["item"]["id"], 1);
    assert!(g.get(1).get("dependency_ready").is_none());
}
#[test]
fn wait_recorded_while_job_completes_is_not_overwritten() {
    for result in ["状態: 完了", "状態: 要判断\n質問"] {
        // Wait command already stored status=wait: the finished job must not touch the item.
        let mut f = Fixture::new();
        f.managed(1, 1, "civ6", 0);
        f.put(item(2, "civ6", "todo", &[]));
        f.e.backend.records[0]["status"] = json!("succeeded");
        f.e.backend.records[0]["last_message"] = json!(result);
        f.e.backend.ps.clear();
        wait_row(&mut f, 1, json!([2]));
        let before = f.get(1);
        f.reconcile();
        assert_eq!(f.get(1)["status"], "wait");
        assert_eq!(f.get(1)["wait_on"], json!([2]));
        assert_eq!(f.get(1)["note"], before["note"]);
        // Only wait_on was written (status still doing): parked, not done/block, note kept.
        let mut g = Fixture::new();
        g.managed(1, 1, "civ6", 0);
        g.put(item(2, "civ6", "todo", &[]));
        g.e.backend.records[0]["status"] = json!("succeeded");
        g.e.backend.records[0]["last_message"] = json!(result);
        g.e.backend.ps.clear();
        g.e.edit(&json!(1), |row| {
            row["wait_on"] = json!([2]);
            row["note"] = json!("既存のメモ");
        })
        .unwrap();
        g.reconcile();
        let row = g.get(1);
        assert_eq!(row["status"], "wait");
        assert_eq!(row["note"], "既存のメモ");
        assert!(row["scheduler"]["job"].is_null());
        assert_eq!(row["scheduler"]["last_job"], "j1");
    }
}
#[test]
fn recovery_requeue_keeps_wait_and_owner_block_is_not_converted() {
    let mut f = Fixture::new();
    f.managed(1, 1, "civ6", 0);
    f.put(item(2, "civ6", "todo", &[]));
    f.e.backend.records[0]["status"] = json!("lost");
    f.e.backend.ps.clear();
    f.e.edit(&json!(1), |row| row["wait_on"] = json!([2]))
        .unwrap();
    f.reconcile();
    assert_eq!(f.get(1)["status"], "wait");
    // A generic wait_on on an owner-blocked item does not turn it into wait.
    let mut g = Fixture::new();
    let mut blocked = item(1, "civ6", "block", &[]);
    blocked["wait_on"] = json!([2]);
    g.put(blocked);
    g.put(item(2, "civ6", "todo", &[]));
    g.reconcile();
    assert_eq!(g.get(1)["status"], "block");
}
#[test]
fn capacity_cooldown_preserves_the_existing_job_and_terminal_budget_is_not_reset() {
    for exhausted in [false, true] {
        let mut f = Fixture::new();
        f.managed(1, 1, "other", 0);
        f.e.backend.records[0]["capacity_wait_pending"] = json!(!exhausted);
        f.e.backend.records[0]["capacity_exhausted"] = json!(exhausted);
        f.e.backend.records[0]["last_error"] =
            json!("429 Too Many Requests: Selected model is at capacity");
        f.e.backend.records[0]["status"] = json!(if exhausted { "failed" } else { "queued" });
        f.e.backend.records[0]["retry_not_before"] = json!(T + 900.);
        f.reconcile();
        f.reconcile();
        assert_eq!(f.get(1)["status"], "doing");
        assert_eq!(f.get(1)["scheduler"]["job"], "j1");
        assert!(f.e.backend.cancelled.is_empty());
        assert!(f.e.backend.launched.is_empty());
        assert!(f.get(1)["scheduler"]["notice_pending"].is_null());
        if exhausted {
            assert_eq!(f.get(1)["scheduler"]["phase"], "capacity_exhausted");
        }
    }
}
