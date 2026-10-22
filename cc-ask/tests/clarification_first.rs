//! Public bridge acceptance with synthetic input and a substituted runner.
//! These tests do not prove Discord identity or delivery; sender tests cover that.
use cc_ask::{
    conversation_bridge as bridge,
    core::{Effects, Output, Result},
};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{collections::BTreeMap, fs, path::PathBuf};

// The Go integration test starts this exact Rust test executable. Effects::run
// delegates to the Go test process, which calls real conversationLogInner and
// substitutes only the Discord sender. Production endpoints are never changed.
#[test]
fn real_go_state_bridge_fixture() {
    let Some(root) = std::env::var_os("CLARIFICATION_BRIDGE_FIXTURE_HOME") else {
        return;
    };
    let root = PathBuf::from(root);
    let go_test = std::env::var_os("CLARIFICATION_GO_TEST_EXE").unwrap();
    fs::write(
        root.join("conversation-mode.json"),
        r#"{"domain":"discord-conversation-mode-v1","enabled":true}"#,
    )
    .unwrap();
    let body = root.join("source.txt");
    fs::write(&body, "固定の説明です。\r\n同じ本文を保持します。").unwrap();
    let proof = root.join("proof.json");
    fs::write(&proof, r#"{"fixture":"cross-layer-proof"}"#).unwrap();
    struct GoState(std::ffi::OsString);
    impl Effects for GoState {
        fn http(&mut self, _: &str, _: &str, _: Option<&Value>, _: bool) -> Result<Value> {
            Err("no HTTP in cross-layer fixture".into())
        }
        fn run(&mut self, args: &[String], _: u64) -> Result<Output> {
            let mut cmd = std::process::Command::new(&self.0);
            cmd.arg("-test.run=^TestClarificationGoStateHelper$")
                .env("CLARIFICATION_GO_STATE_HELPER", "1")
                .env(
                    "CLARIFICATION_CALL_ARGS",
                    serde_json::to_string(args).unwrap(),
                );
            #[cfg(windows)]
            {
                use std::os::windows::process::CommandExt;
                cmd.creation_flags(0x08000000);
            }
            let out = cmd
                .output()
                .map_err(|_| "Go test helper did not complete")?;
            let text =
                String::from_utf8(out.stdout).map_err(|_| "Go helper output is not UTF-8")?;
            let code = text
                .lines()
                .find_map(|line| line.strip_prefix("CC_CODE="))
                .and_then(|v| v.parse().ok())
                .ok_or("Go helper code missing")?;
            let stdout = text
                .lines()
                .find_map(|line| line.strip_prefix("CC_REPORT="))
                .ok_or("Go helper result missing")?
                .to_owned();
            Ok(Output {
                code,
                stdout,
                stderr: String::new(),
            })
        }
    }
    let mut runner = GoState(go_test);
    if std::env::var_os("CLARIFICATION_SEED_FAILED_LOG").is_some() {
        assert!(bridge::try_post(&root, &mut runner, "610", &body, false, &[]).is_err());
    }
    for _ in 0..2 {
        let result =
            bridge::try_post_clarification(&root, &mut runner, "610", &body, false, &[], &proof)
                .unwrap()
                .unwrap();
        assert_eq!(result["delivery"], "verified");
        assert_eq!(result["message_id"], "620");
    }
}

fn sha(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}

fn arg<'a>(args: &'a [String], key: &str) -> &'a str {
    args.windows(2)
        .find(|pair| pair[0] == key)
        .map(|pair| pair[1].as_str())
        .unwrap_or("")
}

struct Fixture {
    root: tempfile::TempDir,
    body: PathBuf,
    binding: PathBuf,
    normal_id: String,
}

