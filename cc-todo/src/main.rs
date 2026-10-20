use clap::Parser;
fn main() {
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
