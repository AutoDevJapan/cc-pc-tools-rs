//! Owner's department order. Only explicit ownership and dependency edges affect rank.
use serde_json::Value;
use std::collections::BTreeMap;

pub const DEFAULT: u32 = 100;
pub fn project_rank(project: &str) -> u32 {
    let p = project.to_lowercase();
    let normalized: String = p
        .replace('α', "alpha")
        .chars()
        .filter(|c| !matches!(c, '-' | '_' | ' ' | ':'))
        .collect();
    // Display-name changes must not lower a previously assigned product priority.
    match normalized.as_str() {
        "ycchp" | "arps" | "socialpilot" => return 10,
        "civ6" | "civ6banseiki" | "banseiki" | "万世紀" | "万世記" => return 20,
        "skills" | "skill" => return 50,
        "goddmcpalpha" => return 60,
        "goddpress" => return 61,
        "goddao" => return 62,
        "godd13a13e1" | "13a13e1" => return 63,
        "goddrosetta" | "goddrosseta" => return 64,
        "goddgenesis" | "genesis" => return 65,
        "goddvirtualstreamer" | "virtualstreamer" => return 66,
        "localaiconcole" | "localaiconsole" | "localaiwebrs" | "opencodex" => return 30,
        _ => {}
    }
    match p.as_str() {
        "arps" | "socialpilot" | "social pilot" | "クライアント案件" => 10,
        "civ6" | "civ6-banseiki" | "banseiki" | "万世紀" => 20,
        "skills" | "skill" => 50,
        "godd-mcp-alpha" => 60,
        "godd-press" => 61,
        "godd-ao" => 62,
        "13a13e1" | "godd-13a13e1" => 63,
        "godd-rosetta" | "godd-rosseta" => 64,
        "godd-genesis" | "genesis" => 65,
        "godd-virtualstreamer" | "virtualstreamer" => 66,
        _ if p.starts_with("godd-press-") => 61,
        _ if p.starts_with("godd") => 67,
        _ if p.starts_with("cc-")
            || p.starts_with("opencodex")
            || [
                "tools",
                "pcツール",
                "pcの道具",
                "常駐",
                "makhmeto",
                "localai-web-rs",
                "chatgpt-web",
                "chrome-bookmark-worker",
            ]
            .contains(&p.as_str()) =>
        {
            30
        }
        _ if [
            "youtube",
            "health-channel",
            "adj-daily-shorts",
            "x-skill",
            "xブックマーク",
        ]
        .iter()
        .any(|s| p.contains(s)) =>
        {
            40
        }
        _ => DEFAULT,
    }
}
pub fn department_rank(department: &str) -> Option<u32> {
    match department {
        "受託部" => Some(10),
        "万世紀" | "万世記" | "ゲーム部" => Some(20),
        "情報システム部" => Some(30),
        "発信部" => Some(40),
        "Skills" => Some(50),
        "GoDD事業部" => Some(67),
        "自社サービス部" | "自社サービス" => Some(100),
        _ => None,
    }
}
fn rank(value: &Value) -> Option<u32> {
    value
        .as_u64()
        .and_then(|n| u32::try_from(n).ok())
        .filter(|n| matches!(*n, 10 | 20 | 30 | 40 | 50 | 60..=67 | 100))
}
pub fn item_rank(item: &Value, policy: &Value) -> u32 {
    let id = item["id"]
        .as_u64()
        .map(|n| n.to_string())
        .unwrap_or_default();
    let ownership = &policy["items"][&id];
    if let Some(n) = rank(ownership) {
        return n;
    }
    let project = ownership["project"]
        .as_str()
        .or_else(|| item["owner_project"].as_str())
        .or_else(|| item["project"].as_str())
        .unwrap_or("");
    if let Some(n) = ownership["department"]
        .as_str()
        .or_else(|| item["department"].as_str())
        .and_then(department_rank)
    {
        // Retain the owner's product order within GoDD.
        return if n == 67 {
            let rank = project_rank(project);
            if (60..=67).contains(&rank) { rank } else { 67 }
        } else {
            n
        };
    }
    policy["projects"]
        .as_object()
        .or_else(|| policy.as_object())
        .and_then(|map| {
            map.iter()
                .find(|(key, _)| key.eq_ignore_ascii_case(project))
        })
        .and_then(|(_, value)| rank(value))
        .unwrap_or_else(|| project_rank(project))
}
/// Propagate only unresolved, recorded prerequisites, including transitive waits.
pub fn todo_ranks(db: &Value, policy: &Value) -> BTreeMap<u64, u32> {
    let items: Vec<_> = db["items"].as_array().into_iter().flatten().collect();
    let mut ranks: BTreeMap<_, _> = items
        .iter()
        .filter_map(|i| i["id"].as_u64().map(|id| (id, item_rank(i, policy))))
        .collect();
    loop {
        let mut changed = false;
        for item in &items {
            if item["status"] == "done"
                || item["status"] == "block"
                || !item["wait_ready"].is_null()
            {
                continue;
            }
            let Some(parent) = item["id"].as_u64().and_then(|id| ranks.get(&id).copied()) else {
                continue;
            };
            for id in item["wait_on"]
                .as_array()
                .into_iter()
                .flatten()
                .chain(item["work_wait_on"].as_array().into_iter().flatten())
                .filter_map(Value::as_u64)
            {
                if items.iter().any(|i| i["id"] == id && i["status"] != "done")
                    && let Some(child) = ranks.get_mut(&id)
                    && parent < *child
                {
                    *child = parent;
                    changed = true;
                }
            }
        }
        if !changed {
            return ranks;
        }
    }
}
pub fn linked(item: &Value, job: &Value) -> bool {
    (item["id"].is_number()
        && (item["id"] == job["todo_id"]
            || job["related_todo_ids"]
                .as_array()
                .is_some_and(|a| a.contains(&item["id"]))))
        || (job["id"].is_string()
            && (item["jobs"]
                .as_array()
                .is_some_and(|a| a.contains(&job["id"]))
                || item["scheduler"]["job"] == job["id"]))
}
pub fn job_ranks(jobs: &[Value], db: &Value, policy: &Value) -> BTreeMap<String, u32> {
    let todos = todo_ranks(db, policy);
    let mut ranks: BTreeMap<_, _> = jobs
        .iter()
        .filter_map(|j| {
            j["id"].as_str().map(|id| {
                let mut current = j;
                let mut seen = std::collections::BTreeSet::new();
                let rank = loop {
                    let owned = db["items"].as_array().into_iter().flatten()
                        .filter(|i| linked(i,current))
                        .filter_map(|i| i["id"].as_u64().and_then(|id| todos.get(&id).copied())).min();
                    if let Some(rank) = owned { break rank; }
                    if current["subwork"].is_object() { break item_rank(current,policy); }
                    if !seen.insert(current["id"].as_str().unwrap_or("")) { break item_rank(j,policy); }
                    let parent = current["parent_job_id"].as_str().and_then(|id| jobs.iter().find(|parent| parent["id"].as_str() == Some(id)
                        && ["queued","starting","running"].contains(&parent["status"].as_str().unwrap_or(""))
                        && parent["cancel_requested"] != true));
                    let Some(parent) = parent else { break item_rank(current,policy); };
                    current=parent;
                };
                (id.to_owned(),rank)
            })
        })
        .collect();
    loop {
        let mut changed = false;
        for job in jobs {
            // Independent subwork is deliberately not a prerequisite.
            if job["subwork"].is_object()
                || !["queued", "starting", "running"]
                    .contains(&job["status"].as_str().unwrap_or(""))
            {
                continue;
            }
            let Some(parent) = job["parent_job_id"]
                .as_str()
                .filter(|id| {
                    jobs.iter().any(|parent| {
                        parent["id"].as_str() == Some(*id)
                            && ["queued", "starting", "running"]
                                .contains(&parent["status"].as_str().unwrap_or(""))
                            && parent["cancel_requested"] != true
                    })
                })
                .and_then(|id| ranks.get(id).copied())
            else {
                continue;
            };
            if let Some(child) = job["id"].as_str().and_then(|id| ranks.get_mut(id))
                && parent < *child
            {
                *child = parent;
                changed = true;
            }
        }
        if !changed {
            return ranks;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    #[test]
    fn order_aliases_and_unknown() {
        let projects = [
            "ARPS",
            "civ6",
            "localai-web-rs",
            "youtube",
            "Skills",
            "GoDD-MCP-Alpha",
            "GoDD-Press",
            "GoDD-AO",
            "13a13e1",
            "GoDD-Rosseta",
            "Genesis",
            "VirtualStreamer",
            "unknown",
        ];
        assert_eq!(
            projects.map(project_rank),
            [10, 20, 30, 40, 50, 60, 61, 62, 63, 64, 65, 66, 100]
        );
        assert_eq!(project_rank("SocialPilot"), 10);
        assert_eq!(project_rank("YCC-HP"), 10);
        assert_eq!(project_rank("万世記"), 20);
        assert_eq!(project_rank("local-ai-concole"), 30);
        assert_eq!(project_rank("GoDD MCP α"), 60);
        assert_eq!(project_rank("GoDD Rosetta"), 64);
        assert_eq!(project_rank("GoDD Virtual Streamer"), 66);
        assert_eq!(
            item_rank(&json!({"project":"CIV6"}), &json!({"civ6":20})),
            20
        );
        assert_eq!(item_rank(&json!({"project":"x"}), &json!({"x":0})), 100);
        assert_eq!(
            item_rank(&json!({"project":"civ6"}), &json!({"civ6":1})),
            20
        );
    }
    #[test]
    fn mixed_ownership_transitive_waits_and_completed_edges() {
        let db = json!({"items":[{"id":1,"project":"ADJ","status":"wait","wait_on":[2]}, {"id":2,"project":"other","status":"wait","wait_on":[3]}, {"id":3,"project":"other","status":"todo"}, {"id":4,"project":"ARPS","status":"done","wait_on":[5]}, {"id":5,"project":"other","status":"todo"}]});
        let ranks = todo_ranks(&db, &json!({"items":{"1":{"project":"GoDD-Press"}}}));
        assert_eq!(
            (ranks[&1], ranks[&2], ranks[&3], ranks[&5]),
            (61, 61, 61, 100)
        );
        assert_eq!(
            item_rank(
                &json!({"id":9,"project":"ADJ"}),
                &json!({"items":{"9":{"department":"情報システム部"}}})
            ),
            30
        );
    }
    #[test]
    fn children_inherit_but_independent_subwork_does_not() {
        let jobs = vec![
            json!({"id":"p","project":"ARPS","status":"running"}),
            json!({"id":"c","parent_job_id":"p","status":"queued"}),
            json!({"id":"g","parent_job_id":"c","status":"queued"}),
            json!({"id":"s","parent_job_id":"p","subwork":{},"status":"queued"}),
        ];
        let ranks = job_ranks(&jobs, &json!({}), &json!({}));
        assert_eq!((ranks["c"], ranks["g"], ranks["s"]), (10, 10, 100));
        let mut finished = jobs.clone();
        finished[0]["status"] = json!("succeeded");
        let ranks = job_ranks(&finished, &json!({}), &json!({}));
        assert_eq!((ranks["c"], ranks["g"]), (100, 100));
    }
    #[test]
    fn helper_directory_does_not_promote_a_product_review_to_it() {
        let jobs=vec![json!({"id":"p","todo_id":1,"project":"tools","status":"running"}),
            json!({"id":"c","parent_job_id":"p","project":"cc-ask","status":"queued"}),
            json!({"id":"own","parent_job_id":"p","todo_id":2,"project":"cc-ask","status":"queued"}),
            json!({"id":"independent","parent_job_id":"p","subwork":{},"project":"cc-ask","status":"queued"})];
        let db=json!({"items":[{"id":1,"project":"Re:X","status":"doing"},{"id":2,"project":"SocialPilot","status":"todo"}]});
        let ranks=job_ranks(&jobs,&db,&json!({}));
        assert_eq!((ranks["p"],ranks["c"],ranks["own"],ranks["independent"]),(100,100,10,30));
    }
}
