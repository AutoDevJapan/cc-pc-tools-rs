//! Finite machine listing. No free text, paths, history or job files are returned.
use serde_json::{json, Value};
use std::collections::{BTreeMap, BTreeSet};

pub const PAGE_LIMIT: usize = 100;
pub const LINK_LIMIT: usize = 64;
pub const RESPONSE_LIMIT: usize = 1024 * 1024;

#[derive(Default, Debug)]
pub struct Query {
    pub after: u64,
    pub id: Option<u64>,
    pub limit: usize,
    pub link_offset: usize,
    pub revision: Option<String>,
}
impl Query {
    pub fn parse(raw: &str) -> Result<Self, &'static str> {
        let mut q = Self {
            limit: PAGE_LIMIT,
            ..Self::default()
        };
        let mut used = BTreeSet::new();
        for pair in raw.split('&').filter(|pair| !pair.is_empty()) {
            let (key, value) = pair.split_once('=').ok_or("invalid listing query")?;
            if !used.insert(key) {
                return Err("duplicate listing parameter");
            }
            if key == "revision" {
                if value.len() != 64 || !value.bytes().all(|b| b.is_ascii_hexdigit()) {
                    return Err("invalid revision");
                }
                q.revision = Some(value.to_ascii_lowercase());
                continue;
            }
            if value.is_empty() || !value.bytes().all(|b| b.is_ascii_digit()) {
                return Err("invalid listing number");
            }
            let number = value.parse::<u64>().map_err(|_| "invalid listing number")?;
            match key {
                "after" => q.after = number,
                "id" if number > 0 => q.id = Some(number),
                "limit" if number > 0 && number <= PAGE_LIMIT as u64 => q.limit = number as usize,
                "link_offset" => {
                    q.link_offset = usize::try_from(number).map_err(|_| "invalid offset")?
                }
                _ => return Err("invalid listing parameter"),
            }
        }
        if q.id.is_some() && used.contains("after")
            || q.id.is_none() && used.contains("link_offset")
        {
            return Err("id cannot use after; link_offset requires id");
        }
        Ok(q)
    }
}

fn opaque(value: &str) -> String {
    ring::digest::digest(&ring::digest::SHA256, value.as_bytes())
        .as_ref()
        .iter()
        .map(|b| format!("{b:02x}"))
        .collect()
}
fn links(values: impl Iterator<Item = Value>, offset: usize) -> Value {
    let values: Vec<_> = values.collect();
    let end = offset.saturating_add(LINK_LIMIT).min(values.len());
    json!({"values":values.iter().skip(offset).take(LINK_LIMIT).collect::<Vec<_>>(),
        "total":values.len(),"nextOffset":(end < values.len()).then_some(end)})
}

