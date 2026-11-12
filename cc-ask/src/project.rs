//! Project labels for owner questions. Client names never appear in posted text.
use crate::core::*;
use regex::{Regex, RegexBuilder};
use std::path::Path;

/// This is ordinary source classification, never an image-purpose grant.
pub(crate) fn qualify_source(effects: &mut dyn Effects, path: &Path) -> Result<()> {
    #[derive(serde::Deserialize)]
    struct Proof {
        ok: bool,
        access_allowed: bool,
        client_resource: bool,
        classification_unverified: bool,
    }
    let path =
        std::path::absolute(path).map_err(|_| "question source qualification unavailable")?;
    let output = effects
        .run(
            &[
                "D:/CC/Tools/client-records/scrub391.exe".into(),
                "--guard-path".into(),
                path.to_string_lossy().into_owned(),
            ],
            10,
        )
        .map_err(|_| "question source qualification unavailable")?;
    if output.code != 0 || !output.stderr.is_empty() || output.stdout.len() > 65536 {
        return Err("question source qualification unavailable".into());
    }
    let proof = serde_json::from_str::<Proof>(&output.stdout)
        .map_err(|_| "question source qualification unavailable")?;
    if !proof.ok
        || !proof.access_allowed
        || proof.client_resource
        || proof.classification_unverified
    {
        return Err("question source is protected or unverified".into());
    }
    Ok(())
}

pub struct QuestionSourceLease {
    files: Vec<std::fs::File>,
}

pub fn question_text(path: &Path) -> Result<String> {
    use std::io::Read;
    const LIMIT: u64 = 1 << 20;
    let file = open_regular_source(path).map_err(|_| "question source read unavailable")?;
    let meta = file
        .metadata()
        .map_err(|_| "question source read unavailable")?;
    if !meta.is_file() || meta.len() > LIMIT {
        return Err("question input exceeds 1 MiB".into());
    }
    let mut bytes = Vec::with_capacity(meta.len() as usize);
    file.take(LIMIT + 1)
        .read_to_end(&mut bytes)
        .map_err(|_| "question source read unavailable")?;
    if bytes.len() as u64 > LIMIT {
        return Err("question input exceeds 1 MiB".into());
    }
    String::from_utf8(bytes)
        .map(|text| text.replace("\r\n", "\n").replace('\r', "\n"))
        .map_err(|_| "question source must be UTF-8".into())
}

pub(crate) fn lease_source(effects: &mut dyn Effects, path: &Path) -> Result<QuestionSourceLease> {
    qualify_source(effects, path)?;
    let resolved =
        std::fs::canonicalize(path).map_err(|_| "question source qualification unavailable")?;
    let normal =
        std::path::PathBuf::from(crate::resume::strip_verbatim(&resolved.to_string_lossy()));
    qualify_source(effects, &normal)?;
    let lease = pin_plain_source(path)?;
    qualify_source(effects, path)?;
    qualify_source(effects, &normal)?;
    Ok(lease)
}

