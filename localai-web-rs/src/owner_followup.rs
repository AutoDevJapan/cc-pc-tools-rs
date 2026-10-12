//! Existing resident's finite, independently enrolled recovery reply observation.
//! Unconfigured installations perform no calls and create no state or tasks.
use serde_json::{json, Value};
use std::path::Path;

pub fn poll(root: &Path) {
    let home = root.join("discord-ask");
    if !home.join("recovery-followup.json").is_file() {
        return;
    }
    let result = crate::command_capture::run(
        &home.join("discord_ask.exe"),
        &["recovery-followup".into(), "tick".into()],
        root,
    );
    let status = observation(result, chrono::Utc::now().to_rfc3339());
    // This status is written by the resident, independently of an agent checking
    // it. Preserve the successful reply evidence if this observation fails.
    let _ = cc_ask::atomic_json(&home.join("recovery-followup-resident.json"), &status, 2);
}
fn observation(result: Result<String, &'static str>, at: String) -> Value {
    match result {
        // The finite CLI emits deadline/HTTP spans before its final JSON line.
        // Only that final line can be evidence; never promote a quoted log row.
        Ok(output) => match serde_json::from_str::<Value>(
            output
                .lines()
                .rfind(|line| !line.trim().is_empty())
                .unwrap_or(""),
        ) {
            Ok(report) if report["domain"] == "new-owner-recovery-followup" => {
                json!({"source":"resident-120s","observed_at":at,"ok":true,"report":report})
            }
            _ => {
                json!({"source":"resident-120s","observed_at":at,"ok":false,"error":"followup_output_invalid"})
            }
        },
        Err(error) => json!({"source":"resident-120s","observed_at":at,"ok":false,"error":error}),
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn absent_enrollment_does_not_restore_legacy_tick_or_create_records() {
        let root = tempfile::tempdir().unwrap();
        poll(root.path());
        assert_eq!(std::fs::read_dir(root.path()).unwrap().count(), 0);
    }
    #[test]
    fn resident_result_requires_the_new_namespace_and_keeps_failure_separate() {
        let at = "2026-10-13T00:00:00Z".to_owned();
        let ok = observation(
            Ok(
                json!({"domain":"new-owner-recovery-followup","scheduled_observations":1})
                    .to_string(),
            ),
            at.clone(),
        );
        assert_eq!(ok["source"], "resident-120s");
        assert_eq!(ok["ok"], true);
        let with_spans=observation(Ok("2026-10-13 tick span start network GET\n{\"domain\":\"new-owner-recovery-followup\",\"scheduled_observations\":1}\n".into()),at.clone());
        assert_eq!(with_spans["ok"], true);
        let misleading = observation(
            Ok("{\"domain\":\"new-owner-recovery-followup\"}\ncommand failed\n".into()),
            at.clone(),
        );
        assert_eq!(misleading["ok"], false);
        assert_eq!(observation(Err("command_timeout"), at.clone())["ok"], false);
        assert_eq!(
            observation(Ok(json!({"domain":"lost-old-questions"}).to_string()), at)["ok"],
            false
        );
    }
}
