mod common;
use common::Harness;
use serde_json::{Value, json};

#[test]
fn missing_job_recovery_restores_saved_field_lifetimes_and_cli_arguments() {
    for stored in [false, true] {
        for (sandbox, mcp, mask) in [
            (false, false, "none"),
            (true, false, "sandbox"),
            (false, true, "mcp_profile"),
            (true, true, "both"),
        ] {
            let mut h = Harness::new();
            h.tick();
            let mut state = h.state();
            let mut selection = json!({"id":"job1","model":"gpt-6.1-sol","effort":"max",
                "task_type":"implementation","sandbox":"read-only","mcp_profile":"minimal",
                "scope_source":"caller_constraint","capability_provenance_version":1,
                "explicit_capability_constraints":{"sandbox":sandbox,"mcp_profile":mcp}});
            let cwd = state["requests"]["100"]["cwd"].clone();
            selection["cwd"] = cwd.clone();
            state["requests"]["100"]["selection"] = selection.clone();
            state["requests"]["100"]["job_info"] = if stored { selection } else { Value::Null };
            h.save(&state);
            std::fs::remove_file(h.ctx.jobs.join("jobs/job1/job.json")).unwrap();
            h.tick();
            let fake = h.fake.borrow();
            assert_eq!(fake.launches.len(), 2, "stored={stored}, mask={mask}");
            let args = &fake
                .runs
                .iter()
                .rev()
                .find(|(args, _)| {
                    args.iter().any(|arg| arg == "run")
                        && args.iter().any(|arg| arg == "--mcp-profile")
                })
                .unwrap()
                .0;
            for (flag, expected) in [
                ("--sandbox", if sandbox { "read-only" } else { "inherit" }),
                ("--mcp-profile", if mcp { "minimal" } else { "inherit" }),
                ("--capability-constraints", mask),
            ] {
                assert!(
                    args.windows(2).any(|pair| pair == [flag, expected]),
                    "stored={stored}, mask={mask}, flag={flag}, args={args:?}"
                );
            }
            assert_eq!(h.state()["requests"]["100"]["job"], "job2");
        }
    }
}

#[test]
fn owner_steering_completes_a_partial_source_without_promoting_its_temporary_field() {
    use cc_ask::{core::*, restart::Slot, steering};
    let mut h = Harness::new();
    h.tick();
    let file = h.ctx.jobs.join("jobs/job1/job.json");
    let mut job = read_json(&file).unwrap();
    job["status"] = json!("running");
    job["sandbox"] = json!("read-only");
    job["mcp_profile"] = Value::Null;
    job["scope_source"] = json!("launcher");
    job["capability_provenance_version"] = json!(1);
    job["explicit_capability_constraints"] = json!({"sandbox":false,"mcp_profile":false});
    atomic_json(&file, &job, 2).unwrap();
    let mut state = h.state();
    state["requests"]["100"]["selection"] = json!({"model":"gpt-6.1-sol","effort":"max",
        "sandbox":"read-only","mcp_profile":"minimal","scope_source":"caller_constraint",
        "capability_provenance_version":1,"explicit_capability_constraints":{"sandbox":false,"mcp_profile":true}});
    h.save(&state);
    h.reply("同じ仕事を続ける", "100");
    let mut record = state["requests"]["100"].clone();
    let thread = h.fake.borrow().threads["100"].clone();
    assert!(
        steering::deliver(
            &mut h.ctx,
            &mut state,
            &thread,
            &mut record,
            Slot::Request,
            "OWN",
            &mut false
        )
        .unwrap()
    );
    let fake = h.fake.borrow();
    assert_eq!(fake.launches.len(), 2);
    let args = &fake
        .runs
        .iter()
        .rev()
        .find(|(args, _)| args.iter().any(|arg| arg == "--mcp-profile"))
        .unwrap()
        .0;
    for (flag, value) in [
        ("--sandbox", "inherit"),
        ("--mcp-profile", "minimal"),
        ("--capability-constraints", "mcp_profile"),
    ] {
        assert!(
            args.windows(2).any(|pair| pair == [flag, value]),
            "{flag}: {args:?}"
        );
    }
}
