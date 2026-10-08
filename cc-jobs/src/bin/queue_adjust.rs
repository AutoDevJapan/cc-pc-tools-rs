#![cfg_attr(windows, windows_subsystem = "windows")]
use cc_jobs::*;
fn main() {
    let result = Context::installed()
        .map_err(Error::from)
        .and_then(|ctx| ctx.adjust_limit());
    if result.is_err() {
        std::process::exit(1);
    }
}
