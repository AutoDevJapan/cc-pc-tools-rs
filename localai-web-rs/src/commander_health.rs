//! Observed patrol health, independently of workers and resource capacity.
use serde_json::{Value, json};
pub fn project(record: Option<&Value>, queue: &Value, now: u64, failed_read: bool) -> Value {
    let seconds = |v: &Value| {
        v.as_u64()
            .filter(|v| *v > 0)
            .and_then(|v| v.checked_mul(1000))
    };
    let recent = |at: Option<u64>, age| {
        at.is_some_and(|at| at <= now + 60_000 && now.saturating_sub(at) <= age)
    };
    let completed = if let Some(record) = record {
        seconds(&record["finished_at"])
    } else {
        seconds(&queue["last_tick"])
    };
    let started = record.and_then(|v| seconds(&v["started_at"]));
    let running =
        started.is_some_and(|at| completed.is_none_or(|done| at > done)) && recent(started, 90_000);
    let error = record.unwrap_or(queue)[if record.is_some() {
        "error"
    } else {
        "last_error"
    }]
    .as_str()
    .filter(|e| !e.is_empty());
    let (status, detail) = if failed_read {
        ("attention", "司令塔の巡回記録を読み取れません。")
    } else if error.is_some() {
        (
            "attention",
            "巡回で異常を検出しました。既存の常駐処理が次回の巡回で再確認します。",
        )
    } else if running {
        (
            "working",
            "常駐の巡回処理を実行中です。完了結果を確認しています。",
        )
    } else if recent(completed, 180_000) {
        (
            "working",
            "常駐の巡回完了を確認しました。配下の状態は各作業に表示します。",
        )
    } else if completed.is_some() || started.is_some() {
        (
            "attention",
            "巡回記録の更新が途切れています。待機や休止とは区別して表示します。",
        )
    } else {
        ("unknown", "司令塔の巡回状態は未取得です。")
    };
    json!({"id":"commander","name":"司令塔","status":status,"detail":detail,
        "startedAt":started,"completedAt":completed,"state":if status=="working"{"connected"}else if status=="unknown"{"unknown"}else{"unavailable"}})
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn distinguishes_unobserved_failure_expiry_and_actual_running_patrol() {
        let now = 1_000_000;
        assert_eq!(
            project(
                Some(&json!({"started_at":900})),
                &json!({"last_tick":920}),
                now,
                false
            )["status"],
            "attention"
        );
        assert_eq!(project(None, &json!({}), now, false)["status"], "unknown");
        assert_eq!(project(None, &json!({}), now, true)["status"], "attention");
        assert_eq!(
            project(None, &json!({"last_tick":999}), now, false)["status"],
            "working"
        );
        assert_eq!(
            project(None, &json!({"last_tick":700}), now, false)["status"],
            "attention"
        );
        assert_eq!(
            project(
                None,
                &json!({"last_tick":999,"last_error":"tool_failed"}),
                now,
                false
            )["status"],
            "attention"
        );
        assert_eq!(
            project(
                Some(&json!({"started_at":998})),
                &json!({"last_tick":700}),
                now,
                false
            )["status"],
            "working"
        );
        assert_eq!(
            project(Some(&json!({"started_at":700})), &json!({}), now, false)["status"],
            "attention"
        );
        assert_eq!(
            project(None, &json!({"last_tick":u64::MAX}), now, false)["status"],
            "unknown"
        );
    }
}
