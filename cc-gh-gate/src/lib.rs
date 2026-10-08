//! `gh` publication gate. Masks secrets in everything a PR, issue, release or
//! API mutation would publish, then runs the real gh with the same exit code.
//! Read-only commands pass through unchanged. Forms the gate cannot filter are
//! refused instead of being published unfiltered. Boundaries: see README.md.
use cc_redact::Redactor;
use std::{
    collections::HashMap,
    fs,
    io::Read,
    path::PathBuf,
    process::{Command, Stdio},
};

pub const REAL_ENV: &str = "CC_GH_GATE_REAL_GH";
pub const DEPTH_ENV: &str = "CC_GH_GATE_DEPTH";
pub const DEFAULT_REAL: &str = r"C:\Program Files\GitHub CLI\gh.exe";
/// Exit code when the gate refuses to run gh.
pub const REFUSED: i32 = 2;
const NO_WINDOW: u32 = 0x0800_0000;

#[derive(Clone, Copy, PartialEq, Debug)]
enum Kind {
    Bool,
    /// Bool that makes gh use commit messages as the PR title and body.
    Fill,
    Value,
    Text,
    File,
    RawField,
    Field,
    Input,
    Reject,
}
use Kind::*;

/// (long name, short alias or ' ', kind)
type Flag = (&'static str, char, Kind);

const COMMON: &[Flag] = &[("repo", 'R', Value), ("help", 'h', Bool)];
const COMMENT: &[Flag] = &[
    ("body", 'b', Text),
    ("body-file", 'F', File),
    ("create-if-none", ' ', Bool),
    ("delete-last", ' ', Bool),
    ("editor", 'e', Reject),
    ("edit-last", ' ', Bool),
    ("web", 'w', Bool),
    ("yes", ' ', Bool),
];
const EDIT: &[Flag] = &[
    ("add-assignee", ' ', Value),
    ("add-label", ' ', Value),
    ("add-project", ' ', Value),
    ("add-reviewer", ' ', Value),
    ("base", 'B', Value),
    ("body", 'b', Text),
    ("body-file", 'F', File),
    ("milestone", 'm', Value),
    ("remove-assignee", ' ', Value),
    ("remove-label", ' ', Value),
    ("remove-milestone", ' ', Bool),
    ("remove-project", ' ', Value),
    ("remove-reviewer", ' ', Value),
    ("title", 't', Text),
];
const API: &[Flag] = &[
    ("method", 'X', Value),
    ("header", 'H', Value),
    ("raw-field", 'f', RawField),
    ("field", 'F', Field),
    ("input", ' ', Input),
    ("jq", 'q', Value),
    ("template", 't', Value),
    ("preview", 'p', Value),
    ("hostname", ' ', Value),
    ("cache", ' ', Value),
    ("include", 'i', Bool),
    ("paginate", ' ', Bool),
    ("slurp", ' ', Bool),
    ("silent", ' ', Bool),
    ("verbose", ' ', Bool),
];

/// Commands whose text the gate rewrites. Unknown flags here are refused.
fn table(cmd: &str, sub: &str) -> Option<&'static [Flag]> {
    Some(match (cmd, sub) {
        ("api", _) => API,
        ("pr", "create") => &[
            ("assignee", 'a', Value),
            ("base", 'B', Value),
            ("body", 'b', Text),
            ("body-file", 'F', File),
            ("draft", 'd', Bool),
            ("dry-run", ' ', Bool),
            ("editor", 'e', Reject),
            ("fill", 'f', Fill),
            ("fill-first", ' ', Fill),
            ("fill-verbose", ' ', Fill),
            ("head", 'H', Value),
            ("label", 'l', Value),
            ("milestone", 'm', Value),
            ("no-maintainer-edit", ' ', Bool),
            ("project", 'p', Value),
            ("recover", ' ', Reject),
            ("reviewer", 'r', Value),
            ("title", 't', Text),
            ("template", 'T', Value),
            ("web", 'w', Bool),
        ],
        ("issue", "create") => &[
            ("assignee", 'a', Value),
            ("body", 'b', Text),
            ("body-file", 'F', File),
            ("editor", 'e', Reject),
            ("label", 'l', Value),
            ("milestone", 'm', Value),
            ("project", 'p', Value),
            ("recover", ' ', Reject),
            ("template", 'T', Value),
            ("title", 't', Text),
            ("web", 'w', Bool),
        ],
        ("pr" | "issue", "edit") => EDIT,
        ("pr" | "issue", "comment") => COMMENT,
        ("pr", "review") => &[
            ("approve", 'a', Bool),
            ("body", 'b', Text),
            ("body-file", 'F', File),
            ("comment", 'c', Bool),
            ("request-changes", 'r', Bool),
        ],
        ("pr", "merge") => &[
            ("admin", ' ', Bool),
            ("author-email", 'A', Value),
            ("auto", ' ', Bool),
            ("body", 'b', Text),
            ("body-file", 'F', File),
            ("delete-branch", 'd', Bool),
            ("disable-auto", ' ', Bool),
            ("match-head-commit", ' ', Value),
            ("merge", 'm', Bool),
            ("rebase", 'r', Bool),
            ("squash", 's', Bool),
            ("subject", 't', Text),
        ],
        ("pr", "close") => &[("comment", 'c', Text), ("delete-branch", 'd', Bool)],
        ("issue", "close") => &[
            ("comment", 'c', Text),
            ("reason", 'r', Value),
            ("duplicate-of", ' ', Value),
        ],
        ("pr" | "issue", "reopen") => &[("comment", 'c', Text)],
        ("release", "create") => &[
            ("discussion-category", ' ', Value),
            ("draft", 'd', Bool),
            ("fail-on-no-commits", ' ', Bool),
            ("generate-notes", ' ', Bool),
            ("latest", ' ', Bool),
            ("notes", 'n', Text),
            ("notes-file", 'F', File),
            ("notes-from-tag", ' ', Reject),
            ("notes-start-tag", ' ', Value),
            ("prerelease", 'p', Bool),
            ("target", ' ', Value),
            ("title", 't', Text),
            ("verify-tag", ' ', Bool),
        ],
        ("release", "edit") => &[
            ("discussion-category", ' ', Value),
            ("draft", ' ', Bool),
            ("latest", ' ', Bool),
            ("notes", 'n', Text),
            ("notes-file", 'F', File),
            ("prerelease", ' ', Bool),
            ("tag", ' ', Value),
            ("target", ' ', Value),
            ("title", 't', Text),
            ("verify-tag", ' ', Bool),
        ],
        ("release", "upload") => &[("clobber", ' ', Bool)],
        _ => return None,
    })
}

