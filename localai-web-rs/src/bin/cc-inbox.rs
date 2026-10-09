//! Read-only inbox adapter. Reuses the installed Rust Discord transport; no Python dependency.
use cc_ask::core::{Effects, RealEffects};
use serde_json::{Value, json};
use std::{
    collections::{BTreeMap, BTreeSet},
    path::{Path, PathBuf},
    time::{SystemTime, UNIX_EPOCH},
};
type Result<T> = std::result::Result<T, String>;
const FORUM: &str = "1554771264298819675";
fn safe_text(text: impl AsRef<str>) -> String {
    static TOKENS: std::sync::OnceLock<regex::Regex> = std::sync::OnceLock::new();
    let tokens = TOKENS.get_or_init(|| {
        regex::Regex::new(
            r"[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{6,}\.[A-Za-z0-9_-]{20,}|mfa\.[A-Za-z0-9_-]{40,}",
        )
        .expect("fixed token pattern")
    });
    tokens
        .replace_all(&cc_ask::core::safe_text(text), "[秘密]")
        .into_owned()
}
fn s<'a>(v: &'a Value, k: &str) -> &'a str {
    v[k].as_str().unwrap_or("")
}
fn id(v: &Value) -> Result<u64> {
    v.as_str()
        .ok_or("invalid id")?
        .parse()
        .map_err(|_| "invalid id".into())
}
fn array(v: &Value) -> Result<&Vec<Value>> {
    v.as_array().ok_or_else(|| "invalid array".into())
}
fn encode(s: &str) -> String {
    s.bytes()
        .map(|b| {
            if b.is_ascii_alphanumeric() || b"-_.~".contains(&b) {
                (b as char).to_string()
            } else {
                format!("%{b:02X}")
            }
        })
        .collect()
}
fn millis(s: &str) -> Result<i64> {
    chrono::DateTime::parse_from_rfc3339(s)
        .map(|d| d.timestamp_millis())
        .map_err(|_| "invalid archive timestamp".into())
}
fn archives(
    get: &mut impl FnMut(&str) -> Result<Value>,
    cid: &str,
    cutoff: i64,
) -> Result<Vec<Value>> {
    let mut before = String::new();
    let mut out = Vec::new();
    let mut previous_oldest = None;
    let mut seen = BTreeSet::new();
    loop {
        let suffix = if before.is_empty() {
            String::new()
        } else {
            format!("&before={}", encode(&before))
        };
        let page = get(&format!(
            "/channels/{cid}/threads/archived/public?limit=100{suffix}"
        ))?;
        let threads = array(&page["threads"])?;
        for t in threads {
            if millis(s(&t["thread_metadata"], "archive_timestamp"))? > cutoff
                && seen.insert(id(&t["id"])?)
            {
                out.push(t.clone());
            }
        }
        if page["has_more"] != true {
            break;
        }
        let oldest = s(
            &threads.last().ok_or("empty archive page")?["thread_metadata"],
            "archive_timestamp",
        );
        let oldest_ms = millis(oldest)?;
        if oldest_ms <= cutoff {
            break;
        }
        if previous_oldest.is_some_and(|previous| oldest_ms >= previous) {
            return Err("ambiguous archive boundary; checkpoint retained".into());
        }
        previous_oldest = Some(oldest_ms);
        // Re-read the boundary timestamp. IDs remove duplicates; never skip tied timestamps.
        before = chrono::DateTime::from_timestamp_millis(oldest_ms + 1)
            .ok_or("archive time out of range")?
            .to_rfc3339_opts(chrono::SecondsFormat::Millis, true);
    }
    Ok(out)
}
fn messages(
    get: &mut impl FnMut(&str) -> Result<Value>,
    cid: &str,
    last: u64,
) -> Result<Vec<Value>> {
    let mut before = None;
    let mut out = Vec::new();
    loop {
        let suffix = before.map(|n| format!("&before={n}")).unwrap_or_default();
        let page = get(&format!("/channels/{cid}/messages?limit=100{suffix}"))?;
        let page = array(&page)?;
        for m in page {
            if id(&m["id"])? > last {
                out.push(m.clone());
            }
        }
        if page.len() < 100 {
            break;
        }
        let oldest = page
            .iter()
            .map(|m| id(&m["id"]))
            .collect::<Result<Vec<_>>>()?
            .into_iter()
            .min()
            .ok_or("empty message page")?;
        if oldest <= last {
            break;
        }
        if before.is_some_and(|n| oldest >= n) {
            return Err("message cursor did not advance".into());
        }
        before = Some(oldest);
    }
    Ok(out)
}
fn verified(
    get: &mut impl FnMut(&str) -> Result<Value>,
    cfg: &Value,
    last: u64,
    upper: u64,
) -> Result<Vec<Value>> {
    let guild = id(&cfg["guild_id"])?;
    let owner = id(&cfg["owner_user_id"])?;
    let channels = get(&format!("/guilds/{guild}/channels"))?;
    let active = get(&format!("/guilds/{guild}/threads/active"))?;
    let mut threads = array(&active["threads"])?.clone();
    // One second of overlap handles rounded archive timestamps; message IDs deduplicate it.
    let cutoff = ((last >> 22) + 1_420_070_400_000) as i64 - 1000;
    for c in array(&channels)? {
        if matches!(c["type"].as_u64(), Some(0 | 5 | 15 | 16)) && s(c, "id") != FORUM {
            threads.extend(archives(get, s(c, "id"), cutoff)?);
        }
    }
    let names: BTreeMap<_, _> = array(&channels)?
        .iter()
        .map(|c| (s(c, "id").to_owned(), s(c, "name").to_owned()))
        .collect();
    let mut places: BTreeMap<String, Value> = array(&channels)?
        .iter()
        .filter(|c| matches!(c["type"].as_u64(), Some(0 | 5)))
        .map(|c| (s(c, "id").into(), c.clone()))
        .collect();
    for t in threads {
        let newest = id(&t["last_message_id"])
            .unwrap_or(id(&t["id"])?)
            .max(id(&t["id"])?);
        if s(&t, "parent_id") != FORUM && newest > last {
            places.insert(s(&t, "id").into(), t);
        }
    }
    let mut found = BTreeMap::new();
    for (cid, place) in places {
        // Any failure aborts the entire batch, so another channel cannot hide unread messages.
        for m in messages(get, &cid, last)? {
            let mid = id(&m["id"])?;
            if mid >= upper || id(&m["author"]["id"])? != owner {
                continue;
            }
            let parent = s(&place, "parent_id");
            let label = if place.get("thread_metadata").is_some() {
                format!(
                    "{} > {}",
                    names.get(parent).map(String::as_str).unwrap_or("?"),
                    s(&place, "name")
                )
            } else {
                format!("#{}", s(&place, "name"))
            };
            let files = m["attachments"]
                .as_array()
                .map(|a| a.iter().map(|a| s(a, "url")).collect::<Vec<_>>().join(" "))
                .unwrap_or_default();
            let text = format!(
                "{}{}{}",
                s(&m, "content"),
                if files.is_empty() { "" } else { " " },
                files
            );
            found.insert(mid,json!({"id":mid.to_string(),"channel":cid,"parent_id":parent,"where":safe_text(label),"at":s(&m,"timestamp"),"text":safe_text(text)}));
        }
    }
    Ok(found.into_values().collect())
}
fn batch(
    mark: &Path,
    cfg: &Value,
    clock: impl FnOnce() -> u64,
    get: &mut impl FnMut(&str) -> Result<Value>,
) -> Result<Vec<Value>> {
    let last = std::fs::read_to_string(mark)
        .map_err(|_| "cannot read staged checkpoint")?
        .trim()
        .parse::<u64>()
        .map_err(|_| "invalid checkpoint")?;
    let upper = clock(); // Freeze before all network reads; later arrivals belong to the next batch.
    let rows = verified(get, cfg, last, upper)?;
    std::fs::write(mark, last.max(upper.saturating_sub(1)).to_string())
        .map_err(|_| "cannot write staged checkpoint")?;
    Ok(rows)
}
fn run() -> Result<()> {
    let args: Vec<_> = std::env::args().skip(1).collect();
    if args.len() != 2 || args[0] != "--state-file" {
        return Err("use --state-file <staged checkpoint>".into());
    }
    let home = std::env::var_os("USERPROFILE")
        .or_else(|| std::env::var_os("HOME"))
        .map(PathBuf::from)
        .ok_or("home unavailable")?;
    let config = std::env::var_os("CC_ASK_CONFIG")
        .map(PathBuf::from)
        .unwrap_or_else(|| home.join(".godd/discord-notify.json"));
    let cfg: Value =
        serde_json::from_slice(&std::fs::read(config).map_err(|_| "config unavailable")?)
            .map_err(|_| "invalid config")?;
    let mut effects = RealEffects;
    let mut get = |path: &str| {
        effects.http(
            "GET",
            &format!("https://discord.com/api/v10{path}"),
            None,
            true,
        )
    };
    let rows = batch(
        Path::new(&args[1]),
        &cfg,
        || {
            let now = SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap_or_default()
                .as_millis() as u64;
            now.saturating_sub(1_420_070_400_000) << 22
        },
        &mut get,
    )?;
    for row in rows {
        println!("{row}");
    }
    Ok(())
}
fn main() {
    if let Err(error) = run() {
        eprintln!(
            "Discord inbox verification failed; checkpoint is not committed: {}",
            safe_text(error)
        );
        std::process::exit(1);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn bare_discord_token_is_not_output() {
        assert_eq!(
            safe_text(
                "token ABCDEFGHIJKLMNOPQRSTUVWXYZ12.abcdef.abcdefghijklmnopqrstuvwxyz01234567890"
            ),
            "token [秘密]"
        );
    }
    #[test]
    fn multiple_message_pages() {
        let mut calls = 0;
        let mut get = |p: &str| {
            calls += 1;
            Ok(json!(
                (if p.contains("before=102") {
                    2..102
                } else {
                    102..202
                })
                .rev()
                .map(|i| json!({"id":i.to_string()}))
                .collect::<Vec<_>>()
            ))
        };
        let rows = messages(&mut get, "10", 50).unwrap();
        assert_eq!(rows.len(), 151);
        assert_eq!(calls, 2);
    }
    #[test]
    fn multiple_archive_pages() {
        let base = millis("2026-10-01T20:00:00Z").unwrap();
        let mut calls = 0;
        let mut get = |p: &str| {
            calls += 1;
            if calls == 2 {
                assert!(p.contains("before=2026-10-01T19%3A58%3A21.001Z"));
            }
            Ok(
                json!({"threads":(0..100).map(|i|{let offset=if calls==1{i}else{i+99};json!({"id":offset.to_string(),"thread_metadata":{"archive_timestamp":chrono::DateTime::from_timestamp_millis(base-offset*1000).unwrap().to_rfc3339()}})}).collect::<Vec<_>>(),"has_more":calls==1}),
            )
        };
        assert_eq!(archives(&mut get, "10", 0).unwrap().len(), 199);
    }
    #[test]
    fn full_page_at_same_archive_timestamp_is_rejected() {
        let mut calls = 0;
        let mut get = |p: &str| {
            calls += 1;
            if calls == 2 {
                assert!(p.contains("before=2026-10-01T20%3A00%3A00.001Z"));
            }
            Ok(
                json!({"threads":(0..100).map(|i|json!({"id":i.to_string(),"thread_metadata":{"archive_timestamp":"2026-10-01T20:00:00Z"}})).collect::<Vec<_>>(),"has_more":true}),
            )
        };
        assert!(
            archives(&mut get, "10", 0)
                .unwrap_err()
                .contains("ambiguous")
        );
        assert_eq!(calls, 2);
    }
    fn cfg() -> Value {
        json!({"guild_id":"1","owner_user_id":"2"})
    }
    fn channel() -> Value {
        json!([{"id":"10","name":"chat","type":0}])
    }
    #[test]
    fn partial_failure_aborts() {
        let mut get = |p: &str| {
            if p.ends_with("/channels") {
                Ok(channel())
            } else if p.contains("threads") {
                Ok(json!({"threads":[],"has_more":false}))
            } else {
                Err("temporary failure".into())
            }
        };
        assert!(verified(&mut get, &cfg(), 10, 100).is_err());
    }
    #[test]
    fn upper_defers_racing_messages() {
        let mut get = |p: &str| {
            Ok(if p.ends_with("/channels") {
                channel()
            } else if p.contains("threads") {
                json!({"threads":[],"has_more":false})
            } else {
                json!([{"id":"20","author":{"id":"2"},"content":"依頼"},{"id":"30","author":{"id":"2"},"content":"次回"}])
            })
        };
        let rows = verified(&mut get, &cfg(), 10, 30).unwrap();
        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0]["id"], "20");
    }
    #[test]
    fn rounded_archive_boundary_is_read() {
        let stamp = millis("2026-10-01T12:00:00Z").unwrap() as u64;
        let last = (stamp + 500 - 1_420_070_400_000) << 22;
        let mut seen = false;
        let mut get = |p: &str| {
            if p.ends_with("/channels") {
                Ok(channel())
            } else if p.ends_with("/active") {
                Ok(json!({"threads":[]}))
            } else if p.contains("/archived/") {
                Ok(
                    json!({"threads":[{"id":"11","name":"thread","parent_id":"10","last_message_id":(last+1).to_string(),"thread_metadata":{"archive_timestamp":"2026-10-01T12:00:00Z"}}],"has_more":false}),
                )
            } else {
                seen |= p.contains("/channels/11/");
                Ok(json!([{"id":(last+1).to_string(),"author":{"id":"2"},"content":"依頼"}]))
            }
        };
        assert!(
            !verified(&mut get, &cfg(), last, last + 10)
                .unwrap()
                .is_empty()
        );
        assert!(seen);
    }
    #[test]
    fn batch_freezes_clock_before_network_and_advances_idle_mark() {
        let root = std::env::temp_dir();
        let mark = root.join(format!("inbox-test-{}.mark", std::process::id()));
        std::fs::write(&mark, b"10").unwrap();
        let clock = std::cell::Cell::new(30);
        let mut get = |p: &str| {
            clock.set(50);
            Ok(if p.ends_with("/channels") {
                channel()
            } else if p.contains("threads") {
                json!({"threads":[],"has_more":false})
            } else {
                json!([{"id":"40","author":{"id":"2"},"content":"次回"}])
            })
        };
        let rows = batch(&mark, &cfg(), || clock.get(), &mut get).unwrap();
        assert!(rows.is_empty());
        assert_eq!(std::fs::read_to_string(&mark).unwrap(), "29");
        std::fs::remove_file(mark).unwrap();
    }
    #[test]
    fn failed_network_batch_never_advances_the_checkpoint() {
        let mark = std::env::temp_dir().join(format!("inbox-failure-{}.mark", std::process::id()));
        std::fs::write(&mark, b"10").unwrap();
        let mut get = |_: &str| Err("network unavailable".into());
        assert!(batch(&mark, &cfg(), || 30, &mut get).is_err());
        assert_eq!(std::fs::read_to_string(&mark).unwrap(), "10");
        std::fs::remove_file(mark).unwrap();
    }
}
