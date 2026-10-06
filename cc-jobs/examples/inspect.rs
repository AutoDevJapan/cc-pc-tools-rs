//! Read-only parity aid: never refreshes a job, sends ratings, or changes state.
//! Usage: inspect.exe <installed-tool-directory> [list|table|summary <id>]
use cc_jobs::*;
use std::{collections::HashSet, io::Write, path::PathBuf};
fn main() {
    let args = std::env::args().skip(1).collect::<Vec<_>>();
    let mut ctx = Context::installed().unwrap();
    ctx.root = PathBuf::from(args.first().expect("installed tool directory required"));
    ctx.jobs = ctx.root.join("jobs");
    let command = args.get(1).map(String::as_str).unwrap_or("list");
    let out = match command {
        "list" => pretty(&json!(ctx.list_jobs(&HashSet::new()))),
        "table" => ctx.fmt_table(&ctx.list_jobs(&HashSet::new())),
        "summary" => ctx.summary(&ctx.load(args.get(2).expect("job ID required")).unwrap()),
        _ => panic!("unknown read-only command"),
    };
    writeln!(TextWriter(std::io::stdout().lock()), "{out}").unwrap();
}
