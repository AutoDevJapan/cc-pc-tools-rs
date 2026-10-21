//! Owner-declared business targets. These pins confer no repository permissions.
//! Never infer a business target from task prose or a GoDD/MCP substring.
use serde_json::{Value, json};
use std::collections::{BTreeMap, BTreeSet};

#[derive(Debug, PartialEq, Eq)]
pub struct Product {
    pub id: &'static str,
    pub name: &'static str,
    pub department: usize,
    pub rank: u32,
    pub owner_policy: &'static str,
}

pub const CONTRACT: Product = Product {
    id: "godd-mcp-contract",
    name: "GoDD MCP",
    department: 4,
    rank: 10,
    owner_policy: "client_rules",
};
pub const ALPHA: Product = Product {
    id: "godd-mcp-alpha",
    name: "GoDD MCP α",
    department: 1,
    rank: 60,
    owner_policy: "owned_product_rules",
};

fn key(name: &str) -> String {
    name.trim()
        .to_lowercase()
        .replace('α', "alpha")
        .chars()
        .filter(|c| !c.is_whitespace() && !matches!(c, '-' | '_' | ':' | '.'))
        .collect()
}

pub fn by_name(name: &str) -> Option<&'static Product> {
    match key(name).as_str() {
        "goddmcp" => Some(&CONTRACT),
        "goddmcpalpha" => Some(&ALPHA),
        _ => None,
    }
}

