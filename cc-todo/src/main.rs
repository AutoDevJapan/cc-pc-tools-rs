use clap::Parser;
fn main() {
    if cc_job_policy::product_identity::status_requested(std::env::args().skip(1)) {
        println!("{}", cc_job_policy::product_identity::status());
        return;
    }
    let cli = cc_todo::Cli::parse();
    if let Err(e) = cc_todo::run(
        cli,
        &cc_todo::Paths::from_environment(),
        &mut cc_todo::Discord::production(),
    ) {
        eprintln!("{e}");
        std::process::exit(1);
    }
}
