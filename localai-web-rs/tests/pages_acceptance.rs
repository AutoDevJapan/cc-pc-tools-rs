//! Run the real compiled page-script regressions with their lightweight Go driver.
#[test]
fn console_pages_navigation_loading_and_unknown_values() {
    let mut command = std::process::Command::new("go");
    command
        .args(["test", "-count=1", "-timeout=60s", "./..."])
        .current_dir(
            std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../tools/console-pages"),
        );
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    let output = command
        .output()
        .expect("Go and Node run the page acceptance tests");
    assert!(
        output.status.success(),
        "{}\n{}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );
}
