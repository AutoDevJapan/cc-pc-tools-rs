fn main() {
    let root = std::env::current_exe()
        .unwrap()
        .parent()
        .unwrap()
        .to_path_buf();
    let env = std::env::vars().collect();
    if let Err(e) = cc_jobs::cache::finish(&root, &env) {
        eprintln!("cargo cache maintenance: {e}");
        std::process::exit(1);
    }
}
