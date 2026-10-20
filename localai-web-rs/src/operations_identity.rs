//! One indexed observation supplies execution, history and privacy separately.
use super::*;
use cc_scheduler::product_identity::{self as identity, Resolution};

pub(super) fn resolve(row: &Value, policy: &Value) -> Resolution {
    identity::resolve(row, policy)
}
pub(super) fn target_for(resolution: Resolution, row: &Value, fallback: String) -> String {
    match resolution {
        Resolution::Unknown => format!(
            "製品未確認（登録 {}）",
            safe_id("record", &row["id"].to_string())
        ),
        resolution => resolution
            .product()
            .map(|p| p.name.to_owned())
            .unwrap_or(fallback),
    }
}
pub(super) fn code_product(row: &Value) -> Option<&'static str> {
    catalog::find(text(row, "project")).map(|p| p.name)
}

pub(super) struct Associations<'a> {
    assigned: BTreeMap<u64, Vec<&'a Value>>,
    raw: BTreeMap<u64, Vec<&'a Value>>,
    history: BTreeMap<u64, Vec<&'a Value>>,
    targets: BTreeMap<String, Resolution>,
    sensitive: BTreeSet<String>,
}
impl<'a> Associations<'a> {
    pub(super) fn record(
        &self,
        row: &Value,
        current: Option<&Value>,
        policy: &Value,
    ) -> Resolution {
        let base = resolve(row, policy);
        if let Some(target) = current.map(|job| self.target(job)).filter(|target| {
            base == Resolution::Other
                && matches!(
                    target,
                    Resolution::Verified(_) | Resolution::OwnerDeclared(_)
                )
        }) {
            return target;
        }
        base
    }
    pub(super) fn assigned(&self, id: u64) -> &[&'a Value] {
        self.assigned.get(&id).map(Vec::as_slice).unwrap_or(&[])
    }
    pub(super) fn raw(&self, id: u64) -> &[&'a Value] {
        self.raw.get(&id).map(Vec::as_slice).unwrap_or(&[])
    }
    pub(super) fn history(&self, id: u64) -> &[&'a Value] {
        self.history.get(&id).map(Vec::as_slice).unwrap_or(&[])
    }
    pub(super) fn target(&self, job: &Value) -> Resolution {
        self.targets
            .get(text(job, "id"))
            .copied()
            .unwrap_or(Resolution::Unknown)
    }
    pub(super) fn sensitive(&self, job: &Value) -> bool {
        self.sensitive.contains(text(job, "id"))
    }
}

pub(super) fn priority(
    row: &Value,
    identity: Resolution,
    inherited: Option<u32>,
    policy: &Value,
) -> Option<u32> {
    if let Some(product) = identity
        .product()
        .filter(|_| resolve(row, policy) == Resolution::Other)
    {
        return Some(
            inherited
                .filter(|rank| *rank < cc_scheduler::priority::item_rank(row, policy))
                .unwrap_or(product.rank),
        );
    }
    inherited
}

/// Index recorded IDs once, preserving all privacy edges while selecting only
/// one primary execution edge. Never walk paths, prompts or task prose.
pub(super) fn assignments<'a>(
    items: &'a [Value],
    jobs: &'a [Value],
    policy: &Value,
) -> Associations<'a> {
    let by_id: BTreeMap<_, _> = items
        .iter()
        .filter_map(|row| Some((row["id"].as_u64()?, row)))
        .collect();
    let identities: BTreeMap<_, _> = by_id
        .iter()
        .map(|(id, row)| (*id, resolve(row, policy)))
        .collect();
    let mut refs = BTreeMap::<String, BTreeSet<u64>>::new();
    for (id, item) in &by_id {
        for key in item["jobs"]
            .as_array()
            .into_iter()
            .flatten()
            .chain([&item["job"], &item["scheduler"]["job"]])
        {
            if let Some(key) = key.as_str().filter(|key| !key.is_empty()) {
                refs.entry(key.to_owned()).or_default().insert(*id);
            }
        }
    }
    let mut out = Associations {
        assigned: BTreeMap::new(),
        raw: BTreeMap::new(),
        history: BTreeMap::new(),
        targets: BTreeMap::new(),
        sensitive: BTreeSet::new(),
    };
    for job in jobs {
        let id = text(job, "id");
        let mut linked = BTreeSet::new();
        for key in ["id", "thread_id", "resume_session"] {
            if let Some(ids) = refs.get(text(job, key)) {
                linked.extend(ids);
            }
        }
        let direct = job["todo_id"].as_u64();
        if let Some(id) = direct.filter(|id| by_id.contains_key(id)) {
            linked.insert(id);
        }
        let mut candidates: Vec<_> = linked
            .iter()
            .copied()
            .filter(|id| direct.is_none_or(|direct| direct == *id))
            .collect();
        if candidates
            .iter()
            .any(|id| by_id[id]["scheduler"]["job"] == job["id"])
        {
            candidates.retain(|id| by_id[id]["scheduler"]["job"] == job["id"]);
        }
        let target = identity::job_target(
            job,
            direct.and_then(|id| by_id.get(&id).copied()),
            candidates.iter().map(|id| identities[id]),
            policy,
        );
        if direct.is_none()
            && !candidates
                .iter()
                .any(|id| by_id[id]["scheduler"]["job"] == job["id"])
        {
            let eligible: Vec<_> = candidates
                .iter()
                .copied()
                .filter(|id| matches!(text(by_id[id], "status"), "doing" | "todo"))
                .collect();
            if !eligible.is_empty() {
                candidates = eligible;
            }
        }
        out.targets.insert(id.to_owned(), target);
        if target == Resolution::Unknown
            || target.product().is_some_and(|p| p.rank == 10)
            || linked.iter().any(|id| {
                identities[id] == Resolution::Unknown
                    || cc_scheduler::priority::item_rank(by_id[id], policy) == 10
            })
        {
            out.sensitive.insert(id.to_owned());
        }
        for todo in linked {
            out.raw.entry(todo).or_default().push(job);
            if identity::compatible(identities[&todo], target)
                && !(direct.is_some()
                    && identities[&todo].product().is_some()
                    && target.product().is_none())
            {
                out.history.entry(todo).or_default().push(job);
            }
        }
        if let Some(todo) = candidates
            .into_iter()
            .find(|id| identity::compatible(identities[id], target))
        {
            out.assigned.entry(todo).or_default().push(job);
        }
    }
    out
}

pub(super) fn catalogue(product: &catalog::Product, mut value: Value) -> Value {
    if let Some(identity) = identity::by_name(product.name) {
        value["productIdentity"] = json!({"state":"catalogue","productId":identity.id,"ownerPolicy":identity.owner_policy});
        value["priority"] = json!(identity.rank);
        value["activityState"] =
            json!(if value["workers"]
                .as_array()
                .into_iter()
                .flatten()
                .any(|w| w["primaryJob"].is_string()
                    && matches!(
                        w["jobStatus"].as_str(),
                        Some("running" | "starting" | "pending")
                    ))
            {
                "recorded_active"
            } else {
                "unobserved"
            });
        value["observationScope"] = json!("cc_jobs");
        value["progress"] = Value::Null;
    }
    value
}
