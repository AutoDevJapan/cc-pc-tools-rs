use cc_bookmark_queue::{Entry, Queue, ROOT, Result, serve};
use clap::{Parser, Subcommand};
use std::path::PathBuf;

#[derive(Parser)]
struct Cli {
    #[arg(long, default_value = ROOT, global = true)]
    root: PathBuf,
    #[command(subcommand)]
    command: Command,
}
#[derive(Subcommand)]
enum Command {
    InstallTask,
    Init,
    Add {
        #[arg(long)]
        url: String,
        #[arg(long)]
        name: String,
        #[arg(long)]
        folder: String,
    },
    Pending,
    Serve,
}
fn run() -> Result<()> {
    let cli = Cli::parse();
    let q = Queue::new(&cli.root);
    match cli.command {
        Command::InstallTask => cc_bookmark_queue::task::install(&cli.root)?,
        Command::Init => {
            q.init()?;
            println!("拡張フォルダを準備しました");
        }
        Command::Add { url, name, folder } => {
            let entry = Entry::new(&url, &name, &folder)?;
            println!("{} {}", q.add(&entry)?, entry.id);
        }
        Command::Pending => println!("{}", serde_json::to_string_pretty(&q.pending()?)?),
        Command::Serve => serve(q)?,
    }
    Ok(())
}
fn main() {
    if let Err(e) = run() {
        eprintln!("{e}");
        std::process::exit(1);
    }
}