pub fn mcp_family(name: &str) -> bool {
    key(name).starts_with("goddmcp")
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Resolution {
    Verified(&'static Product),
    OwnerDeclared(&'static Product),
    NameOnly(&'static Product),
    Unknown,
    Other,
}
impl Resolution {
    pub fn product(self) -> Option<&'static Product> {
        match self {
            Self::Verified(p) | Self::OwnerDeclared(p) | Self::NameOnly(p) => Some(p),
            _ => None,
        }
    }
    pub fn metadata(self) -> Value {
        let state = match self {
            Self::Verified(_) => "verified",
            Self::OwnerDeclared(_) => "owner_declared",
            Self::NameOnly(_) => "name_only",
            Self::Unknown => "unknown",
            Self::Other => "not_applicable",
        };
        let mut value = json!({"state":state,"productId":self.product().map(|p|p.id)});
        if let Self::Verified(p) | Self::OwnerDeclared(p) = self {
            value["ownerPolicy"] = json!(p.owner_policy);
        }
        value
    }
}

// Real repository/owner pairs stay in private distribution inputs. Public CI
// supplies synthetic policy bindings; an unconfigured executable fails closed.
fn binding(product: &Product, policy: &Value) -> Option<(u64, u64)> {
    if let Some(bindings) = policy.get("product_identities") {
        let row = &bindings[product.id];
        let pair = (row["repository_id"].as_u64()?, row["owner_id"].as_u64()?);
        return (pair.0 > 0 && pair.1 > 0).then_some(pair);
    }
    let (repository, owner) = if product.id == CONTRACT.id {
        (
            option_env!("CC_PRODUCT_MCP_CONTRACT_REPOSITORY_ID")?,
            option_env!("CC_PRODUCT_MCP_CONTRACT_OWNER_ID")?,
        )
    } else {
        (
            option_env!("CC_PRODUCT_MCP_ALPHA_REPOSITORY_ID")?,
            option_env!("CC_PRODUCT_MCP_ALPHA_OWNER_ID")?,
        )
    };
    let pair: (u64, u64) = (repository.parse().ok()?, owner.parse().ok()?);
    (pair.0 > 0 && pair.1 > 0).then_some(pair)
}

fn explicit(value: &Value, policy: &Value) -> Resolution {
    let Some(object) = value.as_object() else {
        return Resolution::Unknown;
    };
    let product = [&CONTRACT, &ALPHA].into_iter().find(|p| {
        binding(p, policy)
            .is_some_and(|(repository, _)| value["repository_id"].as_u64() == Some(repository))
    });
    let Some(product) = product else {
        return Resolution::Unknown;
    };
    if value["owner_id"].as_u64() != binding(product, policy).map(|(_, owner)| owner)
        || object
            .get("product_id")
            .is_some_and(|id| id.as_str() != Some(product.id))
    {
        return Resolution::Unknown;
    }
    Resolution::Verified(product)
}

/// Only finite, recorded owner declarations bridge a maintenance repo and a product.
/// The global ToDo IDs are scoped to their recorded maintenance project, never prose.
fn declared(row: &Value) -> Option<Resolution> {
    if matches!(row["project"].as_str(), Some("cc-pc-tools-rs"))
        && matches!(row["id"].as_u64(), Some(2641 | 2737 | 3093))
    {
        Some(Resolution::OwnerDeclared(&ALPHA))
    } else {
        None
    }
}

pub fn display_project<'a>(row: &'a Value, policy: &'a Value) -> &'a str {
    let id = row["id"]
        .as_u64()
        .map(|id| id.to_string())
        .unwrap_or_default();
    policy["items"][&id]["project"]
        .as_str()
        .or_else(|| row["owner_project"].as_str())
        .or_else(|| row["project"].as_str())
        .unwrap_or("")
}

/// Explicit business identity outranks code identity. An invalid explicit value
/// stops resolution; names and paths must not silently rescue a failed owner check.
pub fn resolve(row: &Value, policy: &Value) -> Resolution {
    let id = row["id"]
        .as_u64()
        .map(|id| id.to_string())
        .unwrap_or_default();
    let owned = &policy["items"][&id];
    let a = owned
        .get("product_identity")
        .map(|value| explicit(value, policy));
    let b = row
        .get("product_identity")
        .map(|value| explicit(value, policy));
    if let (Some(a), Some(b)) = (a, b)
        && a != b
    {
        return Resolution::Unknown;
    }
    if let Some(result) = a.or(b) {
        if declared(row).is_some_and(|declared| !compatible(declared, result)) {
            return Resolution::Unknown;
        }
        return result;
    }
    if let Some(result) = declared(row) {
        return result;
    }
    let project = display_project(row, policy);
    if (row.get("repository_id").is_some() || row.get("repository_owner_id").is_some())
        && ([&CONTRACT, &ALPHA].iter().any(|p| {
            binding(p, policy)
                .is_some_and(|(repository, _)| row["repository_id"].as_u64() == Some(repository))
        }) || mcp_family(project))
    {
        return explicit(
            &json!({"repository_id":row["repository_id"],"owner_id":row["repository_owner_id"]}),
            policy,
        );
    }
    if let Some(product) = by_name(project) {
        return Resolution::NameOnly(product);
    }
    if mcp_family(project) {
        Resolution::Unknown
    } else {
        Resolution::Other
    }
}

/// This checks product identity only; a caller must also verify a recorded job edge.
pub fn compatible(a: Resolution, b: Resolution) -> bool {
    if a == Resolution::Unknown || b == Resolution::Unknown {
        return false;
    }
    match (a.product(), b.product()) {
        (Some(a), Some(b)) => a.id == b.id,
        _ => true,
    }
}

/// Conflicting legacy edges are evidence of uncertainty, never a tie to break
/// by task number. Scope this rule to these products; other rankings stay intact.
pub fn ambiguous(resolutions: impl IntoIterator<Item = Resolution>) -> bool {
    let mut ids = std::collections::BTreeSet::new();
    for resolution in resolutions {
        if resolution == Resolution::Unknown {
            return true;
        }
        if let Some(product) = resolution.product() {
            ids.insert(product.id);
        }
    }
    ids.len() > 1
}

/// Resolve the job's business target using its canonical primary task. History
/// aliases are useful only in the absence of that primary, never in its place.
pub fn job_target(
    job: &Value,
    primary: Option<&Value>,
    legacy: impl IntoIterator<Item = Resolution>,
    policy: &Value,
) -> Resolution {
    let own = resolve(job, policy);
    if let Some(primary) = primary {
        let target = resolve(primary, policy);
        if !compatible(own, target) {
            return Resolution::Unknown;
        }
        if own == Resolution::Other {
            return target;
        }
        return own;
    }
    if own == Resolution::Other && ambiguous(legacy) {
        Resolution::Unknown
    } else {
        own
    }
}

/// Same opaque key as agent-progress; source job IDs never enter the response.
pub fn job_key(id: &str) -> String {
    use sha2::{Digest, Sha256};
    format!("job-{:x}", Sha256::digest(id.as_bytes()))
}

pub fn bindings_configured() -> bool {
    binding(&CONTRACT, &Value::Null)
        .zip(binding(&ALPHA, &Value::Null))
        .is_some_and(|(a, b)| a.0 != b.0)
}

/// A read-only deployment probe. It performs no installed-store access and
/// returns neither the values nor a guessable digest of private identities.
pub fn status_requested(mut args: impl Iterator<Item = String>) -> bool {
    args.next().as_deref() == Some("--product-identity-status") && args.next().is_none()
}
pub fn status() -> Value {
    json!({"schemaVersion":1,"configured":bindings_configured()})
}

// One observation supplies current execution, compatible history and the full
// privacy union. No file, path, prompt or task prose is read by this index.
fn record_text<'a>(row: &'a Value, key: &str) -> &'a str {
    row[key].as_str().unwrap_or("")
}
pub struct Associations<'a> {
    assigned: BTreeMap<u64, Vec<&'a Value>>,
    raw: BTreeMap<u64, Vec<&'a Value>>,
    history: BTreeMap<u64, Vec<&'a Value>>,
    targets: BTreeMap<String, Resolution>,
    sensitive: BTreeSet<String>,
    primary: BTreeMap<String, u64>,
}
impl<'a> Associations<'a> {
    pub fn record(&self, row: &Value, policy: &Value) -> Resolution {
        let current = self.current(row["id"].as_u64().unwrap_or(0));
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
    /// Current identity selection is shared with dependency propagation.
    pub fn current(&self, id: u64) -> Option<&'a Value> {
        self.assigned(id).iter().copied().find(|job| {
            matches!(
                record_text(job, "status"),
                "queued" | "pending" | "starting" | "running"
            )
        })
    }
    pub fn primary(&self, job: &Value) -> Option<u64> {
        self.primary.get(record_text(job, "id")).copied()
    }
    pub fn assigned(&self, id: u64) -> &[&'a Value] {
        self.assigned.get(&id).map(Vec::as_slice).unwrap_or(&[])
    }
    pub fn raw(&self, id: u64) -> &[&'a Value] {
        self.raw.get(&id).map(Vec::as_slice).unwrap_or(&[])
    }
    pub fn history(&self, id: u64) -> &[&'a Value] {
        self.history.get(&id).map(Vec::as_slice).unwrap_or(&[])
    }
    pub fn target(&self, job: &Value) -> Resolution {
        self.targets
            .get(record_text(job, "id"))
            .copied()
            .unwrap_or(Resolution::Unknown)
    }
    pub fn sensitive(&self, job: &Value) -> bool {
        self.sensitive.contains(record_text(job, "id"))
    }
}