/// Retain a plain named object without reading its content. Authorization is
/// supplied by the caller's ordinary source or exact image-purpose verifier.
pub(crate) fn pin_plain_source(path: &Path) -> Result<QuestionSourceLease> {
    let resolved =
        std::fs::canonicalize(path).map_err(|_| "question source identity unavailable")?;
    #[cfg(windows)]
    let mut files = Vec::new();
    #[cfg(windows)]
    {
        use std::os::windows::fs::{MetadataExt, OpenOptionsExt};
        // Pin the original namespace, including every parent component. A
        // canonical target lease alone does not retain a mutable source link.
        let absolute =
            std::path::absolute(path).map_err(|_| "question source identity unavailable")?;
        let parents: Vec<_> = absolute.ancestors().skip(1).collect();
        for parent in parents.into_iter().rev() {
            let file = std::fs::OpenOptions::new()
                .access_mode(0x80)
                .share_mode(3)
                .custom_flags(0x02200000)
                .open(parent)
                .map_err(|_| "question source identity unavailable")?;
            if file
                .metadata()
                .map_err(|_| "question source identity unavailable")?
                .file_attributes()
                & 0x400
                != 0
            {
                return Err("question source identity unavailable".into());
            }
            files.push(file);
        }
        let file = std::fs::OpenOptions::new()
            .read(true)
            .share_mode(1)
            .custom_flags(0x00200000)
            .open(&absolute)
            .map_err(|_| "question source identity unavailable")?;
        let meta = file
            .metadata()
            .map_err(|_| "question source identity unavailable")?;
        if !meta.is_file() || meta.file_attributes() & 0x400 != 0 {
            return Err("question source identity unavailable".into());
        }
        if std::fs::canonicalize(&absolute).map_err(|_| "question source identity unavailable")?
            != resolved
        {
            return Err("question source identity changed".into());
        }
        files.push(file);
    }
    #[cfg(unix)]
    let files = {
        let file =
            open_plain_unix_source(path).map_err(|_| "question source identity unavailable")?;
        if std::fs::canonicalize(path).map_err(|_| "question source identity unavailable")?
            != resolved
        {
            return Err("question source identity changed".into());
        }
        vec![file]
    };
    #[cfg(not(any(windows, unix)))]
    let files = {
        return Err("question source identity unavailable".into());
    };
    // Windows handles now exclude file writes/deletes and parent replacement;
    // reclassify that still-named object before any caller consumes its bytes.
    Ok(QuestionSourceLease { files })
}

#[cfg(unix)]
fn open_plain_unix_source(path: &Path) -> std::io::Result<std::fs::File> {
    use std::os::unix::fs::OpenOptionsExt;
    let file = std::fs::OpenOptions::new()
        .read(true)
        .custom_flags(libc::O_NONBLOCK | libc::O_NOFOLLOW)
        .open(path)?;
    // Check the opened object, not a pathname checked before open. In
    // particular, opening a FIFO must not wait for a writer before rejection.
    if !file.metadata()?.is_file() {
        return Err(std::io::Error::new(
            std::io::ErrorKind::InvalidInput,
            "question source must be a regular file",
        ));
    }
    Ok(file)
}

pub(crate) fn open_regular_source(path: &Path) -> std::io::Result<std::fs::File> {
    #[cfg(unix)]
    {
        open_plain_unix_source(path)
    }
    #[cfg(not(unix))]
    {
        std::fs::File::open(path)
    }
}

#[cfg(all(test, unix))]
mod unix_source_tests {
    use super::*;

    #[test]
    fn no_writer_fifo_and_directory_are_rejected_before_content_read() {
        const CHILD: &str = "CC_ASK_OWNED_FIFO_UNIT";
        if std::env::var_os(CHILD).is_none() {
            let mut child = std::process::Command::new(std::env::current_exe().unwrap())
                .args(["--exact", "project::unix_source_tests::no_writer_fifo_and_directory_are_rejected_before_content_read", "--nocapture"])
                .env(CHILD,"1").spawn().unwrap();
            let start = std::time::Instant::now();
            loop {
                if let Some(status) = child.try_wait().unwrap() {
                    assert!(status.success());
                    break;
                }
                if start.elapsed() > std::time::Duration::from_secs(2) {
                    child.kill().unwrap();
                    child.wait().unwrap();
                    panic!("FIFO unit open did not return");
                }
                std::thread::sleep(std::time::Duration::from_millis(10));
            }
            return;
        }
        use std::os::unix::ffi::OsStrExt;
        let root = tempfile::tempdir().unwrap();
        let fifo = root.path().join("input.png");
        let name = std::ffi::CString::new(fifo.as_os_str().as_bytes()).unwrap();
        assert_eq!(unsafe { libc::mkfifo(name.as_ptr(), 0o600) }, 0);
        let before = std::time::Instant::now();
        assert!(pin_plain_source(&fifo).is_err());
        assert!(before.elapsed() < std::time::Duration::from_secs(1));
        assert!(pin_plain_source(root.path()).is_err());
    }

