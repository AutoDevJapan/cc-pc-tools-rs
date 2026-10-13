use std::path::{Component, Path};

pub const C_DELETE_ROOT: &str = r"C:\Users\makhmeto\Downloads\削除希望";
pub const D_DELETE_ROOT: &str = r"D:\削除希望";

// No configurable production roots: a missing value cannot become a workspace.
pub fn authorized_root(path: &Path) -> bool {
    [C_DELETE_ROOT, D_DELETE_ROOT]
        .iter()
        .any(|root| path == Path::new(root))
}

pub fn strict_child(root: &Path, child: &Path) -> bool {
    let Ok(relative) = child.strip_prefix(root) else {
        return false;
    };
    !relative.as_os_str().is_empty()
        && relative
            .components()
            .all(|c| matches!(c, Component::Normal(_)))
}

pub fn retained_name(name: &str) -> bool {
    let name = name.to_ascii_lowercase();
    let ext = Path::new(&name)
        .extension()
        .and_then(|x| x.to_str())
        .unwrap_or("");
    [
        ".git",
        "src",
        "client-projects.txt",
        "cargo.toml",
        "go.mod",
        "go.sum",
        "package.json",
        "pyproject.toml",
        "requirements.txt",
    ]
    .contains(&name.as_str())
        || [
            "2467",
            "recovery",
            "復旧",
            "原設定",
            "原記録",
            "ledger",
            "protection",
            "verification",
            "test-result",
            "test_result",
            "junit",
            "coverage",
            "sarif",
        ]
        .iter()
        .any(|marker| name.contains(marker))
        || name.contains(".private.")
        || matches!(
            ext,
            "rs" | "go"
                | "c"
                | "cpp"
                | "h"
                | "py"
                | "ts"
                | "tsx"
                | "jsx"
                | "log"
                | "jsonl"
                | "sarif"
                | "trx"
                | "tap"
        )
}

pub fn retained_original(path: &str) -> bool {
    let normalized = path.replace('/', "\\").to_ascii_lowercase();
    [
        r"d:\cc\tools",
        r"d:\cc\work",
        r"d:\cc\skills",
        r"c:\users\makhmeto\.codex",
        r"c:\users\makhmeto\.claude",
    ]
    .iter()
    .any(|root| normalized == *root || normalized.starts_with(&format!("{root}\\")))
        || normalized.split('\\').any(retained_name)
}

// Original recycle-bin paths stay in memory, never in receipts or stdout.
pub fn recycle_original(bytes: &[u8]) -> Option<String> {
    let version = u64::from_le_bytes(bytes.get(..8)?.try_into().ok()?);
    let words = match version {
        1 => bytes.get(24..544)?,
        2 => {
            let length = u32::from_le_bytes(bytes.get(24..28)?.try_into().ok()?) as usize;
            if length == 0 || length > 32_768 {
                return None;
            }
            bytes.get(28..28usize.checked_add(length.checked_mul(2)?)?)?
        }
        _ => return None,
    };
    let words: Vec<_> = words
        .as_chunks::<2>()
        .0
        .iter()
        .map(|x| u16::from_le_bytes([x[0], x[1]]))
        .take_while(|x| *x != 0)
        .collect();
    let value = String::from_utf16(&words).ok()?;
    let b = value.as_bytes();
    (b.len() >= 3 && b[0].is_ascii_alphabetic() && b[1] == b':' && b[2] == b'\\').then_some(value)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn explicit_roots_cannot_be_empty_parent_or_e() {
        assert!(authorized_root(Path::new(C_DELETE_ROOT)));
        assert!(authorized_root(Path::new(D_DELETE_ROOT)));
        for root in [
            "",
            r"D:\",
            r"D:\CC",
            r"C:\Users\makhmeto\Downloads",
            r"E:\削除希望",
        ] {
            assert!(!authorized_root(Path::new(root)));
        }
    }
    #[test]
    fn boundary_rejects_root_siblings_and_parent_components() {
        let root = Path::new("root");
        assert!(!strict_child(root, root));
        assert!(!strict_child(root, Path::new("root-sibling/file")));
        assert!(!strict_child(root, Path::new("root/../outside")));
        assert!(strict_child(root, Path::new("root/..cache/file")));
    }
    #[test]
    fn source_and_recovery_are_kept_even_inside_deletion_requests() {
        for name in [
            "src",
            ".git",
            "Cargo.toml",
            "main.rs",
            "client-projects.txt",
            "recovery-2467",
            "records.private.json",
            "verification.md",
            "scan.sarif",
        ] {
            assert!(retained_name(name));
        }
        assert!(!retained_name("finished-cache.bin"));
        assert!(retained_original(r"D:\CC\Tools\todo\todo.json"));
        assert!(retained_original(r"D:\CC\Work\old\receipt.json"));
        assert!(!retained_original(r"D:\CC\Tools-sibling\finished.bin"));
    }
    #[test]
    fn corrupt_truncated_and_new_recycle_formats_are_not_guessed() {
        assert_eq!(recycle_original(&[]), None);
        let mut data = vec![0; 28];
        data[..8].copy_from_slice(&2u64.to_le_bytes());
        data[24..28].copy_from_slice(&u32::MAX.to_le_bytes());
        assert_eq!(recycle_original(&data), None);
        data[..8].copy_from_slice(&3u64.to_le_bytes());
        assert_eq!(recycle_original(&data), None);
    }
    #[test]
    fn recycle_metadata_preserves_its_real_origin() {
        let expected = r"C:\finished.bin";
        let words: Vec<u16> = expected.encode_utf16().chain(Some(0)).collect();
        let mut data = vec![0; 28];
        data[..8].copy_from_slice(&2u64.to_le_bytes());
        data[24..28].copy_from_slice(&(words.len() as u32).to_le_bytes());
        for word in words {
            data.extend(word.to_le_bytes());
        }
        assert_eq!(recycle_original(&data).as_deref(), Some(expected));
        data.pop();
        assert_eq!(recycle_original(&data), None);
    }
}
