//! Runs gh_gate.exe against a recording stand-in for gh.exe and asserts what
//! the child actually received. Secrets are synthetic and built at run time.
use cc_redact::MARK;
use serde_json::Value;
use std::{
    fs,
    io::Write,
    path::{Path, PathBuf},
    process::{Output, Stdio},
};

fn token() -> String {
    format!("{}{}", "gh", "p_") + &"Ab3De5Gh7J".repeat(4)
}
fn service_secret() -> String {
    format!("serviceSecret={}", "sv7Hq2Xw".repeat(3))
}

struct Env {
    dir: PathBuf,
    real: PathBuf,
}
impl Env {
    fn new(name: &str) -> Self {
        let dir =
            std::env::temp_dir().join(format!("cc-gh-gate-test-{name}-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(dir.join("real")).unwrap();
        let real = dir.join("real").join("gh.exe");
        fs::copy(env!("CARGO_BIN_EXE_gh_gate_fixture"), &real).unwrap();
        Self { dir, real }
    }
    fn record_path(&self) -> PathBuf {
        self.dir.join("record.json")
    }
    fn run(&self, args: &[&str], stdin: &[u8], env: &[(&str, &str)]) -> (Output, Option<Value>) {
        let _ = fs::remove_file(self.record_path());
        let mut c = cc_gh_gate::hidden(env!("CARGO_BIN_EXE_gh_gate"));
        c.args(args)
            .current_dir(&self.dir)
            .env(cc_gh_gate::REAL_ENV, &self.real)
            .env("FAKE_GH_RECORD", self.record_path())
            .env_remove(cc_gh_gate::DEPTH_ENV)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        for (k, v) in env {
            c.env(k, v);
        }
        let mut child = c.spawn().unwrap();
        child.stdin.take().unwrap().write_all(stdin).unwrap();
        let out = child.wait_with_output().unwrap();
        let record = fs::read_to_string(self.record_path())
            .ok()
            .map(|s| serde_json::from_str(&s).unwrap());
        (out, record)
    }
    fn ok(&self, args: &[&str], stdin: &[u8]) -> Value {
        let (out, record) = self.run(args, stdin, &[]);
        assert_eq!(
            out.status.code(),
            Some(0),
            "{}",
            String::from_utf8_lossy(&out.stderr)
        );
        assert_eq!(String::from_utf8_lossy(&out.stdout).trim(), "fake-gh-out");
        let record = record.expect("real gh was not called");
        assert_eq!(record["depth"], "1");
        assert_eq!(
            record["console_window"], 0,
            "child must start with CREATE_NO_WINDOW"
        );
        // Owned copies are gone once the gate exits.
        for path in record["files"].as_object().unwrap().keys() {
            if path.contains("cc-gh-gate-") && !path.contains("cc-gh-gate-test-") {
                assert!(!Path::new(path).exists(), "temporary copy left: {path}");
            }
        }
        record
    }
    fn refused(&self, args: &[&str], stdin: &[u8]) -> String {
        let (out, record) = self.run(args, stdin, &[]);
        assert_eq!(out.status.code(), Some(cc_gh_gate::REFUSED));
        assert!(
            record.is_none(),
            "refused call still reached gh: {record:?}"
        );
        let err = String::from_utf8_lossy(&out.stderr).into_owned();
        assert!(err.starts_with("gh-gate: refused: "), "{err}");
        assert!(!err.contains(&token()), "refusal message leaked the secret");
        err
    }
    fn git(&self, args: &[&str]) {
        let out = cc_gh_gate::hidden("git")
            .args(args)
            .current_dir(&self.dir)
            .env("GIT_AUTHOR_NAME", "t")
            .env("GIT_AUTHOR_EMAIL", "t@example.test")
            .env("GIT_COMMITTER_NAME", "t")
            .env("GIT_COMMITTER_EMAIL", "t@example.test")
            .output()
            .unwrap();
        assert!(
            out.status.success(),
            "{}",
            String::from_utf8_lossy(&out.stderr)
        );
    }
}
impl Drop for Env {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.dir);
    }
}
fn argv(record: &Value) -> Vec<String> {
    serde_json::from_value(record["argv"].clone()).unwrap()
}
fn file_for<'a>(record: &'a Value, arg_prefix: &str) -> (&'a str, &'a str) {
    let arg = record["argv"]
        .as_array()
        .unwrap()
        .iter()
        .filter_map(Value::as_str)
        .find(|a| a.starts_with(arg_prefix))
        .unwrap_or_else(|| panic!("no {arg_prefix} in {record}"));
    let path = &arg[arg_prefix.len()..];
    (path, record["files"][path].as_str().unwrap())
}

