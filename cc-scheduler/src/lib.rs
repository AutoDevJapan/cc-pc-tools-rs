//! Windows capacity-aware ToDo dispatcher, implemented with Rust and Windows APIs.
//!
//! status and orphans --dry-run only measure; step is also called by the hidden Discord tick.
//! General termination requires kill_enabled. Verified no-action recovery is separate.
//! Activity reads only event type/id/status; no payloads, commands or credentials are retained.
pub mod backend;
pub mod admission;
pub mod common;
pub mod dispatch_receipt;
pub mod launch_receipt;
pub mod engine;
pub mod native;
pub mod no_action;
pub mod policy;
pub mod preparation;
pub mod product_review;
mod quota_recovery;
pub mod initial_review;
pub mod registration_resume;
pub mod received_input;
pub mod resume_checkpoint;
pub mod targeted_dispatch;
pub mod unstarted_review;
pub mod unstarted_review_runtime;
pub mod workspace_checkpoint;
pub mod workspace_relocation;
pub use cc_ask::recovery_failure;
pub use cc_job_policy::priority;
pub use cc_job_policy::product_identity;