impl Fixture {
    fn new() -> Self {
        let root = tempfile::tempdir().unwrap();
        fs::write(
            root.path().join("conversation-mode.json"),
            r#"{"domain":"discord-conversation-mode-v1","enabled":true}"#,
        )
        .unwrap();
        let body = root.path().join("source.txt");
        let normalized = "料金の内訳を補足します。\n必要性は利用実態で確認します。";
        fs::write(&body, normalized.replace('\n', "\r\n")).unwrap();
        let identity = json!([
            "normal-log-v1",
            "既存の作業への報告",
            sha(normalized.as_bytes()),
            "600",
            false,
            []
        ]);
        let normal_id = format!("normal-log-{}", sha(identity.to_string().as_bytes()));
        let binding = root.path().join("binding.json");
        let proof = json!({
            "domain":"discord-question-clarification-v1",
            "purpose":"explain_original_question",
            "request_id":format!("conversation-log-{}", &sha(normal_id.as_bytes())[..32]),
            "question_request_id":"fixture-original-question",
            "todo_id":9042,"thread_id":"600","question_message_id":"600",
            "question_fingerprint":"0".repeat(64),"question_body_sha256":"1".repeat(64),
            "bot_id":"900","owner_id":"800","reply_message_id":"602",
            "reply_body_sha256":"2".repeat(64),
            "reply_timestamp":"2026-10-12T00:00:01+00:00","reply_edited_timestamp":null,
            "body_sha256":sha(normalized.as_bytes())
        });
        fs::write(&binding, proof.to_string()).unwrap();
        Self {
            root,
            body,
            binding,
            normal_id,
        }
    }

    fn snapshot(&self) -> BTreeMap<PathBuf, String> {
        fn walk(
            root: &std::path::Path,
            at: &std::path::Path,
            files: &mut BTreeMap<PathBuf, String>,
        ) {
            for entry in fs::read_dir(at).unwrap() {
                let path = entry.unwrap().path();
                if path.is_dir() {
                    files.insert(
                        path.strip_prefix(root).unwrap().to_owned(),
                        "directory".into(),
                    );
                    walk(root, &path, files);
                } else {
                    files.insert(
                        path.strip_prefix(root).unwrap().to_owned(),
                        sha(&fs::read(&path).unwrap()),
                    );
                }
            }
        }
        let mut files = BTreeMap::new();
        walk(self.root.path(), self.root.path(), &mut files);
        files
    }

    fn runner(&self, reject: bool) -> Runner {
        Runner {
            storage: self.root.path().join("conversation-bridge"),
            reject,
            checks: vec![],
            sends: vec![],
        }
    }
}

struct Runner {
    storage: PathBuf,
    reject: bool,
    checks: Vec<(Vec<String>, bool)>,
    sends: Vec<Vec<String>>,
}

impl Effects for Runner {
    fn http(&mut self, _: &str, _: &str, _: Option<&Value>, _: bool) -> Result<Value> {
        Err("the bridge must delegate authenticated inspection to its runner".into())
    }

    fn run(&mut self, args: &[String], _: u64) -> Result<Output> {
        let checking = args.iter().any(|value| value == "--check-only");
        if checking {
            self.checks.push((args.to_vec(), self.storage.exists()));
        } else {
            self.sends.push(args.to_vec());
        }
        // A real sender independently checks question/owner/current reply. Here
        // that boundary only returns a prescribed acceptance or rejection.
        let accepted = if checking {
            !self.reject
        } else {
            !self.reject && !self.checks.is_empty()
        };
        let report = if checking {
            json!({"domain":"discord-conversation-v1","request_id":arg(args,"--request-id"),
                "check_only":true,"delivery":"not_sent","original_intent_retained":true,
                "body_sha256":sha(String::from_utf8(fs::read(arg(args,"--body-file")).unwrap()).unwrap().trim_start_matches('\u{feff}').replace("\r\n","\n").replace('\r',"\n").as_bytes()),
                "binding_sha256":sha(&fs::read(arg(args,"--clarification-file")).unwrap())})
        } else {
            json!({"domain":"discord-conversation-v1","request_id":arg(args,"--request-id"),
                "delivery":"verified","thread_id":"600","message_id":"604","reused":false})
        };
        Ok(Output {
            code: if accepted { 0 } else { 1 },
            stdout: report.to_string(),
            stderr: String::new(),
        })
    }
}

