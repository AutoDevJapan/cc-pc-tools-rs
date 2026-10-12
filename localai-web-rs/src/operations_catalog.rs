//! Display names and membership explicitly supplied by the owner on 2026-10-12.
//! This is a positive display catalogue, not a replacement for lost privacy records.

pub struct Product {
    pub name: &'static str,
    pub department: usize,
    aliases: &'static [&'static str],
}

pub const PRODUCTS: &[Product] = &[
    Product {
        name: "GoDD 13a13e1",
        department: 1,
        aliases: &["GoDD-13a13e1", "13a13e1"],
    },
    Product {
        name: "GoDD AO",
        department: 1,
        aliases: &["GoDD-AO"],
    },
    Product {
        name: "GoDD Agent",
        department: 1,
        aliases: &["GoDD-Agent", "godd-agent-prd", "godd-agent-dev"],
    },
    Product {
        name: "GoDD Creators",
        department: 1,
        aliases: &["GoDD-Creators"],
    },
    Product {
        name: "GoDD Genesis",
        department: 1,
        aliases: &["GoDD-Genesis", "Genesis"],
    },
    Product {
        name: "GoDD MCP α",
        department: 1,
        aliases: &["GoDD-MCP-Alpha", "GoDD-MCP-α"],
    },
    Product {
        name: "GoDD Press",
        department: 1,
        aliases: &["GoDD-Press"],
    },
    Product {
        name: "GoDD Rosetta",
        department: 1,
        aliases: &["GoDD-Rosetta", "GoDD-Rosseta"],
    },
    Product {
        name: "GoDD Virtual Streamer",
        department: 1,
        aliases: &["GoDD-VirtualStreamer", "VirtualStreamer"],
    },
    Product {
        name: "ALT-LINE",
        department: 2,
        aliases: &[],
    },
    Product {
        name: "cashcow",
        department: 2,
        aliases: &["CashCow"],
    },
    Product {
        name: "CoChara",
        department: 2,
        aliases: &[],
    },
    Product {
        name: "Geonaut",
        department: 2,
        aliases: &[],
    },
    Product {
        name: "Git道",
        department: 2,
        aliases: &["GitDo", "Git道"],
    },
    Product {
        name: "KaraokeMaker",
        department: 2,
        aliases: &[],
    },
    Product {
        name: "OmegaPortal",
        department: 2,
        aliases: &[],
    },
    Product {
        name: "PassShare",
        department: 2,
        aliases: &[],
    },
    Product {
        name: "QaJaRuEn",
        department: 2,
        aliases: &["QaJaRu"],
    },
    Product {
        name: "Re:X",
        department: 2,
        aliases: &["Re-X", concat!("x", "LIFE"), concat!("GoDD-x", "LIFE")],
    },
    Product {
        name: "Scoopa",
        department: 2,
        aliases: &["scoopa-api", "soopa-api"],
    },
    Product {
        name: "no-a",
        department: 2,
        aliases: &[],
    },
    Product {
        name: "PCの道具",
        department: 5,
        aliases: &["tools", "PCツール", "cc-pc-tools-rs"],
    },
    Product {
        name: "local-ai-concole",
        department: 5,
        aliases: &["localai-web-rs", "localai-web", "local-ai-console"],
    },
    Product {
        name: "OpenCodex",
        department: 5,
        aliases: &[],
    },
    // Retained from the owner's previous explicit instruction, as an internal unit.
    Product {
        name: "Skills",
        department: 5,
        aliases: &["skill"],
    },
    Product {
        name: "万世記",
        department: 0,
        aliases: &["civ6", "civ6-banseiki", "banseiki", "万世紀"],
    },
    Product {
        name: "PhosGaea",
        department: 0,
        aliases: &[],
    },
    Product {
        name: "YCC-HP",
        department: 4,
        aliases: &[],
    },
    Product {
        name: "ARPS",
        department: 4,
        aliases: &[],
    },
];

fn key(name: &str) -> String {
    name.trim()
        .to_lowercase()
        .replace('α', "alpha")
        .chars()
        .filter(|c| !matches!(c, '-' | '_' | ' ' | ':' | '.'))
        .collect()
}

pub fn find(name: &str) -> Option<&'static Product> {
    let key = key(name);
    PRODUCTS
        .iter()
        .find(|p| key == self::key(p.name) || p.aliases.iter().any(|alias| key == self::key(alias)))
}

/// Older registry aliases can be recovered from an exact, owner-named root.
/// Only a compiled catalogue label is returned; the original path is never displayed.
pub fn registered_root(path: &str) -> Option<&'static Product> {
    let path = path.replace('\\', "/");
    let root = path.trim_end_matches('/');
    if root.eq_ignore_ascii_case("D:/CC") || root.eq_ignore_ascii_case("D:/CC/Tools") {
        return find("PCの道具");
    }
    let leaf = root.rsplit('/').next()?;
    find(leaf.strip_suffix("-wt").unwrap_or(leaf))
}

/// Names already explicitly used by the owner; no arbitrary registered name is exposed.
pub fn previously_named(name: &str) -> Option<&'static str> {
    match key(name).as_str() {
        "adj" => Some("ADJ"),
        "phosgaeavol1" => Some("PhosGaea-Vol1"),
        "socialpilot" => Some("Social Pilot"),
        "youtubedaily" => Some("youtube-daily"),
        "chatgptweb" => Some("chatgpt-web"),
        "常駐" | "makhmeto" => Some("常駐"),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn owner_catalogue_is_exact_and_aliases_share_membership() {
        assert_eq!(PRODUCTS.len(), 29);
        assert_eq!(find("GoDD-MCP-Alpha").unwrap().name, "GoDD MCP α");
        assert_eq!(find("godd-mcp-α").unwrap().name, "GoDD MCP α");
        assert_eq!(find("GoDD-Rosseta").unwrap().name, "GoDD Rosetta");
        assert_eq!(find("civ6-banseiki").unwrap().name, "万世記");
        assert_eq!(find("scoopa-api").unwrap().name, "Scoopa");
        assert_eq!(find("localai-web-rs").unwrap().department, 5);
        assert_eq!(find("ARPS").unwrap().department, 4);
        assert_eq!(find("skill").unwrap().department, 5);
        assert!(find("CC").is_none());
        assert_eq!(registered_root("D:/CC").unwrap().name, "PCの道具");
        assert_eq!(registered_root("D:/CC/Tools").unwrap().name, "PCの道具");
        assert_eq!(
            registered_root("D:/synthetic/CashCow").unwrap().name,
            "cashcow"
        );
        assert!(registered_root("D:/synthetic/CC").is_none());
        assert!(registered_root("D:/CC/customer/CC").is_none());
        assert_eq!(registered_root("D:/public/YCC-HP").unwrap().name, "YCC-HP");
        assert!(registered_root("D:/private/YCC-HP-customer").is_none());
        assert!(find("PhosGaea-Vol1").is_none());
        assert!(find("GoDD-Rosetta-private-customer").is_none());
        assert!(find("https://example.com/ARPS").is_none());
        assert!(find("D:/private/GoDD-AO").is_none());
    }
}
