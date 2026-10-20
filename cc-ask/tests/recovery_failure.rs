use cc_ask::recovery_failure::*;
use serde_json::json;

#[test]
fn launcher_model_context_proxy_and_task_errors_are_classified() {
    for (message, expected) in [
        (
            "failed to initialize in-process app-server client: Access is denied. (os error 5)",
            Failure::Console,
        ),
        ("app-server client disconnected", Failure::Console),
        (
            "unexpected status 502 Bad Gateway from proxy",
            Failure::Proxy,
        ),
        ("HTTP 502", Failure::Proxy),
        ("model unavailable", Failure::Model),
        (
            "The requested model 'old-model' does not exist",
            Failure::Model,
        ),
        ("model_not_found", Failure::Model),
        ("context window exceeded", Failure::Context),
        ("maximum context length exceeded", Failure::Context),
        (
            "thread-store conflict: thread session already has an active writer",
            Failure::Writer,
        ),
        ("assertion failed: test invoice total", Failure::Task),
        ("exit code 1", Failure::Task),
    ] {
        assert_eq!(classify(message), expected, "{message}");
    }
}
#[test]
fn stderr_tail_provides_concrete_cause_over_generic_exit_code() {
    let dir = tempfile::tempdir().unwrap();
    std::fs::write(
        dir.path().join("stderr.log"),
        format!(
            "{}\nfailed to initialize in-process app-server client: Access is denied",
            "warning\n".repeat(10000)
        ),
    )
    .unwrap();
    let text = evidence(
        &json!({"last_error":"Codex exited with code 1"}),
        dir.path(),
    );
    assert!(text.len() < 16500);
    assert_eq!(classify(&text), Failure::Console);
    assert!(summary(&text).contains("Access is denied"));
}
#[test]
fn remedies_preserve_work_and_permissions_but_replace_full_or_bad_model_sessions() {
    let job = json!({"model":"gpt-6.1-sol","effort":"max","thread_id":"full","resume_session":"full","cwd":"C:/repo","sandbox":"workspace-write","add_dirs":["D:/CC"]});
    for class in [Failure::Context, Failure::Model, Failure::Writer] {
        let repaired = repaired_job(&job, class);
        assert!(repaired["thread_id"].is_null());
        assert!(repaired["resume_session"].is_null());
        for field in ["cwd", "sandbox", "add_dirs"] {
            assert_eq!(repaired[field], job[field]);
        }
        if class == Failure::Model {
            assert_eq!(repaired["model"], "gpt-6-astra");
            assert_eq!(repaired["effort"], "max");
        } else {
            assert_eq!(repaired["model"], job["model"]);
        }
    }
    assert_eq!(repaired_job(&job, Failure::Task), job);
    assert_eq!(repaired_job(&job, Failure::Console), job);
}
#[test]
fn two_quick_retries_become_hourly_and_console_cooldown_survives_process_restart() {
    assert_eq!(retry_at(&[], 10000.), 0.);
    assert_eq!(retry_at(&[json!(10000.)], 10000.), 10060.);
    assert_eq!(retry_at(&[json!(10000.), json!(10060.)], 10060.), 13660.);
    let dir = tempfile::tempdir().unwrap();
    assert!(reserve_console(dir.path(), 10000.).unwrap());
    assert!(!reserve_console(dir.path(), 10001.).unwrap());
    assert!(!reserve_console(dir.path(), 13599.).unwrap());
    assert!(reserve_console(dir.path(), 13600.).unwrap());
}
