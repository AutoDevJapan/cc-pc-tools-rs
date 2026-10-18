//! Test stand-in for gh.exe. Records argv, stdin and the content of every file
//! an argument names (`path`, `--flag=path`, `key=@path`) to $FAKE_GH_RECORD.
use std::io::Read;

/// Non-zero when the gate started this process with a console window.
#[cfg(windows)]
fn console_window() -> usize {
    #[link(name = "kernel32")]
    unsafe extern "system" {
        fn GetConsoleWindow() -> *mut std::ffi::c_void;
    }
    unsafe { GetConsoleWindow() as usize }
}
#[cfg(not(windows))]
fn console_window() -> usize {
    0
}

fn main() {
    let args = std::env::args().skip(1).collect::<Vec<_>>();
    let mut stdin = String::new();
    let _ = std::io::stdin().read_to_string(&mut stdin);
    let mut files = serde_json::Map::new();
    for arg in &args {
        let path = arg
            .rsplit_once("=@")
            .map(|(_, p)| p)
            .or_else(|| {
                arg.strip_prefix("--")
                    .and_then(|a| a.split_once('='))
                    .map(|(_, p)| p)
            })
            .unwrap_or(arg);
        if let Ok(text) = std::fs::read_to_string(path) {
            files.insert(path.to_string(), text.into());
        }
    }
    let record = serde_json::json!({
        "argv": args,
        "stdin": stdin,
        "files": files,
        "depth": std::env::var("CC_GH_GATE_DEPTH").unwrap_or_default(),
        "console_window": console_window(),
    });
    std::fs::write(
        std::env::var("FAKE_GH_RECORD").expect("FAKE_GH_RECORD"),
        record.to_string(),
    )
    .unwrap();
    if std::env::var_os("FAKE_GH_ANSI").is_some() {
        if !args.iter().any(|a| a == "--allow-escape-sequences") {
            eprintln!(
                "the response contains terminal escape sequences; pass --allow-escape-sequences to output it anyway"
            );
            std::process::exit(1);
        }
        println!("\x1b[31msynthetic job log\x1b[0m");
        return;
    }
    println!("fake-gh-out");
    std::process::exit(
        std::env::var("FAKE_GH_EXIT")
            .ok()
            .and_then(|c| c.parse().ok())
            .unwrap_or(0),
    );
}
