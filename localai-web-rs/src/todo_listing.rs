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
    // Handles identify references; they do not claim a worker is alive.
    ring::digest::digest(
        &ring::digest::SHA256,
        format!("todo-list/job/v1:{value}").as_bytes(),
    )
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
    if q.limit == 0 || q.limit > PAGE_LIMIT {
        return Err("invalid_query");
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
    // Hash only the machine projection, streaming one row at a time. Changes
    // to notes/history do not make a finite traversal restart or serialize them.
    let mut context = ring::digest::Context::new(&ring::digest::SHA256);
    context.update(b"todo-list/metadata/v2");
    let mut projected = BTreeMap::new();
    for (id, item) in &ordered {
        let row = metadata(db, item);
        context.update(row.to_string().as_bytes());
        if q.id.map_or(*id > q.after, |wanted| *id == wanted) && projected.len() <= q.limit {
            projected.insert(*id, row);
        }
    }
    let revision: String = context
        .finish()
        .as_ref()
        .iter()
        .map(|b| format!("{b:02x}"))
        .collect();
    if q.revision.as_ref().is_some_and(|saved| *saved != revision) {
        return Err("listing_changed");
    }
    let selected: Vec<_> = projected.iter().take(q.limit + 1).collect();
    if q.id.is_some() && selected.is_empty() {
        return Err("todo_not_found");
    }
    let has_more = selected.len() > q.limit;
    let mut rows = Vec::new();
    for (_, item) in selected.iter().take(q.limit) {
        let mut row = (**item).clone();
        for (key, total, next) in [
            ("jobs", "jobsTotal", "jobsNextOffset"),
            ("wait_on", "waitOnTotal", "waitOnNextOffset"),
            ("waitPending", "waitPendingTotal", "waitPendingNextOffset"),
        ] {
            let page = links(item[key].as_array().unwrap().iter().cloned(), q.link_offset);
            row[key] = page["values"].clone();
            row[total] = page["total"].clone();
            row[next] = page["nextOffset"].clone();
        }
        rows.push(row);
    }
    let next = has_more.then(|| rows.last().unwrap()["id"].clone());
    let links_complete = rows.iter().all(|row| {
        [
            "jobsNextOffset",
            "waitOnNextOffset",
            "waitPendingNextOffset",
        ]
        .iter()
        .all(|key| row[*key].is_null())
    });
    let complete =
        !has_more && links_complete && q.link_offset == 0 && (q.id.is_some() || q.after == 0);
    let result = json!({"items":rows,"total":source.len(),"nextAfter":next,"revision":revision,
        "limit":q.limit,"after":q.after,"requestedId":q.id,"linkOffset":q.link_offset,"complete":complete,
        "protectionMode":"masked_metadata","metadataOnly":true});
    if json!({"ok":true,"version":1,"data":result})
        .to_string()
        .len()
        > RESPONSE_LIMIT
    {
        return Err("listing_too_large");
    }
    Ok(result)
}