    #[test]
    fn held_regular_object_passes_and_final_link_is_not_followed() {
        use std::os::unix::fs::symlink;
        let root = tempfile::tempdir().unwrap();
        let plain = root.path().join("regular.png");
        std::fs::write(&plain, b"owned image fixture").unwrap();
        let file = open_plain_unix_source(&plain).unwrap();
        assert!(file.metadata().unwrap().is_file());
        assert!(pin_plain_source(&plain).is_ok());
        let link = root.path().join("link.png");
        symlink(&plain, &link).unwrap();
        assert!(open_plain_unix_source(&link).is_err());
        assert_eq!(std::fs::read(plain).unwrap(), b"owned image fixture");
    }
}

pub fn qualify_question_sources(
    effects: &mut dyn Effects,
    registry: &Path,
    args: &crate::commands::AskArgs,
) -> Result<QuestionSourceLease> {
    qualify_question_sources_with(effects, registry, args, true)
}

pub(crate) fn qualify_question_sources_with(
    effects: &mut dyn Effects,
    registry: &Path,
    args: &crate::commands::AskArgs,
    ordinary_attachments: bool,
) -> Result<QuestionSourceLease> {
    let names = ProjectNames::load_for_owner_questions(registry)?;
    if !names.catalog_only() {
        crate::ask_gate::enforce(
            &args.title,
            &question_text(&args.body_file)?,
            args.owner_only,
        )?;
        if let Some(path) = &args.decision_file {
            question_text(path)?;
        }
        return Ok(QuestionSourceLease { files: Vec::new() });
    }
    let cwd = args
        .cwd
        .clone()
        .filter(|cwd| !cwd.is_empty())
        .map(std::path::PathBuf::from)
        .map(Ok)
        .unwrap_or_else(std::env::current_dir)
        .map_err(|_| "question source qualification unavailable")?;
    qualify_source(effects, &cwd)?;
    let mut held = QuestionSourceLease { files: Vec::new() };
    for path in std::iter::once(&args.body_file)
        .chain(args.decision_file.iter())
        .chain(args.attach.iter().filter(|_| ordinary_attachments))
    {
        held.files.extend(lease_source(effects, path)?.files);
    }
    let root = effects.run(
        &[
            "git".into(),
            "-C".into(),
            cwd.to_string_lossy().into_owned(),
            "rev-parse".into(),
            "--show-toplevel".into(),
        ],
        10,
    );
    let folder = root
        .as_ref()
        .ok()
        .filter(|out| out.code == 0)
        .and_then(|out| out.stdout.lines().next())
        .filter(|line| !line.trim().is_empty())
        .map(str::to_owned)
        .unwrap_or_else(|| cwd.to_string_lossy().into_owned());
    if names.label(&project_from_folder(&folder)) == "プロジェクト不明" {
        return Err("project privacy classification unavailable for this project".into());
    }
    // Classifying a source never asserts that arbitrary prose is public. Keep
    // the sender's credential checks, and reject rather than rewrite a bound
    // question when catalog-protected text occurs in its original inputs.
    let body = question_text(&args.body_file)?;
    crate::ask_gate::enforce(&args.title, &body, args.owner_only)?;
    for text in std::iter::once(args.title.clone())
        .chain(std::iter::once(body))
        .chain(
            args.decision_file
                .iter()
                .map(|path| question_text(path))
                .collect::<Result<Vec<_>>>()?,
        )
    {
        crate::recovery_messages::validate_followup_text(&text)?;
        if names.redact(&text) != text {
            return Err("question input contains protected project information".into());
        }
    }
    Ok(held)
}

pub struct ProjectNames {
    clients: Vec<Regex>,
    catalog: Option<crate::project_catalog::Catalog>,
}

impl ProjectNames {
    pub fn catalog_only(&self) -> bool {
        self.catalog.is_some()
    }
    pub fn load(path: &Path) -> Result<Self> {
        Self::load_inner(path, false)
    }

    /// Existing question displays may use the normal department projection.
    /// Original decision text continues to require its original name registry.
    pub fn load_for_owner_questions(path: &Path) -> Result<Self> {
        Self::load_inner(path, true)
    }