const BUILTIN: &[&str] = &[
    "accessibility",
    "agent-task",
    "alias",
    "api",
    "attestation",
    "auth",
    "browse",
    "cache",
    "codespace",
    "completion",
    "config",
    "copilot",
    "extension",
    "gist",
    "gpg-key",
    "help",
    "issue",
    "label",
    "licenses",
    "org",
    "pr",
    "preview",
    "project",
    "release",
    "repo",
    "ruleset",
    "run",
    "search",
    "secret",
    "ssh-key",
    "status",
    "variable",
    "version",
    "workflow",
];

/// An owned temporary copy, removed when the gate finishes.
pub struct Temp(pub PathBuf);
impl Drop for Temp {
    fn drop(&mut self) {
        let _ = fs::remove_file(&self.0);
    }
}

pub struct Gate<'a> {
    redactor: Redactor,
    stdin: &'a mut dyn Read,
    /// The child gets no stdin once the gate consumed it.
    pub stdin_used: bool,
    pub temps: Vec<Temp>,
}

fn refuse<T>(reason: impl Into<String>) -> Result<T, String> {
    Err(reason.into())
}

/// A caller-supplied command or flag name for an error message. gh names are
/// short lowercase words; anything else may be a value and is not echoed.
fn shown(name: &str) -> &str {
    let word = name.len() <= 32
        && name.starts_with(|c: char| c.is_ascii_lowercase())
        && name
            .bytes()
            .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-');
    if word { name } else { "<hidden>" }
}

impl<'a> Gate<'a> {
    pub fn new(redactor: Redactor, stdin: &'a mut dyn Read) -> Self {
        Self {
            redactor,
            stdin,
            stdin_used: false,
            temps: Vec::new(),
        }
    }

    /// The argv to give the real gh, or why the call is refused.
    pub fn rewrite(&mut self, args: &[String]) -> Result<Vec<String>, String> {
        // gh only accepts --repo before the command words.
        let mut words = Vec::new();
        let mut i = 0;
        while i < args.len() && words.len() < 2 {
            match args[i].as_str() {
                "--" => break,
                "-R" | "--repo" => i += 1,
                a if !a.starts_with('-') || a == "-" => words.push(i),
                _ => (),
            }
            i += 1;
        }
        let word = |n: usize| words.get(n).map_or("", |&i| args[i].as_str());
        let (cmd, sub) = (word(0), word(1));
        if cmd == "api" {
            words.truncate(1);
        }
        match table(cmd, sub) {
            Some(flags) => self.publish(cmd, sub, args, &words, flags),
            None => self.other(cmd, sub, args),
        }
    }