fn metadata(db: &Value, item: &Value) -> Value {
    let mut unverified = cc_todo::job_refs::unverified_count(&item["jobs"]);
    let extra = [
        &item["scheduler"]["job"],
        &item["worker_receipt"]["job"],
        &item["job"],
    ];
    let active = extra
        .iter()
        .filter_map(|v| v.as_str())
        .find(|id| cc_todo::job_refs::valid_id(id));
    let jobs = cc_todo::job_refs::ids(&item["jobs"])
        .chain(
            extra
                .iter()
                .filter_map(|v| v.as_str())
                .filter(|id| cc_todo::job_refs::valid_id(id)),
        )
        .map(opaque)
        .collect::<BTreeSet<_>>();
    for v in extra {
        if !v.is_null()
            && v.as_str() != Some("")
            && !v.as_str().is_some_and(cc_todo::job_refs::valid_id)
        {
            unverified += 1;
        }
    }
    let status = item["status"]
        .as_str()
        .filter(|s| {
            matches!(
                *s,
                "todo" | "doing" | "block" | "wait" | "done" | "recovery_unverified"
            )
        })
        .unwrap_or("unknown");
    let phase = item["scheduler"]["phase"].as_str().map(|s| {
        if matches!(
            s,
            "running"
                | "waiting"
                | "retry"
                | "queued"
                | "preparing"
                | "prepared"
                | "blocked"
                | "finished"
                | "cancelled"
                | "capacity_exhausted"
        ) {
            s
        } else {
            "unknown"
        }
    });
    let valid_wait = |v: &Value| v.as_i64().is_some_and(|n| n > 0);
    let dependencies = item["wait_on"]
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(Value::as_i64)
        .filter(|n| *n > 0)
        .collect::<BTreeSet<_>>();
    let pending = cc_todo::pending_on(db, item)
        .into_iter()
        .filter(|n| *n > 0)
        .collect::<BTreeSet<_>>();
    let wait_unverified = if item["wait_on"].is_null() {
        0
    } else {
        item["wait_on"]
            .as_array()
            .map_or(1, |a| a.iter().filter(|v| !valid_wait(v)).count())
    };
    let ready = if item["wait_ready"].is_null() {
        Value::Null
    } else {
        let condition = item["wait_ready"]["condition"]
            .as_str()
            .filter(|c| matches!(*c, "chatgpt-plus" | "retry-time"))
            .unwrap_or("unknown");
        json!({"condition":condition,"notBefore":item["wait_ready"]["not_before"].as_u64()})
    };
    json!({"id":item["id"],"status":status,"progress":item["progress"].as_u64().filter(|p|*p<=100),
        "agent":active.map(opaque),"activeJob":active.map(opaque),"schedulerPhase":phase,
        "jobs":jobs,"jobsUnverified":unverified,"wait_on":dependencies,"waitPending":pending,
        "waitUnverified":wait_unverified,"waitReady":ready})
}

