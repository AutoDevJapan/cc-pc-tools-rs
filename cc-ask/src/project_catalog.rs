//! Opaque projection of the existing department classification, not a new list.
use crate::core::Result;
use regex::Regex;
use serde_json::Value;
use sha2::{Digest, Sha256};
use std::{collections::BTreeSet, sync::LazyLock};

pub(crate) struct Catalog {
    protected: BTreeSet<String>,
    lengths: BTreeSet<usize>,
    known: BTreeSet<String>,
}

fn letters(text: &str) -> Vec<(char, usize, usize)> {
    static LETTER: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^[\p{L}\p{Nd}]$").unwrap());
    text.char_indices()
        .flat_map(|(start, ch)| {
            ch.to_lowercase().filter_map(move |lower| {
                LETTER
                    .is_match(lower.encode_utf8(&mut [0u8; 4]))
                    .then_some((lower, start, start + ch.len_utf8()))
            })
        })
        .collect()
}

fn digest(text: impl Iterator<Item = char>) -> String {
    format!("{:x}", Sha256::digest(text.collect::<String>().as_bytes()))
}

impl Catalog {
    pub(crate) fn embedded() -> Result<Self> {
        Self::parse(include_str!("../../repair-guard/owner_catalog.json"))
    }

    fn parse(raw: &str) -> Result<Self> {
        let data: Value =
            serde_json::from_str(raw).map_err(|_| "project privacy catalog invalid")?;
        if data["domain"] != "owner-department-classification-v1" {
            return Err("project privacy catalog invalid".into());
        }
        let mut result = Self {
            protected: BTreeSet::new(),
            lengths: BTreeSet::new(),
            known: BTreeSet::new(),
        };
        for group in data["groups"]
            .as_array()
            .ok_or("project privacy catalog groups missing")?
        {
            let aliases = group["aliases"]
                .as_array()
                .ok_or("project privacy catalog aliases missing")?;
            let protected = group["kind"] == "client_project";
            if !protected && group["kind"] != "self" {
                return Err("project privacy catalog kind invalid".into());
            }
            for alias in aliases {
                let value = alias
                    .as_str()
                    .filter(|v| v.len() == 64 && v.bytes().all(|b| b.is_ascii_hexdigit()))
                    .ok_or("project privacy catalog hash invalid")?;
                result.known.insert(value.to_owned());
                if protected {
                    result.protected.insert(value.to_owned());
                }
            }
            for width in group["normalized_lengths"]
                .as_array()
                .ok_or("project privacy catalog lengths missing")?
            {
                let width = width
                    .as_u64()
                    .filter(|n| *n > 0 && *n <= 256)
                    .ok_or("project privacy catalog width invalid")?
                    as usize;
                if protected {
                    result.lengths.insert(width);
                }
            }
        }
        if result.known.is_empty() || result.protected.is_empty() || result.lengths.is_empty() {
            return Err("project privacy catalog incomplete".into());
        }
        Ok(result)
    }

    pub(crate) fn known_project(&self, text: &str) -> bool {
        let normalized = text.to_lowercase();
        let text = normalized.strip_prefix("resume-").unwrap_or(&normalized);
        let letters = letters(text);
        self.known.contains(&digest(letters.iter().map(|v| v.0)))
    }

    pub(crate) fn contains_protected(&self, text: &str) -> bool {
        let normalized = letters(text);
        self.lengths.iter().any(|&width| {
            normalized.windows(width).any(|window| {
                self.protected
                    .contains(&digest(window.iter().map(|value| value.0)))
            })
        })
    }

    pub(crate) fn redact(&self, text: &str) -> String {
        let normalized = letters(text);
        let mut matches = Vec::new();
        for &width in &self.lengths {
            for window in normalized.windows(width) {
                if self.protected.contains(&digest(window.iter().map(|v| v.0))) {
                    matches.push((window[0].1, window[window.len() - 1].2));
                }
            }
        }
        matches.sort_unstable();
        let mut merged: Vec<(usize, usize)> = Vec::new();
        for (start, end) in matches {
            if let Some(last) = merged.last_mut().filter(|last| start <= last.1) {
                last.1 = last.1.max(end);
            } else {
                merged.push((start, end));
            }
        }
        let mut result = String::new();
        let mut previous = 0;
        for (start, end) in merged {
            result.push_str(&text[previous..start]);
            result.push_str("クライアント案件");
            previous = end;
        }
        result.push_str(&text[previous..]);
        result
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn existing_department_projection_can_replace_missing_legacy_names() {
        assert!(Catalog::embedded().is_ok());
    }

    #[test]
    fn protected_names_and_paths_are_redacted_without_plaintext_catalog() {
        let hash = digest("privateclient".chars());
        let raw = serde_json::json!({"domain":"owner-department-classification-v1","groups":[{"kind":"client_project","aliases":[hash],"normalized_lengths":[13]}]}).to_string();
        assert!(!raw.contains("privateclient"));
        let catalog = Catalog::parse(&raw).unwrap();
        assert_eq!(
            catalog.redact("about Private-Client and C:/PrivateClient/report"),
            "about クライアント案件 and C:/クライアント案件/report"
        );
        assert_eq!(catalog.redact("public product"), "public product");
        assert!(catalog.known_project("Private-Client"));
        assert!(catalog.contains_protected("D:/Private-Client/report.png"));
        assert!(!catalog.contains_protected("D:/public-product/report.png"));
        assert!(!catalog.known_project("unknown"));
        for bad in [
            "{}",
            r#"{"domain":"owner-department-classification-v1","groups":[]}"#,
        ] {
            assert!(Catalog::parse(bad).is_err());
        }
    }

    #[test]
    fn normalized_utf8_spans_overlap_and_resume_aliases_preserve_public_text() {
        let aliases = ["privateclient", "client", "架空案件"];
        let raw = serde_json::json!({"domain":"owner-department-classification-v1","groups":[{
            "kind":"client_project", "aliases":aliases.map(|name| digest(name.chars())),
            "normalized_lengths":[13,6,4]
        }]})
        .to_string();
        let catalog = Catalog::parse(&raw).unwrap();
        assert_eq!(
            catalog.redact("始 Private-Client 終 架空・案件"),
            "始 クライアント案件 終 クライアント案件"
        );
        assert_eq!(catalog.redact("public"), "public");
        assert!(catalog.contains_protected("resume-PrivateClient"));
        assert!(!catalog.known_project("public"));
    }
}
