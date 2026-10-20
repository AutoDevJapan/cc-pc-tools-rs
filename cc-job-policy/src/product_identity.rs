//! Owner-declared business targets. These pins confer no repository permissions.
//! Never infer a business target from task prose or a GoDD/MCP substring.
use serde_json::{Value, json};

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