pub fn project(db: &Value, q: &Query) -> Result<Value, &'static str> {
    if q.limit == 0 || q.limit > PAGE_LIMIT { return Err("invalid_query"); }
    let revision = opaque(&db.to_string());
    if q.revision.as_ref().is_some_and(|saved| *saved != revision) {
        return Err("listing_changed");
    }
    let source = db["items"].as_array().ok_or("invalid_store")?;
    let mut ordered = BTreeMap::new();
    for item in source {
        let id = item["id"]
            .as_u64()
            .filter(|id| *id > 0)
            .ok_or("invalid_store")?;
        if ordered.insert(id, item).is_some() {
            return Err("invalid_store");
        }
    }
    let selected: Vec<_> = ordered
        .iter()
        .filter(|(id, _)| q.id.map_or(**id > q.after, |wanted| **id == wanted))
        .take(q.limit + 1)
        .collect();
    if q.id.is_some() && selected.is_empty() {
        return Err("todo_not_found");
    }
    let has_more = selected.len() > q.limit;
    let mut rows = Vec::new();
    for (id, item) in selected.iter().take(q.limit) {
        let status = item["status"]
            .as_str()
            .filter(|s| matches!(*s, "todo" | "doing" | "block" | "wait" | "done"))
            .unwrap_or("unknown");
        // Mixed recovered job objects stay in the source; only string references
        // enter this listing, as opaque handles rather than unverified text.
        let jobs = item["jobs"]
            .as_array()
            .into_iter()
            .flatten()
            .filter_map(Value::as_str)
            .chain(item["job"].as_str())
            .chain(item["scheduler"]["job"].as_str())
            .filter(|id| !id.is_empty())
            .map(opaque)
            .collect::<BTreeSet<_>>();
        let dependencies = item["wait_on"]
            .as_array()
            .into_iter()
            .flatten()
            .filter_map(Value::as_u64)
            .filter(|id| *id > 0)
            .collect::<BTreeSet<_>>();
        let waits = links(dependencies.into_iter().map(Value::from), q.link_offset);
        let assigned = links(jobs.into_iter().map(Value::from), q.link_offset);
        rows.push(json!({"id":id,"status":status,
            "progress":item["progress"].as_u64().filter(|p| *p <= 100),
            "agent":item["agent"].as_str().filter(|a| !a.is_empty()).map(opaque),
            "jobs":assigned["values"],"jobsTotal":assigned["total"],"jobsNextOffset":assigned["nextOffset"],
            "wait_on":waits["values"],"waitOnTotal":waits["total"],"waitOnNextOffset":waits["nextOffset"]}));
    }
    let next = has_more.then(|| rows.last().unwrap()["id"].clone());
    let result = json!({"items":rows,"total":source.len(),"nextAfter":next,"revision":revision,
        "limit":q.limit,"linkOffset":q.link_offset,"protectionMode":"masked_metadata","metadataOnly":true});
    if result.to_string().len() > RESPONSE_LIMIT {
        return Err("listing_too_large");
    }
    Ok(result)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn maximum_page_and_links_stay_within_response_budget() {
        let db=json!({"items":(1..=101).map(|id| json!({"id":id,"status":"doing",
            "agent":"assigned","jobs":(0..64).map(|n|format!("job-{id}-{n}")).collect::<Vec<_>>(),
            "wait_on":(0..64).map(|n|u64::MAX-n).collect::<Vec<_>>()})).collect::<Vec<_>>()});
        let data=project(&db,&Query::parse("").unwrap()).unwrap();
        assert_eq!(data["items"].as_array().unwrap().len(),100);
        assert_eq!(data["nextAfter"],100);
        assert!(json!({"ok":true,"version":1,"data":data}).to_string().len()<RESPONSE_LIMIT);
        assert!(project(&db,&Query::default()).is_err());
    }
    #[test]
    fn listing_omits_large_history_and_private_text_without_changing_source() {
        let db = json!({"projects":{"private":"C:/private"},"items":[
            {"id":2,"status":"wait","agent":"secret-agent","wait_on":[1],"jobs":["job-secret",{"evidence":"preserve"}],"history":["x".repeat(17*1024*1024)]},
            {"id":1,"status":"doing","title":"private","spec":"private","note":"private"}]});
        let before = db.to_string();
        let first = project(&db, &Query::parse("limit=1").unwrap()).unwrap();
        assert_eq!(first["items"][0]["id"], 1);
        assert_eq!(first["nextAfter"], 1);
        let second = project(
            &db,
            &Query::parse(&format!(
                "after=1&revision={}",
                first["revision"].as_str().unwrap()
            ))
            .unwrap(),
        )
        .unwrap();
        assert_eq!(second["items"][0]["wait_on"], json!([1]));
        assert_eq!(second["items"][0]["jobsTotal"], 1);
        assert!(second["nextAfter"].is_null());
        assert!(second.to_string().len() < 2048);
        for secret in [
            "private",
            "secret-agent",
            "job-secret",
            "evidence",
            "history",
            "spec",
            "note",
        ] {
            assert!(!second.to_string().contains(secret));
        }
        assert_eq!(before, db.to_string());
        let mut changed = db.clone();
        changed["items"][0]["status"] = json!("doing");
        assert_eq!(
            project(
                &changed,
                &Query::parse(&format!("revision={}", first["revision"].as_str().unwrap()))
                    .unwrap()
            )
            .unwrap_err(),
            "listing_changed"
        );
    }
    #[test]
    fn listing_pages_links_and_rejects_ambiguous_queries_and_records() {
        let db = json!({"items":[{"id":1,"status":"wait","wait_on":(2..600).collect::<Vec<_>>(),"jobs":(0..600).map(|i|format!("job-{i}")).collect::<Vec<_>>()}]});
        let page = project(&db, &Query::parse("id=1").unwrap()).unwrap();
        assert_eq!(
            page["items"][0]["wait_on"].as_array().unwrap().len(),
            LINK_LIMIT
        );
        assert_eq!(page["items"][0]["waitOnNextOffset"], LINK_LIMIT);
        let next = project(&db, &Query::parse("id=1&link_offset=64").unwrap()).unwrap();
        assert_eq!(next["items"][0]["wait_on"][0], 66);
        for raw in [
            "limit=101",
            "id=0",
            "id=1&after=0",
            "link_offset=1",
            "limit=1&limit=2",
            "other=1",
            "after=-1",
            "revision=x",
        ] {
            assert!(Query::parse(raw).is_err(), "{raw}");
        }
        assert_eq!(
            project(&db, &Query::parse("id=9").unwrap()).unwrap_err(),
            "todo_not_found"
        );
        assert!(project(
            &json!({"items":[{"id":1},{"id":1}]}),
            &Query::parse("").unwrap()
        )
        .is_err());
    }
}
