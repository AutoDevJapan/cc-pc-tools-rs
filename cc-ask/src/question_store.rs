//! The complete question snapshot has a finite read/write byte contract.
use serde_json::Value;
use std::{fmt, io, path::Path};

pub const MAX_BYTES: usize = 32 * 1024 * 1024;
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
                "question_state_too_large: limit {MAX_BYTES} bytes (32 MiB)"
            ),
            Self::Io(e) => write!(f, "question_state_io: {e}"),
            Self::InvalidJson(e) => write!(f, "question_state_invalid_json: {e}"),
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
pub fn read(path: &Path) -> Result<Value, Error> {
    cc_job_policy::todo_store::read_bounded(path, MAX_BYTES).map_err(|e| match e {
        cc_job_policy::todo_store::Error::TooLarge => Error::TooLarge,
        cc_job_policy::todo_store::Error::Io(e) => Error::Io(e),
        cc_job_policy::todo_store::Error::InvalidJson(e) => Error::InvalidJson(e),
    })
}