    fn load_inner(path: &Path, use_catalog: bool) -> Result<Self> {
        let text = match std::fs::read_to_string(path) {
            Ok(text) => text,
            Err(error)
                if use_catalog
                    && error.kind() == std::io::ErrorKind::NotFound
                    && std::fs::symlink_metadata(path)
                        .is_err_and(|error| error.kind() == std::io::ErrorKind::NotFound) =>
            {
                return Ok(Self {
                    clients: Vec::new(),
                    catalog: Some(crate::project_catalog::Catalog::embedded()?),
                });
            }
            Err(error) => {
                return Err(format!(
                    "OSError: project privacy source unreadable (kind={:?}, code={:?})",
                    error.kind(),
                    error.raw_os_error()
                ))
            }
        };
        let text = text.replace("\r\n", "\n").replace('\r', "\n");
        let mut names: Vec<_> = text
            .trim_start_matches('\u{feff}')
            .lines()
            .map(str::trim)
            .filter(|name| !name.is_empty() && !name.starts_with('#'))
            .collect();
        // Replace longer names first when entries overlap.
        names.sort_by_key(|name| std::cmp::Reverse(name.len()));
        Ok(Self {
            clients: names
                .into_iter()
                .map(|name| {
                    RegexBuilder::new(&regex::escape(name))
                        .case_insensitive(true)
                        .build()
                        .unwrap()
                })
                .collect(),
            catalog: None,
        })
    }

    pub fn redact(&self, text: &str) -> String {
        let text = self.clients.iter().fold(text.to_owned(), |text, name| {
            name.replace_all(&text, "クライアント案件").into_owned()
        });
        self.catalog
            .as_ref()
            .map_or(text.clone(), |catalog| catalog.redact(&text))
    }

    pub fn label(&self, project: &str) -> String {
        if self
            .catalog
            .as_ref()
            .is_some_and(|catalog| !catalog.known_project(project))
        {
            "プロジェクト不明".into()
        } else if self.clients.iter().any(|name| name.is_match(project))
            || self
                .catalog
                .as_ref()
                .is_some_and(|catalog| catalog.redact(project) != project)
        {
            "クライアント案件".into()
        } else {
            project.to_owned()
        }
    }
}

/// Accept either Windows or Git slash spelling, including deleted worktree paths.
pub fn project_from_folder(folder: &str) -> String {
    let normalized = folder.trim().replace('\\', "/");
    let parts: Vec<_> = normalized
        .split('/')
        .filter(|part| !part.is_empty())
        .collect();
    for pair in parts.windows(2) {
        if pair[0].eq_ignore_ascii_case("worktrees") {
            return pair[1].to_owned();
        }
    }
    for part in &parts {
        if part.to_lowercase().ends_with("-wt") && part.len() > 3 {
            return part[..part.len() - 3].to_owned();
        }
    }
    if parts.len() >= 3
        && parts[0].eq_ignore_ascii_case("D:")
        && parts[1].eq_ignore_ascii_case("CC")
        && parts[2].eq_ignore_ascii_case("Tools")
    {
        return "PCツール".into();
    }
    parts.last().unwrap_or(&"プロジェクト不明").to_string()
}

pub fn determine_project(ctx: &mut Context, cwd: &str, names: &ProjectNames) -> String {
    let root = ctx.run(
        &[
            "git".into(),
            "-C".into(),
            cwd.into(),
            "rev-parse".into(),
            "--show-toplevel".into(),
        ],
        10,
    );
    let folder = root
        .as_ref()
        .ok()
        .filter(|out| out.code == 0)
        .and_then(|out| out.stdout.lines().next())
        .filter(|line| !line.trim().is_empty())
        .unwrap_or(cwd);
    names.label(&project_from_folder(folder))
}