    fn publish(
        &mut self,
        cmd: &str,
        sub: &str,
        args: &[String],
        words: &[usize],
        flags: &[Flag],
    ) -> Result<Vec<String>, String> {
        let find = |f: &dyn Fn(&Flag) -> bool| flags.iter().chain(COMMON).find(|x| f(x)).copied();
        let (mut out, mut positional, mut values) = (Vec::new(), Vec::new(), HashMap::new());
        let mut fill = false;
        let mut i = 0;
        while i < args.len() {
            let a = &args[i];
            if words.contains(&i) {
                out.push(a.clone());
            } else if a == "--" {
                out.push(a.clone());
                for rest in &args[i + 1..] {
                    positional.push(rest.clone());
                    out.push(self.redactor.text(rest).into_owned());
                }
                break;
            } else if let Some(body) = a.strip_prefix("--") {
                let (name, inline) = match body.split_once('=') {
                    Some((n, v)) => (n, Some(v.to_string())),
                    None => (body, None),
                };
                let Some((long, _, kind)) = find(&|f| f.0 == name) else {
                    return refuse(format!("unknown flag --{} for gh {cmd} {sub}", shown(name)));
                };
                match kind {
                    Bool | Fill => {
                        fill |= kind == Fill;
                        out.push(a.clone());
                    }
                    Reject => return refuse(format!("--{long} input cannot be filtered")),
                    _ => {
                        let v = match inline {
                            Some(v) => v,
                            None => {
                                i += 1;
                                args.get(i)
                                    .cloned()
                                    .ok_or(format!("--{long} needs a value"))?
                            }
                        };
                        values.insert(long, v.clone());
                        out.push(format!("--{long}={}", self.value(kind, long, v)?));
                    }
                }
            } else if a.len() > 1 && a.starts_with('-') {
                // pflag short forms: -b text, -btext, -b=text and grouped bools (-db text).
                let chars = a[1..].chars().collect::<Vec<_>>();
                for (j, &c) in chars.iter().enumerate() {
                    let Some((long, _, kind)) = find(&|f| f.1 == c) else {
                        return refuse(format!(
                            "unknown flag -{} for gh {cmd} {sub}",
                            shown(&c.to_string())
                        ));
                    };
                    match kind {
                        Bool | Fill => {
                            fill |= kind == Fill;
                            out.push(format!("--{long}"));
                        }
                        Reject => return refuse(format!("--{long} input cannot be filtered")),
                        _ => {
                            let rest = chars[j + 1..].iter().collect::<String>();
                            let v = if rest.is_empty() {
                                i += 1;
                                args.get(i).cloned().ok_or(format!("-{c} needs a value"))?
                            } else {
                                rest.strip_prefix('=').unwrap_or(&rest).to_string()
                            };
                            values.insert(long, v.clone());
                            out.push(format!("--{long}={}", self.value(kind, long, v)?));
                            break;
                        }
                    }
                }
            } else {
                positional.push(a.clone());
                out.push(self.redactor.text(a).into_owned());
            }
            i += 1;
        }
        if fill {
            self.check_commits(values.get("base"), values.get("head"))?;
        }
        if cmd == "release" && matches!(sub, "create" | "upload") {
            // Assets are uploaded as they are; refuse any that contain a secret.
            for (n, asset) in positional.iter().skip(1).enumerate() {
                let path = asset.split('#').next().unwrap_or(asset);
                let bytes =
                    fs::read(path).map_err(|e| format!("read release asset {}: {e}", n + 1))?;
                if self
                    .redactor
                    .contains_secret(&String::from_utf8_lossy(&bytes))
                {
                    return refuse(format!("release asset {} contains a secret", n + 1));
                }
            }
        }
        Ok(out)
    }

    fn value(&mut self, kind: Kind, long: &str, v: String) -> Result<String, String> {
        Ok(match kind {
            Text => self.redactor.text(&v).into_owned(),
            File => self.copy(&v, long, false)?,
            RawField => self.field(&v, false)?,
            Field => self.field(&v, true)?,
            Input => self.copy(&v, long, true)?,
            _ => v,
        })
    }