#[test]
fn inline_title_and_body_are_masked_after_global_flags_and_in_short_forms() {
    let e = Env::new("inline");
    let (t, s) = (token(), service_secret());
    let title = format!("fix {t}");
    let body = format!("-b{s} done");
    let r = e.ok(
        &[
            "-R",
            "o/r",
            "pr",
            "create",
            "--title",
            &title,
            &body,
            "--base=main",
            "-d",
        ],
        b"",
    );
    assert_eq!(
        argv(&r),
        [
            "--repo=o/r",
            "pr",
            "create",
            &format!("--title=fix {MARK}"),
            &format!("--body=serviceSecret={MARK} done"),
            "--base=main",
            "--draft",
        ]
    );
    let r = e.ok(&["pr", "review", "7", "-cb", &format!("see {t}")], b"");
    assert_eq!(
        argv(&r),
        [
            "pr",
            "review",
            "7",
            "--comment",
            &format!("--body=see {MARK}")
        ]
    );
    let r = e.ok(&["issue", "close", "3", &format!("-c={t}")], b"");
    assert_eq!(
        argv(&r),
        ["issue", "close", "3", &format!("--comment={MARK}")]
    );
    let r = e.ok(
        &["pr", "merge", "9", "--squash", "-t", &t, "--body", &t],
        b"",
    );
    assert_eq!(
        argv(&r),
        [
            "pr",
            "merge",
            "9",
            "--squash",
            &format!("--subject={MARK}"),
            &format!("--body={MARK}")
        ]
    );
}

#[test]
fn body_files_and_stdin_become_owned_masked_copies() {
    let e = Env::new("files");
    let caller = e.dir.join("body.md");
    let original = format!(
        "line\nAuthorization: Bearer {}\n{}\n",
        "v1.Zx9Kq2Lm".repeat(3),
        token()
    );
    fs::write(&caller, &original).unwrap();
    let r = e.ok(&["pr", "comment", "5", "-F", "body.md"], b"");
    let (path, content) = file_for(&r, "--body-file=");
    assert_ne!(Path::new(path), caller);
    assert_eq!(
        content,
        format!("line\nAuthorization: Bearer {MARK}\n{MARK}\n")
    );
    assert_eq!(
        fs::read_to_string(&caller).unwrap(),
        original,
        "caller file changed"
    );

    let r = e.ok(
        &["issue", "create", "-t", "x", "--body-file", "-"],
        format!("from stdin {}", service_secret()).as_bytes(),
    );
    let (_, content) = file_for(&r, "--body-file=");
    assert_eq!(content, format!("from stdin serviceSecret={MARK}"));
    assert_eq!(r["stdin"], "", "stdin was consumed by the gate");

    let r = e.ok(
        &[
            "release",
            "create",
            "v1",
            "--notes-file=body.md",
            "-t",
            "v1",
        ],
        b"",
    );
    let (_, content) = file_for(&r, "--notes-file=");
    assert!(!content.contains(&token()) && content.contains(MARK));
}

