//! Residents must be launched by Task Scheduler, never inherit a Codex job token/environment.
use std::process::Command;

pub const TASK: &str = "local-ai-web-autostart-10012";
pub fn inherited_job<'a>(vars: impl IntoIterator<Item = (&'a str, &'a str)>) -> bool {
    vars.into_iter().any(|(key, value)| {
        !value.is_empty()
            && matches!(
                key,
                "CODEX_SESSION_ID" | "CODEX_THREAD_ID" | "CODEX_SANDBOX"
            )
    })
}
pub fn guard() -> Result<(), String> {
    // Preview binds only in a disposable verification run and launches no residents.
    if isolated_preview(
        std::env::var("LOCALAI_WEB_PREVIEW").as_deref().ok(),
        std::env::var("LOCALAI_WEB_PORT").as_deref().ok(),
    ) {
        return Ok(());
    }
    let vars: Vec<_> = std::env::vars().collect();
    if inherited_job(vars.iter().map(|(k, v)| (k.as_str(), v.as_str()))) {
        return Err(format!(
            "Codexジョブ内から常駐コンソールを直接起動できません。schtasks /Run /TN {TASK} を使ってください。"
        ));
    }
    Ok(())
}
fn isolated_preview(preview: Option<&str>, port: Option<&str>) -> bool {
    preview == Some("1")
        && port
            .and_then(|p| p.parse::<u16>().ok())
            .is_some_and(|p| p != 10012 && p != 10013 && p != 0)
}
pub fn restart_args() -> [&'static str; 3] {
    ["/Run", "/TN", TASK]
}
pub fn ensure_scheduled_launcher() -> Result<(), String> {
    let mut command = Command::new("schtasks.exe");
    command.args(restart_args());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    let out = command.output().map_err(|e| e.to_string())?;
    if !out.status.success() {
        return Err(format!(
            "定期タスクの起動に失敗しました（終了コード {:?}）",
            out.status.code()
        ));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn codex_job_environment_is_refused_but_scheduler_and_npm_install_are_allowed() {
        for key in ["CODEX_SESSION_ID", "CODEX_THREAD_ID", "CODEX_SANDBOX"] {
            assert!(inherited_job([(key, "job")]));
        }
        assert!(!inherited_job([
            ("CODEX_SESSION_ID", ""),
            ("CODEX_MANAGED_BY_NPM", "1")
        ]));
        assert!(!inherited_job([]));
    }
    #[test]
    fn restart_uses_only_the_existing_scheduled_task() {
        assert_eq!(
            restart_args(),
            ["/Run", "/TN", "local-ai-web-autostart-10012"]
        );
    }
    #[test]
    fn preview_cannot_bypass_the_guard_on_a_resident_port() {
        assert!(isolated_preview(Some("1"), Some("19012")));
        for port in [
            None,
            Some("10012"),
            Some("10013"),
            Some("0"),
            Some("invalid"),
        ] {
            assert!(!isolated_preview(Some("1"), port));
        }
    }
}
