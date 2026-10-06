//! Project labels for owner questions. Client names never appear in posted text.
use crate::core::*;
use regex::{Regex, RegexBuilder};
use std::path::Path;

pub struct ProjectNames {
    clients: Vec<Regex>,
}

impl ProjectNames {
    pub fn load(path: &Path) -> Result<Self> {
        let text = read_text(path)?;
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
        })
    }

    pub fn redact(&self, text: &str) -> String {
        self.clients.iter().fold(text.to_owned(), |text, name| {
            name.replace_all(&text, "クライアント案件").into_owned()
        })
    }

    pub fn label(&self, project: &str) -> String {
        if self.clients.iter().any(|name| name.is_match(project)) {
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
    let root = ctx.effects.run(
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