/// Validate the actual response against the request and emit only safe metadata.
pub fn validate(value: Value, q: &Query) -> Result<Value, &'static str> {
    let error = "console returned invalid or unprotected listing";
    let hash = |v: &Value| {
        v.as_str()
            .is_some_and(|s| s.len() == 64 && s.bytes().all(|b| b.is_ascii_hexdigit()))
    };
    let positive = |v: &Value| v.as_i64().is_some_and(|n| n > 0);
    let d = &value["data"];
    let items = d["items"]
        .as_array()
        .filter(|a| a.len() <= q.limit)
        .ok_or(error)?;
    if value["ok"] != true
        || value["version"] != 1
        || d["metadataOnly"] != true
        || d["protectionMode"] != "masked_metadata"
        || !hash(&d["revision"])
        || !d["total"].as_u64().is_some_and(|n| n >= items.len() as u64)
        || d["after"] != q.after
        || d["requestedId"] != json!(q.id)
        || d["limit"] != q.limit
        || d["linkOffset"] != q.link_offset
        || q.revision
            .as_ref()
            .is_some_and(|r| d["revision"].as_str() != Some(r.as_str()))
        || q.id
            .is_some_and(|id| items.len() != 1 || items[0]["id"] != id)
        || (q.id.is_none()
            && q.after == 0
            && d["nextAfter"].is_null()
            && d["total"].as_u64() != Some(items.len() as u64))
        || (!d["nextAfter"].is_null()
            && (q.id.is_some()
                || items.len() != q.limit
                || items.last().map(|r| &r["id"]) != Some(&d["nextAfter"])))
    {
        return Err(error);
    }
    let mut previous = q.after;
    let mut rows = Vec::new();
    for row in items {
        let id = row["id"].as_u64().filter(|n| *n > previous).ok_or(error)?;
        previous = id;
        if !row["status"].as_str().is_some_and(|s| {
            matches!(
                s,
                "todo" | "doing" | "block" | "wait" | "done" | "recovery_unverified" | "unknown"
            )
        }) || !(row["activeJob"].is_null() || hash(&row["activeJob"]))
            || row["agent"] != row["activeJob"]
            || !(row["schedulerPhase"].is_null()
                || row["schedulerPhase"].as_str().is_some_and(|s| {
                    matches!(
                        s,
                        "running"
                            | "waiting"
                            | "retry"
                            | "queued"
                            | "preparing"
                            | "prepared"
                            | "blocked"
                            | "finished"
                            | "cancelled"
                            | "capacity_exhausted"
                            | "unknown"
                    )
                }))
            || !(row["progress"].is_null() || row["progress"].as_u64().is_some_and(|n| n <= 100))
            || !row["jobsUnverified"].is_u64()
            || !row["waitUnverified"].is_u64()
        {
            return Err(error);
        }
        if !row["waitReady"].is_null()
            && !(row["waitReady"].is_object()
                && row["waitReady"]["condition"]
                    .as_str()
                    .is_some_and(|s| matches!(s, "chatgpt-plus" | "retry-time" | "unknown"))
                && (row["waitReady"]["notBefore"].is_null()
                    || row["waitReady"]["notBefore"].is_u64()))
        {
            return Err(error);
        }
        for (key, total, next, numeric) in [
            ("jobs", "jobsTotal", "jobsNextOffset", false),
            ("wait_on", "waitOnTotal", "waitOnNextOffset", true),
            (
                "waitPending",
                "waitPendingTotal",
                "waitPendingNextOffset",
                true,
            ),
        ] {
            let a = row[key].as_array().ok_or(error)?;
            let total = row[total].as_u64().ok_or(error)?;
            let offset = q.link_offset as u64;
            let count = total.saturating_sub(offset).min(LINK_LIMIT as u64);
            let end = offset.saturating_add(count);
            if a.len() as u64 != count
                || !a
                    .iter()
                    .all(|v| if numeric { positive(v) } else { hash(v) })
                || row[next] != json!((end < total).then_some(end))
            {
                return Err(error);
            }
        }
        let mut safe = json!({});
        for key in [
            "id",
            "status",
            "progress",
            "agent",
            "activeJob",
            "schedulerPhase",
            "jobs",
            "jobsTotal",
            "jobsNextOffset",
            "jobsUnverified",
            "wait_on",
            "waitOnTotal",
            "waitOnNextOffset",
            "waitPending",
            "waitPendingTotal",
            "waitPendingNextOffset",
            "waitUnverified",
        ] {
            safe[key] = row[key].clone();
        }
        safe["waitReady"] = if row["waitReady"].is_null() {
            Value::Null
        } else {
            json!({"condition":row["waitReady"]["condition"],"notBefore":row["waitReady"]["notBefore"]})
        };
        rows.push(safe);
    }
    let complete = d["nextAfter"].is_null()
        && q.link_offset == 0
        && (q.id.is_some() || q.after == 0)
        && items.iter().all(|row| {
            [
                "jobsNextOffset",
                "waitOnNextOffset",
                "waitPendingNextOffset",
            ]
            .iter()
            .all(|key| row[*key].is_null())
        });
    if d["complete"] != complete {
        return Err(error);
    }
    let mut safe = json!({"items":rows,"complete":complete});
    for key in [
        "total",
        "nextAfter",
        "revision",
        "limit",
        "after",
        "requestedId",
        "linkOffset",
        "protectionMode",
        "metadataOnly",
    ] {
        safe[key] = d[key].clone();
    }
    Ok(json!({"ok":true,"version":1,"data":safe}))
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn maximum_page_and_links_stay_within_response_budget() {
        let db = json!({"items":(1..=101).map(|id| json!({"id":id,"status":"doing",
            "agent":"assigned","jobs":(0..64).map(|n|format!("job-{id}-{n}")).collect::<Vec<_>>(),
            "wait_on":(0..64).map(|n|i64::MAX-n).collect::<Vec<_>>()})).collect::<Vec<_>>()});
        let data = project(&db, &Query::parse("").unwrap()).unwrap();
        assert_eq!(data["items"].as_array().unwrap().len(), 100);
        assert_eq!(data["nextAfter"], 100);
        assert!(json!({"ok":true,"version":1,"data":data}).to_string().len() < RESPONSE_LIMIT);
        assert!(project(&db, &Query::default()).is_err());
    }
    #[test]
    fn listing_omits_large_history_and_private_text_without_changing_source() {
        let db = json!({"projects":{"private":"C:/private"},"items":[
            {"id":2,"status":"wait","scheduler":{"job":"job-secret","phase":"running"},"wait_on":[1],"jobs":["job-secret",{"evidence":"preserve"}],"history":["x".repeat(17*1024*1024)]},
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
        assert_eq!(second["items"][0]["jobsUnverified"], 1);
        assert_eq!(
            second["items"][0]["activeJob"],
            second["items"][0]["jobs"][0]
        );
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
            assert!(!first.to_string().contains(secret));
        }
        assert_eq!(before, db.to_string());
        let mut changed = db.clone();
        changed["items"][0]["history"] = json!(["history updated"]);
        changed["items"][1]["note"] = json!("note updated");
        assert_eq!(
            project(&changed, &Query::parse("").unwrap()).unwrap()["revision"],
            first["revision"]
        );
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
    #[test]
    fn real_scheduler_fields_waits_and_unverified_references_match_shared_rules() {
        let db = json!({"items":[
            {"id":1,"status":"recovery_unverified","scheduler":{"job":"active-1","phase":"running"},"worker_receipt":{"job":"receipt-1"},"jobs":["old-1",{"id":"unverified-1"},"bad secret",""]},
            {"id":2,"status":"wait","wait_on":[1,3,4],"wait_ready":{"condition":"retry-time","not_before":12345,"message":"hidden"},"jobs":[]},
            {"id":3,"status":"done","jobs":[]},
            {"id":4,"status":"wait","wait_ready":{"condition":"chatgpt-plus"},"worker_receipt":{"job":"worker-4"},"jobs":[]},
            {"id":5,"status":"wait","wait_ready":{"condition":"free-secret","not_before":"private"},"jobs":[]} ]});
        let q = Query::parse("").unwrap();
        let data = project(&db, &q).unwrap();
        let rows = data["items"].as_array().unwrap();
        assert_eq!(rows[0]["status"], "recovery_unverified");
        assert_eq!(rows[0]["schedulerPhase"], "running");
        assert_eq!(rows[0]["activeJob"], opaque("active-1"));
        assert_eq!(
            rows[0]["jobsUnverified"],
            cc_todo::job_refs::unverified_count(&db["items"][0]["jobs"])
        );
        assert_eq!(rows[0]["jobsTotal"], 3);
        assert_eq!(
            rows[1]["waitPending"],
            json!(cc_todo::pending_on(&db, &db["items"][1]))
        );
        assert_eq!(rows[1]["waitPending"], json!([1, 4]));
        assert_eq!(
            rows[1]["waitReady"],
            json!({"condition":"retry-time","notBefore":12345})
        );
        assert_eq!(rows[3]["activeJob"], opaque("worker-4"));
        assert_eq!(rows[4]["waitReady"]["condition"], "unknown");
        assert!(!data.to_string().contains("hidden"));
        assert!(!data.to_string().contains("free-secret"));
        assert!(data["complete"].as_bool().unwrap());
        let envelope = json!({"ok":true,"version":1,"data":data});
        assert!(validate(envelope.clone(), &q).is_ok());
        for (key, bad) in [
            ("total", json!(0)),
            ("nextAfter", json!(999)),
            ("linkOffset", json!(1)),
            ("complete", json!(false)),
        ] {
            let mut broken = envelope.clone();
            broken["data"][key] = bad;
            assert!(validate(broken, &q).is_err(), "{key}");
        }
        assert!(validate(envelope, &Query::parse("id=1").unwrap()).is_err());
    }
}
