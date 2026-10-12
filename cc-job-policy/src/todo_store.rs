//! Byte contract for the complete shared ToDo snapshot, never a filtered view.
use serde_json::Value;
use std::{
    fmt,
    fs::File,
    io::{self, Read},
    path::Path,
};

/// Four times the former console ceiling; history remains in the canonical store.
pub const MAX_BYTES: usize = 64 * 1024 * 1024;

#[derive(Debug)]
pub enum Error {
    TooLarge,
    Io(io::Error),
    InvalidJson(serde_json::Error),
}
impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::TooLarge => write!(
                f,
                "todo_store_too_large: shared ToDo exceeds {MAX_BYTES} bytes (64 MiB)"
            ),
            Self::Io(e) => write!(f, "todo_store_io: {e}"),
            Self::InvalidJson(e) => write!(f, "todo_store_invalid_json: {e}"),
        }
    }
}
impl std::error::Error for Error {}

pub fn check_size(bytes: usize) -> Result<(), Error> {
    if bytes > MAX_BYTES {
        Err(Error::TooLarge)
    } else {
        Ok(())
    }
}

/// Bound actual reads, including a file which grows after it is opened.
/// Parsing uses exactly the bytes read; redaction belongs to presentation.
pub fn read(path: &Path) -> Result<Value, Error> {
    read_bounded(path, MAX_BYTES)
}
/// Shared byte primitive; each canonical store supplies its own limit and error labels.
pub fn read_bounded(path: &Path, limit: usize) -> Result<Value, Error> {
    read_from_limit(File::open(path).map_err(Error::Io)?, limit)
}
#[cfg(test)]
fn read_from(reader: impl Read) -> Result<Value, Error> {
    read_from_limit(reader, MAX_BYTES)
}
fn read_from_limit(reader: impl Read, limit: usize) -> Result<Value, Error> {
    let mut bytes = Vec::new();
    reader
        .take(limit.saturating_add(1) as u64)
        .read_to_end(&mut bytes)
        .map_err(Error::Io)?;
    if bytes.len() > limit {
        return Err(Error::TooLarge);
    }
    let bytes = bytes.strip_prefix(&[0xef, 0xbb, 0xbf]).unwrap_or(&bytes);
    serde_json::from_slice(bytes).map_err(Error::InvalidJson)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn complete_json_above_old_ceiling_keeps_every_field() {
        let db = json!({"projects":{"demo":"demo"},"items":[
            {"id":1,"spec":"saved.md","needs":["cargo"],"subwork":{"parent_todo_id":2},
             "console_creation_request":{"request_id":"manual:finite"},"note_history":["x".repeat(16*1024*1024)]},
            {"id":2,"status":"doing","unknown":{"retain":true}}
        ],"next_id":3,"stalls":{"demo":{"key":"queued-canonical"}}});
        let bytes = serde_json::to_vec(&db).unwrap();
        assert!(bytes.len() > 16 * 1024 * 1024);
        assert_eq!(read_from(bytes.as_slice()).unwrap(), db);
    }

    #[test]
    fn exact_limit_is_allowed_and_excess_is_not_partial_json() {
        let mut bytes = b"{}".to_vec();
        bytes.resize(MAX_BYTES, b' ');
        assert_eq!(read_from(bytes.as_slice()).unwrap(), json!({}));
        bytes.push(b' ');
        assert!(matches!(read_from(bytes.as_slice()), Err(Error::TooLarge)));
        assert!(matches!(check_size(MAX_BYTES + 1), Err(Error::TooLarge)));
    }

    #[test]
    fn corruption_and_io_have_separate_reasons() {
        assert!(matches!(
            read_from(&b"{broken"[..]),
            Err(Error::InvalidJson(_))
        ));
        struct Denied;
        impl Read for Denied {
            fn read(&mut self, _: &mut [u8]) -> io::Result<usize> {
                Err(io::ErrorKind::PermissionDenied.into())
            }
        }
        assert!(
            matches!(read_from(Denied), Err(Error::Io(e)) if e.kind() == io::ErrorKind::PermissionDenied)
        );
        let temp = tempfile::tempdir().unwrap();
        assert!(
            matches!(read(&temp.path().join("absent")), Err(Error::Io(e)) if e.kind() == io::ErrorKind::NotFound)
        );
    }
}
