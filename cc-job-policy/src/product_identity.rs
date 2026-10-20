//! Owner-declared business targets. These pins confer no repository permissions.
//! Never infer a business target from task prose or a GoDD/MCP substring.
use serde_json::{Value, json};

#[derive(Debug, PartialEq, Eq)]
pub struct Product {
    pub id: &'static str,
    pub name: &'static str,
    pub repository_id: u64,
    pub owner_id: u64,
    pub department: usize,
    pub rank: u32,
    pub owner_policy: &'static str,
}

pub const CONTRACT: Product = Product {
    id: "godd-mcp-contract",
    name: "GoDD MCP",
    repository_id: 1156778979,
    owner_id: 133784601,
    department: 4,
    rank: 10,
    owner_policy: "client_rules",
};
pub const ALPHA: Product = Product {
    id: "godd-mcp-alpha",
    name: "GoDD MCP α",
    repository_id: 1154066087,
    owner_id: 246834983,
    department: 1,
    rank: 60,
    owner_policy: "owned_product_rules",
};

fn key(name: &str) -> String {
    name.trim()
        .to_lowercase()
        .replace('α', "alpha")
        .chars()
        .filter(|c| !matches!(c, '-' | '_' | ' ' | ':' | '.'))
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
            value["repositoryId"] = json!(p.repository_id);
            value["ownerId"] = json!(p.owner_id);
            value["ownerPolicy"] = json!(p.owner_policy);
        }
        value
    }
}

fn explicit(value: &Value) -> Resolution {
    let Some(object) = value.as_object() else {
        return Resolution::Unknown;
    };
    let product = [&CONTRACT, &ALPHA]
        .into_iter()
        .find(|p| value["repository_id"].as_u64() == Some(p.repository_id));
    let Some(product) = product else {
        return Resolution::Unknown;
    };
    if value["owner_id"].as_u64() != Some(product.owner_id)
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
    let a = owned.get("product_identity").map(explicit);
    let b = row.get("product_identity").map(explicit);
    if let (Some(a), Some(b)) = (a, b) {
        return if a == b { a } else { Resolution::Unknown };
    }
    if let Some(result) = a.or(b) {
        return result;
    }
    if let Some(result) = declared(row) {
        return result;
    }
    let project = display_project(row, policy);
    if (row.get("repository_id").is_some() || row.get("repository_owner_id").is_some())
        && ([&CONTRACT, &ALPHA]
            .iter()
            .any(|p| row["repository_id"].as_u64() == Some(p.repository_id))
            || mcp_family(project))
    {
        return explicit(
            &json!({"repository_id":row["repository_id"],"owner_id":row["repository_owner_id"]}),
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
    let mut mcp = false;
    for resolution in resolutions {
        if resolution == Resolution::Unknown {
            return true;
        }
        if let Some(product) = resolution.product() {
            mcp = true;
            ids.insert(product.id);
        } else {
            ids.insert("");
        }
    }
    mcp && ids.len() > 1
}
