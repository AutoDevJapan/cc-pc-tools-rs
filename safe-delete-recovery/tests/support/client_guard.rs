// CI-only protocol fixture. Production always uses the installed scrub391.exe.
fn main() {
    let denied = std::env::args().any(|arg| arg.contains("fixture-client-denied"));
    println!("{{\"client_resource\":{denied}}}");
}