    /// `key=value`; with -F a value of `@path` or `@-` is read from a file or stdin.
    /// A credential key (`licenseKey`, `variables[token]`) masks the whole value.
    fn field(&mut self, kv: &str, typed: bool) -> Result<String, String> {
        let Some((key, value)) = kv.split_once('=') else {
            return Ok(kv.into());
        };
        let value = match value.strip_prefix('@') {
            Some(path) if typed => format!("@{}", self.copy_field(path, key)?),
            _ => self.redactor.field(key, value).into_owned(),
        };
        Ok(format!("{key}={value}"))
    }

    fn copy_field(&mut self, source: &str, key: &str) -> Result<String, String> {
        let text = self.read(source, "field")?;
        let masked = self.redactor.field(key, &text).into_owned();
        self.write_temp(masked.into_bytes())
    }

    /// The text of a path or `-` (stdin). Errors never echo the path, which
    /// is a caller value.
    fn read(&mut self, source: &str, long: &str) -> Result<String, String> {
        String::from_utf8(self.read_bytes(source, long)?)
            .map_err(|_| format!("--{long} input is not UTF-8 text"))
    }

    fn read_bytes(&mut self, source: &str, long: &str) -> Result<Vec<u8>, String> {
        Ok(if source == "-" {
            if self.stdin_used {
                return refuse("stdin can be read only once");
            }
            self.stdin_used = true;
            let mut b = Vec::new();
            self.stdin
                .read_to_end(&mut b)
                .map_err(|e| format!("read stdin for --{long}: {e}"))?;
            b
        } else {
            fs::read(source).map_err(|e| format!("read the --{long} file: {e}"))?
        })
    }

    fn write_temp(&mut self, bytes: Vec<u8>) -> Result<String, String> {
        let temp = self.temp()?;
        fs::write(&temp.0, bytes).map_err(|e| format!("write a temporary copy: {e}"))?;
        let path = temp.0.to_string_lossy().into_owned();
        self.temps.push(temp);
        Ok(path)
    }

    /// Read a body source (a path or `-` for stdin) and return the path of an
    /// owned, masked copy. The caller's file is never written.
    fn copy(&mut self, source: &str, long: &str, json: bool) -> Result<String, String> {
        let bytes = self.read_bytes(source, long)?;
        let masked = match serde_json::from_slice::<serde_json::Value>(&bytes) {
            // API bodies (GraphQL variables included): mask parsed strings only.
            Ok(mut v) if json => {
                let before = v.clone();
                self.redactor.json(&mut v);
                if v == before {
                    bytes
                } else {
                    serde_json::to_vec(&v).unwrap()
                }
            }
            _ => match std::str::from_utf8(&bytes) {
                Ok(text) => self.redactor.text(text).into_owned().into_bytes(),
                Err(_) => return refuse(format!("--{long} input is not UTF-8 text")),
            },
        };
        self.write_temp(masked)
    }

    fn temp(&self) -> Result<Temp, String> {
        let dir = std::env::temp_dir();
        for n in 0..100 {
            let nanos = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .map_or(0, |d| d.subsec_nanos());
            let path = dir.join(format!(
                "cc-gh-gate-{}-{nanos}-{}-{n}.txt",
                std::process::id(),
                self.temps.len()
            ));
            match fs::OpenOptions::new()
                .write(true)
                .create_new(true)
                .open(&path)
            {
                Ok(_) => return Ok(Temp(path)),
                Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => continue,
                Err(e) => return refuse(format!("create temporary file: {e}")),
            }
        }
        refuse("create temporary file: name collision")
    }

    /// `--fill*` publishes commit messages; they must be clean.
    fn check_commits(&self, base: Option<&String>, head: Option<&String>) -> Result<(), String> {
        let unreadable =
            "cannot read the commit messages --fill would publish; pass --title and --body";
        let base = match base {
            Some(b) => ["origin/", ""]
                .iter()
                .map(|p| format!("{p}{b}"))
                .find(|r| git(&["rev-parse", "--verify", "--quiet", r]).is_some()),
            None => git(&["symbolic-ref", "--short", "refs/remotes/origin/HEAD"])
                .map(|s| s.trim().to_string()),
        }
        .ok_or(unreadable)?;
        let head = head.map_or("HEAD", |h| h.rsplit(':').next().unwrap_or(h));
        let log = git(&["log", "--format=%B", &format!("{base}..{head}")]).ok_or(unreadable)?;
        if self.redactor.contains_secret(&log) {
            return refuse(
                "commit messages --fill would publish contain a secret; pass --title and --body",
            );
        }
        Ok(())
    }

