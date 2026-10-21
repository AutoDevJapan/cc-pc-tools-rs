//! Windows capacity-aware ToDo dispatcher, implemented with Rust and Windows APIs.
//!
//! status and orphans --dry-run only measure; step is also called by the hidden Discord tick.
//! General termination requires kill_enabled. Verified no-action recovery is separate.
//! Activity reads only event type/id/status; no payloads, commands or credentials are retained.
pub mod backend;
pub mod common;
pub mod dispatch_receipt;
pub mod engine;
pub mod native;
pub mod no_action;
pub mod policy;
pub mod preparation;
pub mod product_review;
pub mod workspace_checkpoint;
pub use cc_ask::recovery_failure;
pub use cc_job_policy::priority;
