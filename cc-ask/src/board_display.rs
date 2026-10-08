//! Compact job board; no model calls, notifications or shared ToDo mutations.
use crate::core::*;
use cc_task_title::Names;
use chrono::{DateTime, FixedOffset};
use serde_json::Value;
use std::collections::BTreeMap;

pub const MACHINE_PROBE: &str = "$ErrorActionPreference='Stop'; $o=Get-CimInstance Win32_OperatingSystem; $c=(Get-CimInstance Win32_Processor | Measure-Object LoadPercentage -Average).Average; [pscustomobject]@{cpu=$c; used=($o.TotalVisibleMemorySize-$o.FreePhysicalMemory)/1MB; total=$o.TotalVisibleMemorySize/1MB} | ConvertTo-Json -Compress";

fn clean(text: &str, names: &Names, limit: usize) -> String {
    let text = cc_task_title::clean(&names.redact(text));
    let text = text.replace(['`', '*', '@', '<', '>', '\n', '\r'], " ");
    let text = text.split_whitespace().collect::<Vec<_>>().join(" ");
    let mut short: String = text.chars().take(limit).collect();
    if text.chars().count() > limit {
        short.push('…');
    }
    if short.is_empty() {
        "作業内容を確認する".into()
    } else {
        short
    }
}

pub fn project_label(project: &str, names: &Names) -> String {
    let p = names.project(project);
    match p.as_str() {
        "civ6-banseiki" | "civ6" => "万世紀".into(),
        "GoDD-Teacher-Odin" => "Odin".into(),
        "GoDD-MCP-Alpha" => "GoDD MCP α".into(),
        "alt-line" => "ALT-LINE".into(),
        "health-channel" => "健康ナビ".into(),
        "phone-friction-control" => "視聴時間アプリ".into(),
        "tools" | "PCツール" | "cc-ask" => "PCの道具".into(),
        "" => "その他".into(),
        _ => clean(&p, names, usize::MAX),
    }
}

#[derive(Default)]
struct Group {
    running: usize,
    queued: usize,
    titles: BTreeMap<String, usize>,
}

/// Excludes HH:MM, so elapsed time alone never causes a PATCH.
pub fn render(status: &Value, machine: &Value, names: &Names, now: f64) -> Result<String> {
    let jobs = status["jobs"].as_array().ok_or("invalid board jobs")?;
    let mut groups: BTreeMap<String, Group> = BTreeMap::new();
    let (mut running, mut queued, mut finished, mut failed) = (0, 0, 0, 0);
    for job in jobs {
        let st = s(&job["status"]);
        if matches!(st, "succeeded" | "failed" | "lost")
            && DateTime::parse_from_rfc3339(s(&job["finished_at"]))
                .ok()
                .is_some_and(|at| {
                    let age = now - at.timestamp() as f64;
                    (0.0..=10800.0).contains(&age)
                })
        {
            finished += 1;
            if st != "succeeded" {
                failed += 1;
            }
        }
        if !matches!(st, "running" | "starting" | "queued") {
            continue;
        }
        let group = groups
            .entry(project_label(s(&job["project"]), names))
            .or_default();
        if st == "queued" {
            queued += 1;
            group.queued += 1;
        } else {
            running += 1;
            group.running += 1;
        }
        *group
            .titles
            .entry(clean(s(&job["task_title"]), names, usize::MAX))
            .or_default() += 1;
    }
    let cpu = machine["cpu"]
        .as_f64()
        .filter(|v| v.is_finite() && (0.0..=100.0).contains(v));
    let mem = machine["used"]
        .as_f64()
        .zip(machine["total"].as_f64())
        .filter(|(used, total)| {
            used.is_finite() && total.is_finite() && *used >= 0.0 && *total > 0.0 && used <= total
        });
    let cpu = cpu
        .map(|v| {
            let v = v.round() as usize;
            format!("{v}%  {}{}", "▮".repeat(v / 10), "▯".repeat(10 - v / 10))
        })
        .unwrap_or_else(|| "取得できません".into());
    let mem = mem
        .map(|(u, t)| format!("{u:.0} / {t:.0} GB"))
        .unwrap_or_else(|| "取得できません".into());
    let header = format!("CPU {cpu}   メモリ {mem}\n実行中 {running}件   順番待ち {queued}件\n");
    let footer = format!("\n3時間で終わった作業  {finished}件（失敗 {failed}）");
    let mut groups: Vec<_> = groups.into_iter().collect();
    groups.sort_by(|a, b| {
        b.1.running
            .cmp(&a.1.running)
            .then(b.1.queued.cmp(&a.1.queued))
            .then(a.0.cmp(&b.0))
    });
    let blocks: Vec<_> = groups
        .iter()
        .map(|(name, g)| {
            let name = clean(name, names, 32);
            let mut text = format!("\n■ {name}    実行中 {}", g.running);
            if g.queued > 0 {
                text.push_str(&format!(" ・ 待ち {}", g.queued));
            }
            text.push('\n');
            let mut titles: Vec<_> = g.titles.iter().collect();
            titles.sort_by(|a, b| b.1.cmp(a.1).then(a.0.cmp(b.0)));
            for (title, count) in titles.iter().take(3) {
                let title = clean(title, names, 48);
                text.push_str(&format!("    ・{title}"));
                if **count > 1 {
                    text.push_str(&format!("（{count}件）"));
                }
                text.push('\n');
            }
            let other: usize = titles.iter().skip(3).map(|(_, n)| **n).sum();
            if other > 0 {
                text.push_str(&format!("    ほか{other}件\n"));
            }
            text
        })
        .collect();
    for keep in (0..=blocks.len()).rev() {
        let mut body = header.clone();
        body.extend(blocks.iter().take(keep).map(String::as_str));
        if keep < blocks.len() {
            let rest = &groups[keep..];
            let each = if rest.iter().all(|(_, g)| g.running + g.queued == 1) {
                "（各1件）".to_owned()
            } else {
                format!(
                    "（計{}件）",
                    rest.iter()
                        .map(|(_, g)| g.running + g.queued)
                        .sum::<usize>()
                )
            };
            body.push_str(&format!("\nほか {}プロジェクト{each}\n", rest.len()));
        }
        body.push_str(&footer);
        // Include the timestamp prefix in the UTF-16 budget.
        if body.encode_utf16().count() + "作業状況  00:00\n".encode_utf16().count() <= MAX_CONTENT
        {
            return Ok(body);
        }
    }
    Err("board header exceeds message limit".into())
}

pub fn stamped(body: &str, now: f64) -> String {
    let time = DateTime::from_timestamp(now as i64, 0)
        .unwrap()
        .with_timezone(&FixedOffset::east_opt(9 * 3600).unwrap());
    format!("作業状況  {}\n{body}", time.format("%H:%M"))
}
