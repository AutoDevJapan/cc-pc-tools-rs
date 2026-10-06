fn main() {
    let env = std::env::vars().collect::<std::collections::BTreeMap<_, _>>();
    let dir = env
        .get("CODEX_JOBS_CARGO_LOCK_DIR")
        .filter(|s| !s.is_empty())
        .map(std::path::PathBuf::from)
        .unwrap_or_else(|| {
            std::env::current_exe()
                .unwrap()
                .parent()
                .unwrap()
                .to_path_buf()
        });
    let code = cc_jobs::gate::run(&std::env::args().skip(1).collect::<Vec<_>>(), &dir, &env)
        .unwrap_or_else(|e| {
            eprintln!("cargo gate: {e}");
            1
        });
    std::process::exit(cc_jobs::gate::python_cli_exit(code));
}