#[test]
fn api_fields_input_json_and_graphql_variables_are_masked() {
    let e = Env::new("api");
    let t = token();
    fs::write(e.dir.join("field.txt"), format!("body {t}")).unwrap();
    let r = e.ok(
        &[
            "api",
            "-X",
            "POST",
            "repos/{owner}/{repo}/issues",
            "-f",
            &format!("title=x {t}"),
            "--raw-field=labels[]=bug",
            "-Fbody=@field.txt",
            "-F",
            "draft=true",
        ],
        b"",
    );
    let args = argv(&r);
    assert_eq!(
        &args[..4],
        [
            "api",
            "--method=POST",
            "repos/{owner}/{repo}/issues",
            &format!("--raw-field=title=x {MARK}")
        ]
    );
    assert_eq!(args[4], "--raw-field=labels[]=bug");
    assert!(args[5].starts_with("--field=body=@"));
    assert_eq!(args[6], "--field=draft=true");
    let path = &args[5]["--field=body=@".len()..];
    assert_eq!(r["files"][path], format!("body {MARK}"));

    let request = serde_json::json!({
        "query": format!("mutation($b:String!){{addComment(input:{{body:$b, note:\"{t}\"}}){{clientMutationId}}}}"),
        "variables": {"b": format!("text {t}"), "serviceSecret": "abcd1234efgh", "n": 3},
    });
    let r = e.ok(
        &["api", "graphql", "--input", "-"],
        request.to_string().as_bytes(),
    );
    let (_, content) = file_for(&r, "--input=");
    let sent: Value = serde_json::from_str(content).expect("input stays valid JSON");
    assert!(!content.contains(&t));
    assert_eq!(sent["variables"]["b"], format!("text {MARK}"));
    assert_eq!(sent["variables"]["serviceSecret"], MARK);
    assert_eq!(sent["variables"]["n"], 3);
    assert!(
        sent["query"]
            .as_str()
            .unwrap()
            .contains(&format!("note:\"{MARK}\""))
    );
}

#[test]
fn api_escape_flag_is_boolean_and_preserves_existing_value_parsing() {
    let e = Env::new("api-escape");
    for flag in [
        "--allow-escape-sequences",
        "--allow-escape-sequences=true",
        "--allow-escape-sequences=false",
    ] {
        let r = e.ok(
            &[
                "api",
                flag,
                "repos/o/r/actions/jobs/1/logs",
                "-XGET",
                "-H",
                "Accept: application/vnd.github+json",
                "--jq",
                ".message",
                "-i",
            ],
            b"",
        );
        assert_eq!(
            argv(&r),
            [
                "api",
                flag,
                "repos/o/r/actions/jobs/1/logs",
                "--method=GET",
                "--header=Accept: application/vnd.github+json",
                "--jq=.message",
                "--include",
            ]
        );
    }
    let t = token();
    let r = e.ok(
        &[
            "api",
            "repos/o/r/issues",
            "--allow-escape-sequences",
            "-XPOST",
            "-f",
            &format!("body={t}"),
        ],
        b"",
    );
    assert_eq!(
        argv(&r),
        [
            "api",
            "repos/o/r/issues",
            "--allow-escape-sequences",
            "--method=POST",
            &format!("--raw-field=body={MARK}")
        ]
    );
    e.refused(
        &[
            "api",
            "--allow-escape-sequences",
            "--unknown-output-flag",
            "repos/o/r",
        ],
        b"",
    );
    e.refused(
        &["pr", "create", "--allow-escape-sequences", "-b", "x"],
        b"",
    );
}

