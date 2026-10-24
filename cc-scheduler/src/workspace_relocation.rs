//! Explicit, provenance-checked relocation of one formerly shared repair task.
use crate::{backend::Backend, common::*, engine::Scheduler};
use serde::{Deserialize, Serialize};
use serde_json::Value;

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Request {
    pub id: u64,
    pub source_job: String,
    pub shared_owner_job: String,
    pub source_branch: String,
    pub source_head: String,
    pub worktree: String,
    pub branch: String,
    pub head: String,
    pub spec_reference: String,
    pub spec_sha256: String,
}

pub fn relocate<B: Backend>(
    _engine: &mut Scheduler<B>,
    _runner: &mut dyn Runner,
    _request: &Request,
    _expected: Option<&str>,
) -> Result<Value> {
    Err(Error("WorkspaceRelocationUnavailable"))
}
