use cc_jobs::*;
fn main() {
    if cc_job_policy::product_identity::status_requested(std::env::args().skip(1)) {
        println!("{}", cc_job_policy::product_identity::status());
        return;
    }
    use std::io::Write;
    let result = Context::installed().map_err(Error::from).and_then(|ctx| {
        ctx.dispatch(
            &std::env::args().skip(1).collect::<Vec<_>>(),
            &mut TextWriter(std::io::stdout().lock()),
            &mut TextWriter(std::io::stderr().lock()),
        )
    });
    let code = match result {
        Ok(code) => code,
        Err(e) => {
            let _ = writeln!(TextWriter(std::io::stderr().lock()), "{}", e.message);
            e.code
        }
    };
    std::process::exit(code);
}
