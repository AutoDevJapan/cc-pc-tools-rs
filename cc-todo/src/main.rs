use clap::Parser;
fn main() {
    let cli = cc_todo::Cli::parse();
    if let Err(e) = cc_todo::run(
        cli,
        &cc_todo::Paths::from_environment(),
        &mut cc_todo::Discord::production(),
    ) {
        if let Some((code, value)) = remote_outcome(&e) {
            eprintln!("{value}");
            std::process::exit(code);
        }
        eprintln!("{e}");
        std::process::exit(1);
    }
}

fn remote_outcome(error: &str) -> Option<(i32, serde_json::Value)> {
    let kind = cc_job_policy::todo_remote::failure_kind(error)?;
    let message = match kind {
        cc_job_policy::todo_remote::FailureKind::CommitUnconfirmed => {
            "保存結果は未確定です。同じ更新を自動再実行せず、次の同期で保存済みの受付番号を照合します。PC側だけへの保存は行っていません。"
        }
        cc_job_policy::todo_remote::FailureKind::CacheResyncRequired => {
            "Cloudへの保存は確定し、キャッシュの再同期が必要です。次の同期が終わるまで新しい更新は行いません。"
        }
    };
    Some((
        76,
        serde_json::json!({"error":kind.code(),"message":message}),
    ))
}

#[cfg(test)]
mod tests {
    #[test]
    fn unconfirmed_has_distinct_exit_and_generic_domain_errors_keep_legacy_path() {
        let (code, value) =
            super::remote_outcome("todo_store_io: todo_remote_commit_unconfirmed").unwrap();
        assert_eq!(code, 76);
        assert_eq!(value["error"], "todo_remote_commit_unconfirmed");
        assert!(super::remote_outcome("missing required item").is_none());
    }
}
