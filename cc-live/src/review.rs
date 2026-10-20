//! Start a read-only review of a batch PR.
//!
//! review.exe <name> <pr_number> <facts_file> [source_job_id]
use crate::*;
use serde_json::json;
pub const TEMPLATE: &str = "Review PR #{pr} of the Civ6 MOD 「Civ6用万世紀」 (current folder = the PR branch feat/{name}; compare with `git diff origin/main...HEAD`). Follow documents/repository-rules.md and the SSOT in documents/spec/ssot (read only the relevant sections). Answer in Japanese.\n\nThis is a review only: do NOT modify, create or delete any file, do not run git commands that change state, do not use gh or the GitHub API, no API keys, do not launch Civ6/Steam, no browser tools, no sub-agents. The sandbox is read-only.\n\nCheck correctness against the SSOT and the task, engine behavior against the official files under \"C:/Program Files (x86)/Steam/steamapps/common/Sid Meier's Civilization VI\" (cite file:line), save/load and multiplayer determinism where relevant, scoping (only RULESET_JAC_BANSEIKI / Sengoku pack / Japan maps as intended), interactions with other features, tests and the live scenario's power to detect failure, and docs. Look for the same class of problem elsewhere, not only where it first appears.\n\nVerified facts (live or by tests) — if a concern contradicts them, still report it but mark it 要実機確認 with your reasoning:\n{facts}\n\nReport findings as Must / Suggestion / Nit with file:line and confidence (高/中/低). Include uncertain concerns too. End with a one-line verdict.\n";

fn source_models(
    p: &Paths,
    name: &str,
    explicit: Option<&String>,
) -> Result<(Vec<String>, Vec<String>), String> {
    let mut candidates = Vec::new();
    if let Some(id) = explicit {
        candidates.push(id.clone());
    } else {
        if let Ok(id) = std::env::var("CC_JOB_ID") {
            candidates.push(id);
        }
        for entry in std::fs::read_dir(&p.jobs).map_err(|e| e.to_string())? {
            let entry = entry.map_err(|e| e.to_string())?;
            let id = entry.file_name().to_string_lossy().into_owned();
            if id.ends_with(&format!("-{name}")) {
                candidates.push(id);
            }
        }
    }
    candidates.sort();
    candidates.dedup();
    let target = cc_job_policy::work_owner::norm(&p.wt.join(name));
    let mut ids = Vec::new();
    let mut models = Vec::new();
    for id in candidates {
        if id.is_empty()
            || id.len() > 128
            || !id
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b"_-".contains(&b))
        {
            if explicit.is_some() {
                return Err("invalid review source job identity".into());
            }
            continue;
        }
        let record = match read_json(p.jobs.join(&id).join("job.json")) {
            Ok(v) => v,
            Err(_) if explicit.is_none() => continue,
            Err(e) => return Err(e),
        };
        let cwd = record["cwd"].as_str().unwrap_or("");
        if record["id"].as_str() != Some(id.as_str())
            || cwd.is_empty()
            || cc_job_policy::work_owner::norm(std::path::Path::new(cwd)) != target
            || cc_job_policy::review_task(
                record["name"].as_str().unwrap_or(""),
                record["task_type"].as_str().unwrap_or(""),
            )
        {
            continue;
        }
        let found = cc_job_policy::author_models(&record);
        if found.is_empty() {
            continue;
        }
        ids.push(id);
        models.extend(found);
    }
    if ids.is_empty() {
        return Err("review source model metadata unavailable for this worktree".into());
    }
    Ok((ids, models))
}

