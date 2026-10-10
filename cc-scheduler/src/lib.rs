//! Windows capacity-aware ToDo dispatcher. Python 3.12, stdlib + optional installed psutil.
//! The original owner's documentation is preserved here; this implementation uses Rust and Windows APIs.
//!
//! status and orphans --dry-run only measure; step is also called by the hidden Discord tick.
//! Process termination is disabled until config.json kill_enabled is explicitly enabled.
//! No command lines, environments, event contents, credentials or model API keys are inspected.
pub mod backend;
pub mod common;
pub mod engine;
pub mod native;
pub mod policy;
pub mod preparation;
pub use cc_ask::recovery_failure;