#[test]
fn api_ansi_response_is_saved_to_a_non_tty_file() {
    let e = Env::new("api-ansi-file");
    let endpoint = "repos/o/r/actions/jobs/1/logs";
    let (out, record) = e.run(&["api", endpoint], b"", &[("FAKE_GH_ANSI", "1")]);
    assert_eq!(out.status.code(), Some(1));
    assert!(record.is_some());
    assert!(out.stdout.is_empty());
    assert!(String::from_utf8_lossy(&out.stderr).contains("--allow-escape-sequences"));

    let path = e.dir.join("synthetic.private.log");
    let out = cc_gh_gate::hidden(env!("CARGO_BIN_EXE_gh_gate"))
        .args(["api", "--allow-escape-sequences", endpoint])
        .current_dir(&e.dir)
        .env(cc_gh_gate::REAL_ENV, &e.real)
        .env_remove(cc_gh_gate::DEPTH_ENV)
        .env("FAKE_GH_RECORD", e.record_path())
        .env("FAKE_GH_ANSI", "1")
        .stdin(Stdio::null())
        .stdout(fs::File::create(&path).unwrap())
        .stderr(Stdio::piped())
        .output()
        .unwrap();
    assert!(out.status.success());
    assert!(out.stdout.is_empty() && out.stderr.is_empty());
    assert!(fs::read(path).unwrap() == b"\x1b[31msynthetic job log\x1b[0m\n");
    let r: Value = serde_json::from_slice(&fs::read(e.record_path()).unwrap()).unwrap();
    assert_eq!(argv(&r), ["api", "--allow-escape-sequences", endpoint]);
    assert_eq!(r["console_window"], 0);
}

#[test]
fn read_only_commands_pass_through_with_stdin_and_exit_code() {
    let e = Env::new("readonly");
    let r = e.ok(
        &["pr", "view", "5", "--json", "title,body", "-q", ".title"],
        b"keep",
    );
    assert_eq!(
        argv(&r),
        ["pr", "view", "5", "--json", "title,body", "-q", ".title"]
    );
    assert_eq!(r["stdin"], "keep");
    let r = e.ok(
        &["api", "repos/o/r/pulls", "--paginate", "--jq", ".[].title"],
        b"",
    );
    assert_eq!(
        argv(&r),
        ["api", "repos/o/r/pulls", "--paginate", "--jq=.[].title"]
    );
    // Credential stores receive their input unchanged.
    let r = e.ok(&["auth", "login", "--with-token"], token().as_bytes());
    assert_eq!(r["stdin"], token());
    let (out, record) = e.run(&["pr", "list"], b"", &[("FAKE_GH_EXIT", "4")]);
    assert_eq!(out.status.code(), Some(4));
    assert!(record.is_some());
}

#[test]
fn unfilterable_forms_are_refused() {
    let e = Env::new("refuse");
    let t = token();
    e.refused(&["pr", "comment", "5", "--editor"], b"");
    e.refused(&["pr", "comment", "5", "-eb", "x"], b"");
    e.refused(&["pr", "create", "--bogus", "x", "-b", "y"], b"");
    e.refused(&["co", "5"], b"");
    e.refused(&["gist", "create", "notes.md"], b"");
    e.refused(&["label", "create", "x", "--description", &t], b"");
    e.refused(&["pr", "revert", "5", "--body-file", "b.md"], b"");
    e.refused(&["workflow", "run", "ci.yml", "--json"], b"{}");
    e.refused(
        &[
            "issue",
            "create",
            "-t",
            "x",
            "-F",
            "-",
            "-b",
            "y",
            "--body-file=-",
        ],
        b"x",
    );
    fs::write(e.dir.join("bin.dat"), [0xff, 0xfe, 0x00]).unwrap();
    e.refused(&["api", "repos/o/r/issues", "--input", "bin.dat"], b"");
    fs::write(e.dir.join("asset.txt"), format!("key {t}")).unwrap();
    e.refused(&["release", "upload", "v1", "asset.txt"], b"");
}

#[test]
fn fill_refuses_secret_commit_messages_and_allows_clean_ones() {
    let e = Env::new("fill");
    e.git(&["init", "-q", "-b", "main"]);
    e.git(&["commit", "-q", "--allow-empty", "-m", "base"]);
    e.git(&["update-ref", "refs/remotes/origin/main", "HEAD"]);
    e.git(&[
        "symbolic-ref",
        "refs/remotes/origin/HEAD",
        "refs/remotes/origin/main",
    ]);
    e.git(&["checkout", "-q", "-b", "feature"]);
    e.git(&["commit", "-q", "--allow-empty", "-m", "clean change"]);
    let r = e.ok(&["pr", "create", "--fill"], b"");
    assert_eq!(argv(&r), ["pr", "create", "--fill"]);
    e.git(&[
        "commit",
        "-q",
        "--allow-empty",
        "-m",
        &format!("add {}", token()),
    ]);
    let err = e.refused(&["pr", "create", "-f", "--base", "main"], b"");
    assert!(err.contains("commit messages"), "{err}");
    e.refused(&["pr", "create", "--fill-verbose"], b"");
}