pub fn cli(r: &mut impl Runner, p: &Paths, a: &[String]) -> Result<i32, String> {
    if a.len() < 3 {
        return Err("review needs name, pr_number and facts_file".into());
    }
    let (name, pr, facts) = (&a[0], &a[1], read_text(&a[2])?);
    let (sources, authors) = source_models(p, name, a.get(3))?;
    let profile = cc_job_policy::review_profile(&authors)
        .ok_or("no independent review model is available")?;
    let prompt = p.here.join(format!("rev_{name}_in.txt"));
    // Replace placeholders in a single pass; braces/placeholders inside facts remain literal.
    let text = TEMPLATE
        .split("{facts}")
        .map(|s| s.replace("{pr}", pr).replace("{name}", name))
        .collect::<Vec<_>>()
        .join(facts.trim());
    write_text(&prompt, &text)?;
    let cj = p
        .jobs
        .parent()
        .ok_or("jobs parent missing")?
        .join("codex_jobs.exe");
    let out = checked(
        r,
        Call::new(args(&[
            &path(cj),
            "run",
            "--name",
            &format!("rev-{name}"),
            "--model",
            profile.model,
            "--effort",
            profile.effort,
            "--review-source-jobs",
            &sources.join(","),
            "--sandbox",
            "read-only",
            "--cwd",
            &path(p.wt.join(name)),
            "--prompt-file",
            &path(prompt),
            "--task-type",
            "review",
        ])),
        "review launch",
    )?;
    let job = out
        .stdout
        .trim()
        .lines()
        .last()
        .filter(|line| !line.is_empty())
        .ok_or("review launch returned no job id")?;
    r.print(job)?;
    Ok(0)
}
/// merge_pr.exe <pr> <comment.md> -> post comment, mark ready, squash-merge
pub fn merge_cli(r: &mut impl Runner, p: &Paths, a: &[String]) -> Result<i32, String> {
    if a.len() < 2 {
        return Err("merge_pr needs pr and comment.md".into());
    }
    let pr = &a[0];
    let payload = p.temp.join(format!("c{pr}.json"));
    write_json(&payload, &json!({"body":read_text(&a[1])?}))?;
    let out = checked(
        r,
        Call::new(args(&[
            "gh",
            "api",
            "-X",
            "POST",
            &format!("repos/{}/issues/{pr}/comments", ship::REPO),
            "--input",
            &path(payload),
            "--jq",
            ".html_url",
        ])),
        "review comment post",
    )?;
    r.print(out.stdout.trim())?;
    let draft = checked(
        r,
        Call::new(args(&[
            "gh",
            "pr",
            "view",
            pr,
            "-R",
            ship::REPO,
            "--json",
            "isDraft",
            "--jq",
            ".isDraft",
        ])),
        "PR draft query",
    )?;
    let is_draft: bool = serde_json::from_str(draft.stdout.trim())
        .map_err(|error| format!("PR draft query returned an invalid boolean: {error}"))?;
    if is_draft {
        checked(
            r,
            Call::new(args(&["gh", "pr", "ready", pr, "-R", ship::REPO])),
            "PR ready",
        )?;
    }
    checked(
        r,
        Call::new(args(&[
            "gh",
            "pr",
            "merge",
            pr,
            "-R",
            ship::REPO,
            "--squash",
        ])),
        "PR merge",
    )?;
    let state = checked(
        r,
        Call::new(args(&[
            "gh",
            "pr",
            "view",
            pr,
            "-R",
            ship::REPO,
            "--json",
            "state",
            "--jq",
            ".state",
        ])),
        "PR merge verification",
    )?;
    if state.stdout.trim() != "MERGED" {
        return Err(format!(
            "PR merge was not confirmed: state {}",
            state.stdout.trim()
        ));
    }
    r.print("merged")?;
    Ok(0)
}

fn checked(r: &mut impl Runner, call: Call, operation: &str) -> Result<Output, String> {
    let out = r
        .run(call)
        .map_err(|error| format!("{operation}: {error}"))?;
    if out.code != 0 {
        return Err(format!(
            "{operation} failed (exit {}): {} {}",
            out.code,
            out.stdout.trim(),
            out.stderr.trim()
        ));
    }
    Ok(out)
}
