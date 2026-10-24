//! Read-only real OAuth metadata check. No model execution or job publication.
//! Only the same fixed aggregate fields used by the launchers reach stdout.
use cc_jobs::{Runtime, platform::System};

fn main() {
    println!("{}", System::default().quota_recovery_inventory());
}