#[test]
fn first_check_accepts_source_without_creating_any_snapshot() {
    let f = Fixture::new();
    let before = f.snapshot();
    let mut runner = f.runner(false);
    let result = bridge::try_check_clarification(
        f.root.path(),
        &mut runner,
        "600",
        &f.body,
        false,
        &[],
        &f.binding,
    );
    assert!(
        result.is_ok(),
        "unreserved explanation must reach GET-only validation: {result:?}"
    );
    let report = result.unwrap().unwrap();
    assert_eq!(report["delivery"], "not_sent");
    assert_eq!(report["check_only"], true);
    assert_eq!(report["request_id"], f.normal_id);
    assert_eq!(f.snapshot(), before);
    assert_eq!(runner.checks.len(), 1);
    assert!(
        !runner.checks[0].1,
        "validation observed a prematurely saved original"
    );
    assert!(runner.sends.is_empty());
}

#[test]
fn first_send_validates_before_saving_original_and_keeps_normal_identity() {
    let f = Fixture::new();
    let mut runner = f.runner(false);
    let result = bridge::try_post_clarification(
        f.root.path(),
        &mut runner,
        "600",
        &f.body,
        false,
        &[],
        &f.binding,
    );
    assert!(
        result.is_ok(),
        "first send must validate before immutable input is saved: {result:?}"
    );
    assert_eq!(result.unwrap().unwrap()["delivery"], "verified");
    assert_eq!(runner.checks.len(), 1);
    assert!(!runner.checks[0].1);
    assert_eq!(runner.sends.len(), 1);
    let sent = &runner.sends[0];
    assert_eq!(arg(sent, "--request-id"), f.normal_id);
    assert_eq!(arg(sent, "--thread"), "600");
    assert_eq!(
        fs::read(arg(sent, "--body-file")).unwrap(),
        "料金の内訳を補足します。\n必要性は利用実態で確認します。".as_bytes()
    );
    assert_eq!(
        fs::read(arg(sent, "--clarification-file")).unwrap(),
        fs::read(&f.binding).unwrap()
    );
    assert!(sent
        .iter()
        .all(|value| value != "--notify" && value != "--attach" && value != "--check-only"));
}

#[test]
fn crlf_retry_preflight_uses_the_same_saved_bytes_as_the_first_send() {
    let f = Fixture::new();
    let mut runner = f.runner(false);
    bridge::try_post_clarification(
        f.root.path(),
        &mut runner,
        "600",
        &f.body,
        false,
        &[],
        &f.binding,
    )
    .unwrap();
    let first_input = fs::read(arg(&runner.sends[0], "--body-file")).unwrap();
    let again = bridge::try_post_clarification(
        f.root.path(),
        &mut runner,
        "600",
        &f.body,
        false,
        &[],
        &f.binding,
    );
    assert!(again.is_ok());
    assert_eq!(
        fs::read(arg(&runner.checks[1].0, "--body-file")).unwrap(),
        first_input
    );
}

#[test]
fn rejected_first_send_preserves_every_original_file_and_creates_no_reservation() {
    let f = Fixture::new();
    let before = f.snapshot();
    let mut runner = f.runner(true);
    let result = bridge::try_post_clarification(
        f.root.path(),
        &mut runner,
        "600",
        &f.body,
        false,
        &[],
        &f.binding,
    );
    assert!(result.is_err());
    assert_eq!(
        f.snapshot(),
        before,
        "rejected identity must not leave original input or binding snapshots"
    );
    assert_eq!(
        runner.checks.len(),
        1,
        "rejection must come from validation of the original request"
    );
    assert!(runner.sends.is_empty());
}
