#![windows_subsystem = "windows"]
use cc_scheduler::received_input::{inspect, parse_request, Host, Layout, ReadError, SystemHost};
use serde_json::json;
fn main() {
    let args: Vec<_> = std::env::args_os().skip(1).collect();
    if args.len() == 1 && (args[0] == "--help" || args[0] == "-h") {
        println!("received_input --request-file <JSON>\nRefetch one registered scheduler input without displaying its body. Fixed local host; no writes, launches or attestations.");
        return;
    }
    let result = (|| {
        if args.len() != 2 || args[0] != "--request-file" {
            return Err(ReadError("input_rejected"));
        }
        if !std::path::Path::new(&args[1]).is_absolute() {
            return Err(ReadError("input_rejected"));
        }
        let mut host = SystemHost::default();
        let bytes = host.read(std::path::Path::new(&args[1]), 64 * 1024)?;
        inspect(&mut host, &Layout::production(), &parse_request(&bytes)?)
    })();
    match result {
        Ok(report) => println!("{report}"),
        Err(error) => {
            println!(
                "{}",
                json!({"domain":"received-input-v1","ok":false,"code":error.0,"saved":false,"launched":0,"asset_qualified":false})
            );
            std::process::exit(1);
        }
    }
}
