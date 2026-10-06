#![allow(dead_code)]
use cc_ask::core::*;
use serde_json::{json, Value};
use std::{
    cell::RefCell,
    collections::{BTreeMap, VecDeque},
    path::PathBuf,
    rc::Rc,
};

pub struct Harness {
    pub root: tempfile::TempDir,
    pub ctx: Context,
    pub fake: Rc<RefCell<Fake>>,
}
pub struct Fake {
    pub recover_fail: bool,
    pub codex_directories: Vec<String>,
    pub channels: Vec<Value>,
    pub root: PathBuf,
    pub now: f64,
    pub threads: BTreeMap<String, Value>,
    pub messages: BTreeMap<String, Vec<Value>>,
    pub sequence: u64,
    pub http_calls: Vec<Value>,
    pub posts: Vec<(String, Value)>,
    pub patches: Vec<(String, Value)>,
    pub picks: Vec<Value>,
    pub pick_replies: VecDeque<Value>,
    pub runs: Vec<(Vec<String>, u64)>,
    pub codex_labels: String,
    pub launches: Vec<Value>,
    pub git_calls: Vec<(String, Vec<String>)>,
    pub git_roots: BTreeMap<String, String>,
    pub pick_down: bool,
    pub remove_session_on_pick: Option<PathBuf>,
    pub launch_error: bool,
    pub launch_error_message: String,
    pub launch_transport_fail: bool,
    pub crash_after_todo_add: bool,
    pub post_error: bool,
    pub archive_error: bool,
    pub git_error: bool,
    pub crash_before_launch: bool,
    pub crash_after_launch: bool,
    pub lose_launch_response: bool,
    pub http_errors: BTreeMap<String, usize>,
    pub history_injection: Option<(usize, String, String)>,
}
pub fn message(mid: &str, text: &str, owner: &str) -> Value {
    json!({"id":mid,"author":{"id":owner},"content":text,"timestamp":"2026-09-30T10:00:00+00:00","attachments":[]})
}
impl Harness {
    pub fn new() -> Self {
        // The user's home may itself be a repository. Python keeps fixtures under
        // the tool directory so a mocked non-repository probe has no real .git ancestor.
        let root = tempfile::tempdir_in(env!("CARGO_MANIFEST_DIR")).unwrap();
        let path = root.path().to_path_buf();
        std::fs::write(path.join("client-projects.txt"), "").unwrap();
        std::fs::create_dir(path.join("discord-tasks")).unwrap();
        let fake = Rc::new(RefCell::new(Fake {
            recover_fail: false,
            codex_directories: vec![],
            channels: vec![],
            root: path.clone(),
            now: 1790773200.,
            sequence: 1000,
            threads: BTreeMap::from([
                (
                    "100".into(),
                    json!({"id":"100","type":11,"parent_id":REQUEST_CHANNEL_ID,"owner_id":"OWN","name":"依頼A","thread_metadata":{"archived":false}}),
                ),
                (
                    "200".into(),
                    json!({"id":"200","type":11,"parent_id":REQUEST_CHANNEL_ID,"owner_id":"BOT","name":"無視","thread_metadata":{"archived":false}}),
                ),
            ]),
            messages: BTreeMap::from([
                ("100".into(), vec![message("100", "READMEを直して", "OWN")]),
                ("200".into(), vec![]),
            ]),
            http_calls: vec![],
            posts: vec![],
            patches: vec![],
            picks: vec![],
            pick_replies: VecDeque::new(),
            runs: vec![],
            codex_labels: String::new(),
            launches: vec![],
            git_calls: vec![],
            git_roots: BTreeMap::new(),
            pick_down: false,
            remove_session_on_pick: None,
            launch_error: false,
            launch_error_message: "mock failure".into(),
            launch_transport_fail: false,
            crash_after_todo_add: false,
            post_error: false,
            archive_error: false,
            git_error: false,
            crash_before_launch: false,
            crash_after_launch: false,
            lose_launch_response: false,
            http_errors: BTreeMap::new(),
            history_injection: None,
        }));
        let ctx = Context {
            here: path.clone(),
            client_projects: path.join("client-projects.txt"),
            civ6_home: path.join("civ6-live"),
            jobs: path.clone(),
            discord_tasks: path.join("discord-tasks"),
            todo: path.join("todo.json"),
            cfg: json!({"owner_user_id":"OWN","guild_id":"G","task_channel_id":"T"}),
            effects: Box::new(SharedFake(fake.clone())),
            codex_command: path.join("codex_jobs.py"),
            scheduler_command: path.join("scheduler.py"),
            shepherd_command: path.join("shepherd.py"),
            todo_command: path.join("todo.py"),
        };
        Self { root, ctx, fake }
    }
    pub fn state(&self) -> Value {
        self.ctx.load().unwrap()
    }
    pub fn save(&self, state: &Value) {
        self.ctx.save(state).unwrap()
    }
    pub fn text(&self) -> String {
        s(&self.fake.borrow().posts.last().unwrap().1["content"]).into()
    }
    pub fn reply(&self, text: &str, tid: &str) -> String {
        let mut f = self.fake.borrow_mut();
        f.sequence += 1;
        let id = f.sequence.to_string();
        f.messages
            .entry(tid.into())
            .or_default()
            .push(message(&id, text, "OWN"));
        f.threads.get_mut(tid).unwrap()["thread_metadata"]["archived"] = json!(false);
        id
    }
    pub fn finish(&self, jid: &str, status: &str, result: Option<&str>, error: Option<&str>) {
        let dir = self.ctx.jobs.join("jobs").join(jid);
        let mut info = read_json(&dir.join("job.json")).unwrap();
        info["status"] = json!(status);
        info["last_error"] = json!(error);
        atomic_json(&dir.join("job.json"), &info, 2).unwrap();
        if let Some(text) = result {
            std::fs::write(dir.join("last_message.md"), text).unwrap();
        }
    }
    pub fn tick(&mut self) {
        let before = self.fake.borrow().launches.len();
        cc_ask::intake::cmd_intake(&mut self.ctx, &mut false).unwrap();
        assert!(self.fake.borrow().launches.len() - before <= 1);
    }
    pub fn recover(&mut self, jid: &str) -> String {
        self.finish(jid, "failed", None, Some("proxy timeout"));
        self.fake.borrow_mut().now += 60.;
        self.tick();
        s(&self.fake.borrow().launches.last().unwrap()["id"]).into()
    }
}
pub struct SharedFake(pub Rc<RefCell<Fake>>);
impl Effects for SharedFake {
    fn codex_directories(&mut self) -> Result<Vec<String>> {
        Ok(self.0.borrow().codex_directories.clone())
    }
    fn now(&self) -> f64 {
        self.0.borrow().now
    }
    fn http(&mut self, m: &str, url: &str, body: Option<&Value>, discord: bool) -> Result<Value> {
        self.0.borrow_mut().http(m, url, body, discord)
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        // Unwind outside the RefCell borrow, so interrupted tests can immediately inspect persisted state.
        let before = {
            let mut f = self.0.borrow_mut();
            let v = f.crash_before_launch && argv.iter().any(|x| x == "--launch-key");
            if v {
                f.crash_before_launch = false;
            }
            v
        };
        if before {
            panic!("mock interruption before launcher")
        }
        let out = self.0.borrow_mut().run(argv, timeout);
        let todo_crash = {
            let mut f = self.0.borrow_mut();
            let crash = f.crash_after_todo_add && argv.iter().any(|a| a == "add");
            if crash {
                f.crash_after_todo_add = false;
            }
            crash
        };
        if todo_crash {
            panic!("mock interruption after ToDo add");
        }
        let after = {
            let mut f = self.0.borrow_mut();
            let v = f.crash_after_launch && argv.iter().any(|x| x == "--launch-key");
            if v {
                f.crash_after_launch = false;
            }
            v
        };
        if after {
            panic!("mock interruption after launcher")
        }
        out
    }
}
impl Fake {
    pub fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        if method == "GET" && url.contains("/messages?limit=100") {
            if let Some((remaining, _, _)) = &mut self.history_injection {
                *remaining -= 1;
                if *remaining == 0 {
                    let (_, tid, text) = self.history_injection.take().unwrap();
                    self.sequence += 1;
                    self.messages.entry(tid).or_default().push(message(
                        &self.sequence.to_string(),
                        &text,
                        "OWN",
                    ));
                }
            }
        }
        let path = url
            .strip_prefix("https://discord.com/api/v10")
            .unwrap_or(url);
        self.http_calls
            .push(json!({"method":method,"url":url,"body":body,"discord":discord}));
        let method_key = format!("{method} {path}");
        let error_key = if self.http_errors.contains_key(&method_key) {
            &method_key
        } else {
            path
        };
        if let Some(left) = self.http_errors.get_mut(error_key) {
            if *left > 0 {
                *left -= 1;
                return Err("mock HTTP unavailable".into());
            }
        }
        if !discord {
            if url == "http://127.0.0.1:10012/api/resident/restart" {
                return Ok(json!({"ok":true}));
            }
            let body = body.cloned().unwrap_or(Value::Null);
            self.picks.push(body.clone());
            if let Some(path) = self.remove_session_on_pick.take() {
                std::fs::remove_file(path).map_err(|_| "mock session removal failed")?;
            }
            if self.pick_down {
                return Err("mock router unavailable".into());
            }
            if let Some(reply) = self.pick_replies.pop_front() {
                return Ok(reply);
            }
            let excluded = body["exclude"].as_array().cloned().unwrap_or_default();
            let model = ["gpt-6.1-sol", "gpt-6-luna", "gpt-6-sol", "auto"]
                .into_iter()
                .find(|m| !excluded.contains(&json!(m)))
                .unwrap();
            return Ok(
                json!({"model":model,"effort":"max","task_type":"implementation","reason":"実装の依頼なので選びました。","ranking":[],"source":"jev"}),
            );
        }
        if path == "/channels/T" {
            return Ok(json!({"type":15}));
        }
        if path == "/users/@me" {
            return Ok(json!({"id":"BOT","bot":true}));
        }
        if path == "/guilds/G/channels" {
            return Ok(json!(self.channels));
        }
        if path == "/channels/T/threads" {
            let b = body.unwrap();
            let thread = json!({"id":"300","type":11,"last_message_id":"300","name":b["name"],"parent_id":"T","owner_id":"BOT","thread_metadata":{"archived":false}});
            self.threads.insert("300".into(), thread.clone());
            self.messages.insert(
                "300".into(),
                vec![message("300", s(&b["message"]["content"]), "BOT")],
            );
            return Ok(thread);
        }
        if method == "POST" && path == "/channels/GENERAL/threads" {
            let b = body.unwrap();
            let thread = json!({"id":"300","guild_id":"G","type":11,"name":b["name"],"parent_id":"GENERAL","owner_id":"BOT","thread_metadata":{"archived":false}});
            self.threads.insert("300".into(), thread.clone());
            self.messages.insert("300".into(), vec![]);
            self.posts.push(("GENERAL".into(), b.clone()));
            return Ok(thread);
        }
        if path.ends_with("/threads/active") {
            return Ok(
                json!({"threads":self.threads.values().filter(|t|!truth(&t["thread_metadata"]["archived"])).cloned().collect::<Vec<_>>()}),
            );
        }
        if path.contains("/threads/archived/public?limit=25")
            || path.contains("/threads/archived/public?limit=100")
        {
            let parent = path.split('/').nth(2).unwrap_or("");
            return Ok(
                json!({"threads":self.threads.values().filter(|t|truth(&t["thread_metadata"]["archived"]) && s(&t["parent_id"]) == parent).cloned().collect::<Vec<_>>(),"has_more":false}),
            );
        }
        let tid = path.split('/').nth(2).unwrap_or("").to_string();
        if method == "PATCH" {
            if self.archive_error {
                self.archive_error = false;
                return Err("mock archive unavailable".into());
            }
            let b = body.unwrap().clone();
            self.patches.push((tid.clone(), b.clone()));
            if path.contains("/messages/") {
                let mid = path.rsplit('/').next().unwrap();
                let message = self
                    .messages
                    .get_mut(&tid)
                    .and_then(|ms| ms.iter_mut().find(|m| s(&m["id"]) == mid))
                    .ok_or("message missing")?;
                for (key, value) in b.as_object().unwrap() {
                    message[key] = value.clone();
                }
                return Ok(message.clone());
            }
            let t = self.threads.get_mut(&tid).ok_or("unknown thread")?;
            if truth(&t["thread_metadata"]["archived"]) && b.get("name").is_some() {
                return Err("Thread is archived".into());
            }
            for (k, v) in b.as_object().unwrap() {
                if k == "name" {
                    t[k] = v.clone();
                } else {
                    t["thread_metadata"][k] = v.clone();
                }
            }
            return Ok(t.clone());
        }
        if method == "POST" {
            if self.post_error {
                self.post_error = false;
                return Err("mock post unavailable".into());
            }
            self.sequence += 1;
            let b = body.unwrap();
            let mut msg = message(&self.sequence.to_string(), s(&b["content"]), "BOT");
            msg["author"]["bot"] = json!(true);
            if b.get("message_reference").is_some() {
                msg["message_reference"] = b["message_reference"].clone();
            }
            self.messages
                .entry(tid.clone())
                .or_default()
                .push(msg.clone());
            self.posts.push((tid, b.clone()));
            return Ok(msg);
        }
        if path.contains("/messages/") {
            let id = path.rsplit('/').next().unwrap();
            return self
                .messages
                .get(&tid)
                .and_then(|ms| ms.iter().find(|m| s(&m["id"]) == id))
                .cloned()
                .ok_or("message missing".into());
        }
        if path.contains("/messages") {
            let query = |key: &str, default: u64| {
                path.split(&format!("{key}="))
                    .nth(1)
                    .and_then(|s| s.split('&').next())
                    .and_then(|s| s.parse().ok())
                    .unwrap_or(default)
            };
            let (after, before) = (query("after", 0), query("before", u64::MAX));
            let mut ms = self.messages.get(&tid).cloned().unwrap_or_default();
            ms.retain(|m| n(&m["id"]) > after && n(&m["id"]) < before);
            ms.sort_by_key(|m| std::cmp::Reverse(n(&m["id"])));
            ms.truncate(100);
            return Ok(json!(ms));
        }
        self.threads
            .get(&tid)
            .cloned()
            .ok_or("unknown thread".into())
    }
    pub fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        self.runs.push((argv.to_vec(), timeout));
        if argv.first().is_some_and(|s| s.ends_with("codex.exe")) {
            return Ok(Output {
                stdout: self.codex_labels.clone(),
                ..Output::default()
            });
        }
        if argv.last().is_some_and(|a| a == "recover") && self.recover_fail {
            return Ok(Output {
                code: 1,
                ..Default::default()
            });
        }
        if argv.first().map(String::as_str) == Some("git") {
            let cwd = argv[2].clone();
            let args = argv[3..].to_vec();
            self.git_calls.push((cwd.clone(), args.clone()));
            if self.git_error {
                return Ok(Output {
                    code: 1,
                    stderr: "mock git unavailable".into(),
                    ..Default::default()
                });
            }
            if args[0] == "rev-parse" {
                let key = |p: &str| p.replace('\\', "/").to_lowercase();
                let c = key(&cwd);
                for (root, repo) in self.git_roots.iter().rev() {
                    let r = key(root);
                    if c == r || c.starts_with(&(r + "/")) {
                        return Ok(Output {
                            stdout: format!(
                                "{}\n{}\n",
                                root,
                                PathBuf::from(repo).join(".git").display()
                            ),
                            ..Default::default()
                        });
                    }
                }
                return Ok(Output {
                    code: 128,
                    stderr: "not a git repository".into(),
                    ..Default::default()
                });
            }
            if args[0] == "worktree" {
                let target = &args[args.len() - 2];
                std::fs::create_dir_all(target).unwrap();
                std::fs::write(PathBuf::from(target).join(".git"), "mock worktree").unwrap();
                self.git_roots.insert(target.clone(), cwd);
            }
            if args.first().map(String::as_str) == Some("remote") {
                return Ok(Output {
                    stdout: "fixture-origin".into(),
                    ..Default::default()
                });
            }
            if args.first().map(String::as_str) == Some("show-ref") {
                return Ok(Output {
                    code: 1,
                    ..Default::default()
                });
            }
            return Ok(Output::default());
        }
        let arg = |flag: &str| {
            argv.iter()
                .position(|a| a == flag)
                .and_then(|i| argv.get(i + 1))
                .cloned()
                .unwrap_or_default()
        };
        if argv
            .iter()
            .any(|a| a.ends_with("todo.py") || a.ends_with("todo.exe"))
        {
            let offset = if argv[0].ends_with("python.exe") {
                2
            } else {
                1
            };
            let args = &argv[offset..];
            let file = self.root.join("todo.json");
            let mut db = if file.exists() {
                read_json(&file)?
            } else {
                json!({"projects":{},"items":[]})
            };
            let stdout = match args[0].as_str() {
                "project" => {
                    db["projects"][&args[1]] = json!(args[2]);
                    String::new()
                }
                "add" => {
                    let id = db["items"]
                        .as_array()
                        .unwrap()
                        .iter()
                        .map(|i| n(&i["id"]))
                        .max()
                        .unwrap_or(0)
                        + 1;
                    db["items"].as_array_mut().unwrap().push(json!({"id":id,"project":args[1],"title":args[2],"note":arg("--note"),"status":"todo","jobs":[]}));
                    format!("added {id}")
                }
                "start" => {
                    let item = db["items"]
                        .as_array_mut()
                        .unwrap()
                        .iter_mut()
                        .find(|i| n(&i["id"]).to_string() == args[1])
                        .unwrap();
                    item["status"] = json!("doing");
                    item["updated"] = json!(chrono::DateTime::from_timestamp(self.now as i64, 0)
                        .unwrap()
                        .to_rfc3339());
                    item["jobs"]
                        .as_array_mut()
                        .unwrap()
                        .push(json!(arg("--job")));
                    String::new()
                }
                "done" => {
                    let item = db["items"]
                        .as_array_mut()
                        .unwrap()
                        .iter_mut()
                        .find(|i| n(&i["id"]).to_string() == args[1])
                        .unwrap();
                    item["status"] = json!("done");
                    String::new()
                }
                _ => String::new(),
            };
            atomic_json(&file, &db, 2)?;
            return Ok(Output {
                stdout,
                ..Default::default()
            });
        }
        if arg("--launch-key").is_empty() {
            if argv.iter().any(|a| a == "cancel") {
                let i = argv.iter().position(|a| a == "cancel").unwrap();
                let file = self.root.join("jobs").join(&argv[i + 1]).join("job.json");
                let mut job = read_json(&file)?;
                job["status"] = json!("cancelled");
                atomic_json(&file, &job, 2)?;
            }
            return Ok(Output::default());
        }
        if self.launch_transport_fail {
            return Err("OSError".into());
        }
        assert_eq!(timeout, 45);
        let prompt_path = arg("--prompt-file");
        let prompt = std::fs::read_to_string(&prompt_path).unwrap();
        let name = arg("--name");
        let mut record = json!({"model":arg("--model"),"effort":arg("--effort"),"sandbox":arg("--sandbox"),"task_type":arg("--task-type"),"launch_key":arg("--launch-key"),"cwd":arg("--cwd"),"name":name,"prompt":prompt,"prompt_path":prompt_path,"resume_session":arg("--resume-session"),"diagnosis":name.starts_with("diag-")});
        record["mcp_profile"] = json!(arg("--mcp-profile"));
        record["scope_source"] = json!(arg("--scope-source"));
        if self.launch_error {
            self.launch_error = false;
            self.launches.push(record);
            return Ok(Output {
                code: 1,
                stderr: self.launch_error_message.clone(),
                ..Default::default()
            });
        }
        let jid = format!("job{}", self.launches.len() + 1);
        record["id"] = json!(jid);
        let dir = self.root.join("jobs").join(&jid);
        std::fs::create_dir_all(&dir).unwrap();
        atomic_json(&dir.join("job.json"),&json!({"id":jid,"status":"running","model":record["model"],"effort":record["effort"],"task_type":record["task_type"],"sandbox":record["sandbox"],"scope_source":record["scope_source"],"thread_id":format!("session-{jid}"),"launch_key":record["launch_key"],"cwd":record["cwd"]}),2).unwrap();
        std::fs::write(dir.join("prompt.md"), s(&record["prompt"])).unwrap();
        self.launches.push(record);
        if self.lose_launch_response {
            self.lose_launch_response = false;
            return Err("mock launcher response lost".into());
        }
        Ok(Output {
            stdout: jid + "\n",
            ..Default::default()
        })
    }
}
