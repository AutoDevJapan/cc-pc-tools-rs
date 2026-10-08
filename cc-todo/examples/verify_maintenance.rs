//! Finite acceptance probe. Default is completely read-only; one explicit switch
//! edits the existing maintenance summary, with no launch or question creation.
use cc_todo::{Notifier, Paths, recovery};
fn main() -> Result<(), String> {
    let args: Vec<_> = std::env::args().skip(1).collect();
    if !args.is_empty() && args != ["--apply-summary"] {
        return Err("use no arguments or --apply-summary".into());
    }
    let paths = Paths::production();
    let report = recovery::preview_files(&paths, &[57, 791])?;
    println!("{report}");
    if !args.is_empty() {
        let text = recovery::acceptance_summary(&report)?;
        let mut discord = cc_todo::Discord::production();
        discord.report(&paths, 57, "finite-two-item-verification-20261008", &text)?;
        println!("summary_verified");
    }
    Ok(())
}