/// Index recorded IDs once, preserving all privacy edges while selecting only
/// one primary execution edge. Never walk paths, prompts or task prose.
pub fn assignments<'a>(items: &'a [Value], jobs: &'a [Value], policy: &Value) -> Associations<'a> {
    let by_id: BTreeMap<_, _> = items
        .iter()
        .filter_map(|row| Some((row["id"].as_u64()?, row)))
        .collect();
    let identities: BTreeMap<_, _> = by_id
        .iter()
        .map(|(id, row)| (*id, resolve(row, policy)))
        .collect();
    let mut refs = BTreeMap::<String, BTreeSet<u64>>::new();
    // Privacy is a union over every physical record, including duplicate IDs.
    let private_ids: BTreeSet<_> = items
        .iter()
        .filter(|row| {
            resolve(row, policy) == Resolution::Unknown
                || crate::priority::item_rank(row, policy) == 10
        })
        .filter_map(|row| row["id"].as_u64())
        .collect();
    for item in items {
        let Some(id) = item["id"].as_u64() else {
            continue;
        };
        for key in item["jobs"]
            .as_array()
            .into_iter()
            .flatten()
            .chain([&item["job"], &item["scheduler"]["job"]])
        {
            if let Some(key) = key.as_str().filter(|key| !key.is_empty()) {
                refs.entry(key.to_owned()).or_default().insert(id);
            }
        }
    }
    let mut out = Associations {
        assigned: BTreeMap::new(),
        raw: BTreeMap::new(),
        history: BTreeMap::new(),
        targets: BTreeMap::new(),
        sensitive: BTreeSet::new(),
        primary: BTreeMap::new(),
    };
    for job in jobs {
        let id = record_text(job, "id");
        let mut linked = BTreeSet::new();
        for key in ["id", "thread_id", "resume_session"] {
            if let Some(ids) = refs.get(record_text(job, key)) {
                linked.extend(ids);
            }
        }
        let direct = job["todo_id"].as_u64();
        if let Some(id) = direct.filter(|id| by_id.contains_key(id)) {
            linked.insert(id);
        }
        linked.extend(
            job["related_todo_ids"]
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(Value::as_u64)
                .filter(|id| by_id.contains_key(id)),
        );
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
        let target = job_target(
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
                .filter(|id| matches!(record_text(by_id[id], "status"), "doing" | "todo"))
                .collect();
            if !eligible.is_empty() {
                candidates = eligible;
            } else if target.product().is_some() {
                // Inactive aliases are history, not a current execution.
                candidates.clear();
            }
        }
        out.targets.insert(id.to_owned(), target);
        if target == Resolution::Unknown
            || target.product().is_some_and(|p| p.rank == 10)
            || linked.iter().any(|id| private_ids.contains(id))
        {
            out.sensitive.insert(id.to_owned());
        }
        for todo in linked {
            out.raw.entry(todo).or_default().push(job);
            if compatible(identities[&todo], target)
                && !(direct.is_some()
                    && identities[&todo].product().is_some()
                    && target.product().is_none())
            {
                out.history.entry(todo).or_default().push(job);
            }
        }
        if let Some(todo) = candidates
            .into_iter()
            .find(|id| compatible(identities[id], target))
        {
            out.primary.insert(id.to_owned(), todo);
            out.assigned.entry(todo).or_default().push(job);
        }
    }
    for jobs in out.assigned.values_mut() {
        jobs.sort_by(|a, b| {
            record_text(b, "created_at")
                .cmp(record_text(a, "created_at"))
                .then_with(|| record_text(a, "id").cmp(record_text(b, "id")))
        });
    }
    // Effective business ownership is known only after current primary jobs
    // have been selected. Its privacy must cover every raw related report,
    // even when that report has a different execution primary.
    let private_current: BTreeSet<_> = items
        .iter()
        .filter(|row| {
            let identity = out.record(row, policy);
            identity == Resolution::Unknown
                || identity.product().is_some_and(|product| product.rank == 10)
        })
        .filter_map(|row| row["id"].as_u64())
        .collect();
    for id in private_current {
        for job in out.raw.get(&id).into_iter().flatten() {
            out.sensitive.insert(record_text(job, "id").to_owned());
        }
    }
    out
}