#[test]
fn real_gh_must_not_lead_back_to_the_gate() {
    let e = Env::new("recursion");
    let gate = env!("CARGO_BIN_EXE_gh_gate");
    let wrapper = e.dir.join("gh.cmd");
    fs::write(&wrapper, "@echo off\r\n").unwrap();
    for real in [
        Path::new(gate),
        wrapper.as_path(),
        &e.dir.join("missing.exe"),
    ] {
        let (out, record) = e.run(
            &["pr", "list"],
            b"",
            &[(cc_gh_gate::REAL_ENV, real.to_str().unwrap())],
        );
        assert_eq!(
            out.status.code(),
            Some(cc_gh_gate::REFUSED),
            "{}",
            real.display()
        );
        assert!(record.is_none());
    }
    let (out, record) = e.run(&["pr", "list"], b"", &[(cc_gh_gate::DEPTH_ENV, "3")]);
    assert_eq!(out.status.code(), Some(cc_gh_gate::REFUSED));
    assert!(record.is_none());
}

#[cfg(windows)]
#[test]
fn gh_cmd_wrapper_reaches_the_gate_and_keeps_the_exit_code() {
    let e = Env::new("wrapper");
    let bin = e.dir.join("bin");
    fs::create_dir(&bin).unwrap();
    fs::copy(env!("CARGO_BIN_EXE_gh_gate"), e.dir.join("gh_gate.exe")).unwrap();
    fs::write(
        bin.join("gh.cmd"),
        include_str!("../bin/gh.cmd")
            .replace("\r\n", "\n")
            .replace('\n', "\r\n"),
    )
    .unwrap();
    let title = format!("x {}", token());
    let out = cc_gh_gate::hidden("cmd.exe")
        .current_dir(&e.dir)
        .args([
            "/d",
            "/c",
            r"bin\gh.cmd",
            "issue",
            "create",
            "-t",
            &title,
            "-b",
            "y",
        ])
        .env("NoDefaultCurrentDirectoryInExePath", "1")
        .env(cc_gh_gate::REAL_ENV, &e.real)
        .env("FAKE_GH_RECORD", e.record_path())
        .env("FAKE_GH_EXIT", "4")
        .env_remove(cc_gh_gate::DEPTH_ENV)
        .stdin(Stdio::null())
        .output()
        .unwrap();
    assert_eq!(
        out.status.code(),
        Some(4),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    let record: Value =
        serde_json::from_str(&fs::read_to_string(e.record_path()).unwrap()).unwrap();
    assert_eq!(
        argv(&record),
        ["issue", "create", &format!("--title=x {MARK}"), "--body=y"]
    );
}

/// An opaque value: no known token shape, so only its field name marks it.
fn opaque() -> String {
    "op".to_string() + &"aqueVal".repeat(3)
}

#[test]
fn credential_field_names_mask_the_whole_value_in_every_api_input() {
    let e = Env::new("fieldkey");
    let o = opaque();
    fs::write(e.dir.join("key.txt"), &o).unwrap();
    let r = e.ok(
        &[
            "api",
            "repos/o/r/actions/variables",
            "-f",
            &format!("serviceSecret={o}"),
            &format!("--raw-field=variables[licenseKey]={o}"),
            "-F",
            "licenseKey=@key.txt",
            "-f",
            &format!("name={o}"),
            "-F",
            "count=3",
        ],
        b"",
    );
    let args = argv(&r);
    assert_eq!(args[2], format!("--raw-field=serviceSecret={MARK}"));
    assert_eq!(args[3], format!("--raw-field=variables[licenseKey]={MARK}"));
    let path = args[4]
        .strip_prefix("--field=licenseKey=@")
        .expect("typed @file stays a file field");
    assert_eq!(r["files"][path], MARK);
    // Fields without a credential name keep their value and type.
    assert_eq!(args[5], format!("--raw-field=name={o}"));
    assert_eq!(args[6], "--field=count=3");
    assert_eq!(fs::read_to_string(e.dir.join("key.txt")).unwrap(), o);

    let r = e.ok(&["api", "graphql", "-F", "token=@-"], o.as_bytes());
    let path = argv(&r)[2]
        .strip_prefix("--field=token=@")
        .unwrap()
        .to_string();
    assert_eq!(r["files"][&path], MARK);
    assert_eq!(r["stdin"], "");

    let request = serde_json::json!({
        "query": "mutation($k:String!){set(licenseKey:$k){ok}}",
        "variables": {"licenseKey": o, "credentials": {"user": "bot", "password": o}, "note": "keep"},
    });
    let r = e.ok(
        &["api", "graphql", "--input", "-"],
        request.to_string().as_bytes(),
    );
    let (_, content) = file_for(&r, "--input=");
    assert!(!content.contains(&o), "{content}");
    let sent: Value = serde_json::from_str(content).unwrap();
    assert_eq!(sent["variables"]["licenseKey"], MARK);
    assert_eq!(sent["variables"]["credentials"], MARK);
    assert_eq!(sent["variables"]["note"], "keep");
    assert_eq!(sent["query"], request["query"]);
}

#[test]
fn refusal_and_io_errors_never_echo_caller_values() {
    let e = Env::new("diag");
    let (t, o) = (token(), opaque());
    for (args, stdin) in [
        (vec!["pr", "create", &format!("--{o}"), "-b", "y"], &b""[..]),
        (vec!["pr", "create", &format!("--{t}=x")], b""),
        (vec![o.as_str(), "x"], b""),
        (vec!["label", o.as_str(), "--body-file", "b.md"], b""),
        (
            vec![
                "pr",
                "comment",
                "5",
                "--body-file",
                &format!("missing-{o}.md"),
            ],
            b"",
        ),
        (
            vec!["api", "graphql", "-F", &format!("licenseKey=@{o}")],
            b"",
        ),
        (vec!["release", "upload", "v1", &format!("{o}.zip")], b""),
    ] {
        let err = e.refused(&args, stdin);
        assert!(!err.contains(&o), "refusal echoed a caller value: {err}");
    }
    // Values of credential-named environment variables are masked as well.
    let (out, _) = e.run(
        &["pr", "create", &format!("--{o}")],
        b"",
        &[("ODD_SERVICE_TOKEN", o.as_str())],
    );
    assert!(!String::from_utf8_lossy(&out.stderr).contains(&o));
}

#[test]
fn git_tree_workflow_boolean_reaches_gh_without_byte_changes() {
    let e = Env::new("checkout-boolean");
    let workflow = "steps:\n  - uses: actions/checkout@v4\n    with:\n      persist-credentials: false\n      fetch-depth: 1\n";
    let request = serde_json::json!({
        "tree": [{"path": ".github/workflows/ci.yml", "mode": "100644", "type": "blob", "content": workflow}],
        "serviceSecret": "opaque-private-value",
    });
    let record = e.ok(
        &[
            "api",
            "repos/owner/repo/git/trees",
            "--method",
            "POST",
            "--input",
            "-",
        ],
        request.to_string().as_bytes(),
    );
    let (_, bytes) = file_for(&record, "--input=");
    let sent: Value = serde_json::from_str(bytes).unwrap();
    assert_eq!(sent["tree"], request["tree"]);
    assert_eq!(sent["tree"][0]["content"], workflow);
    assert_eq!(sent["serviceSecret"], MARK);
}