    fn other(&mut self, cmd: &str, sub: &str, args: &[String]) -> Result<Vec<String>, String> {
        if cmd.is_empty() {
            return Ok(args.to_vec());
        }
        if !BUILTIN.contains(&cmd) {
            return refuse(format!(
                "gh {} is an alias or extension the gate cannot inspect; run the full gh command",
                shown(cmd)
            ));
        }
        // Credential stores, not publications: values pass unchanged.
        if matches!(cmd, "auth" | "secret") {
            return Ok(args.to_vec());
        }
        match (cmd, sub) {
            ("gist", "create" | "edit") => {
                return refuse("gist contents are published unfiltered; use a PR or issue");
            }
            ("workflow", "run") if args.iter().any(|a| a == "--json") => {
                return refuse("workflow inputs from stdin are not filtered; pass -f key=value");
            }
            ("variable", "set")
                if !args
                    .iter()
                    .any(|a| a.starts_with("--body") || a.starts_with("-b")) =>
            {
                return refuse("variable values from stdin or files are not filtered; pass --body");
            }
            _ => (),
        }
        if args.iter().any(|a| {
            [
                "--body-file",
                "--notes-file",
                "--input",
                "--recover",
                "--env-file",
            ]
            .iter()
            .any(|f| a.starts_with(f))
                || a.contains("=@")
        }) {
            return refuse(format!(
                "file input to gh {cmd} {} is not filtered",
                shown(sub)
            ));
        }
        if args.iter().any(|a| self.redactor.contains_secret(a)) {
            return refuse(format!(
                "an argument to gh {cmd} {} contains a secret",
                shown(sub)
            ));
        }
        Ok(args.to_vec())
    }
}

pub fn hidden(program: impl AsRef<std::ffi::OsStr>) -> Command {
    let mut command = Command::new(program);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(NO_WINDOW);
    }
    command
}

fn git(args: &[&str]) -> Option<String> {
    let out = hidden("git")
        .args(args)
        .stdin(Stdio::null())
        .stderr(Stdio::null())
        .output()
        .ok()?;
    out.status
        .success()
        .then(|| String::from_utf8_lossy(&out.stdout).into_owned())
}

/// The real gh.exe, never this gate or a wrapper script that leads back to it.
pub fn real_gh() -> Result<PathBuf, String> {
    let path = std::env::var_os(REAL_ENV)
        .filter(|v| !v.is_empty())
        .map_or_else(|| PathBuf::from(DEFAULT_REAL), PathBuf::from);
    let real = fs::canonicalize(&path).map_err(|e| format!("real gh {}: {e}", path.display()))?;
    let me = std::env::current_exe()
        .and_then(fs::canonicalize)
        .map_err(|e| format!("locate gate: {e}"))?;
    if real == me
        || !real
            .extension()
            .is_some_and(|e| e.eq_ignore_ascii_case("exe"))
    {
        return refuse(format!(
            "{REAL_ENV} must name the real gh.exe, not this gate or a script ({})",
            path.display()
        ));
    }
    Ok(real)
}

/// Entry point of gh_gate.exe; returns the process exit code.
pub fn run() -> i32 {
    let redactor = Redactor::with_env(std::env::vars());
    // Reasons are masked too, so a refusal never becomes another secret-bearing log.
    let fail = |reason: String| {
        eprintln!("gh-gate: refused: {}", redactor.text(&reason));
        REFUSED
    };
    let depth = std::env::var(DEPTH_ENV)
        .ok()
        .and_then(|v| v.parse::<u32>().ok())
        .unwrap_or(0);
    if depth >= 3 {
        return fail("gh-gate is calling itself; check the real gh path".into());
    }
    let Ok(args) = std::env::args_os()
        .skip(1)
        .map(|a| a.into_string())
        .collect::<Result<Vec<_>, _>>()
    else {
        return fail("non-UTF-8 argument".into());
    };
    let real = match real_gh() {
        Ok(real) => real,
        Err(e) => return fail(e),
    };
    let mut stdin = std::io::stdin();
    let mut gate = Gate::new(redactor.clone(), &mut stdin);
    let argv = match gate.rewrite(&args) {
        Ok(argv) => argv,
        Err(e) => return fail(e),
    };
    let mut child = hidden(&real);
    child.args(&argv).env(DEPTH_ENV, (depth + 1).to_string());
    if gate.stdin_used {
        child.stdin(Stdio::null());
    }
    let code = match child.status() {
        Ok(status) => status.code().unwrap_or(1),
        Err(e) => return fail(format!("start {}: {e}", real.display())),
    };
    drop(gate);
    code
}