/// Posted/recovered questions often have a title but no working directory.
pub fn question_project(
    ctx: &mut Context,
    question: &serde_json::Value,
    names: &ProjectNames,
) -> String {
    let title = s(&question["title"]).trim();
    let title = if title.is_empty() {
        s(&question["name"]).trim()
    } else {
        title
    };
    let title = Regex::new(r"^\[[^\]]+\]\s*").unwrap().replace(title, "");
    if let Some((project, rest)) = title.strip_prefix('【').and_then(|v| v.split_once('】')) {
        if project == "アイデア" {
            if let Some(product) = Regex::new(r"^[A-Za-z][A-Za-z0-9_.-]*")
                .unwrap()
                .find(rest.trim())
            {
                return names.label(product.as_str());
            }
        }
        if !project.trim().is_empty() && project != "プロジェクト不明" {
            return names.label(&safe_text(project.trim()));
        }
    }
    let project = s(&question["project"]);
    if !project.is_empty() && project != "プロジェクト不明" {
        names.label(&safe_text(project))
    } else if !s(&question["cwd"]).is_empty() {
        determine_project(ctx, s(&question["cwd"]), names)
    } else {
        "この作業".into()
    }
}

/// Recover routing from existing metadata, then the known project roots. This is
/// an internal routing decision, never a question for the owner.
pub fn infer_question_cwd(
    ctx: &Context,
    state: &serde_json::Value,
    question: &serde_json::Value,
) -> String {
    let tid = s(&question["thread_id"]);
    let request = &state["requests"][tid];
    for candidate in [
        question,
        &question["continuation"],
        &question["continuation"]["job_info"],
        request,
        &request["job_info"],
    ] {
        if Path::new(s(&candidate["cwd"])).is_dir() {
            return s(&candidate["cwd"]).to_owned();
        }
    }
    for previous in state["questions"]
        .as_array()
        .into_iter()
        .flatten()
        .rev()
        .filter(|q| s(&q["thread_id"]) == tid)
    {
        if Path::new(s(&previous["cwd"])).is_dir() {
            return s(&previous["cwd"]).to_owned();
        }
    }
    let title = s(&question["title"]);
    let title = Regex::new(r"^\[[^\]]+\]\s*").unwrap().replace(title, "");
    let project = title
        .strip_prefix('【')
        .and_then(|v| v.split_once('】'))
        .map(|(p, _)| p)
        .filter(|p| !["プロジェクト不明", "アイデア", "この作業"].contains(p))
        .unwrap_or(s(&question["project"]));
    if !project.is_empty() && !project.contains(['/', '\\', ':']) && ![".", ".."].contains(&project)
    {
        if let Ok(todo) = read_json(&ctx.todo) {
            if let Some(projects) = todo["projects"].as_object() {
                for (name, folder) in projects {
                    if (name.eq_ignore_ascii_case(project)
                        || project_from_folder(s(folder)).eq_ignore_ascii_case(project))
                        && Path::new(s(folder)).is_dir()
                    {
                        return s(folder).to_owned();
                    }
                }
            }
        }
        for root in [REQUEST_CWD, "D:/CC"] {
            let folder = Path::new(root).join(project);
            if folder.is_dir() {
                return folder.to_string_lossy().into_owned();
            }
        }
    }
    if Path::new(REQUEST_CWD).is_dir() {
        REQUEST_CWD.into()
    } else if Path::new("D:/CC").is_dir() {
        "D:/CC".into()
    } else {
        ctx.here.to_string_lossy().into_owned()
    }
}

pub fn thread_title(project: &str, _agent: &str, title: &str) -> String {
    crate::task_title::format_title(&crate::task_title::Names::parse(""), project, title)
}

pub fn question_body(project: &str, owner: &str, body: &str) -> String {
    let prefix = format!("【{project}】");
    let body = body.trim();
    let body = body.strip_prefix(&prefix).unwrap_or(body).trim_start();
    format!("{prefix}\n<@{owner}> {body}\n\n-# このスレッドに返信してください。")
}

pub fn reminder_text(project: &str, owner: &str, title: &str) -> String {
    let title = title
        .strip_prefix(&format!("【{project}】"))
        .unwrap_or(title);
    let agent = Regex::new(r"^\[[^\]]+\]\s*").unwrap();
    let title = agent.replace(title, "");
    // Existing titles may put the project prefix after the agent.
    let title = title
        .strip_prefix(&format!("【{project}】"))
        .unwrap_or(&title)
        .trim();
    format!("<@{owner}> 【{project}】「{title}」の件で、上の質問への回答をお願いしたいです！🙇（1時間たったので再送です）")
}
