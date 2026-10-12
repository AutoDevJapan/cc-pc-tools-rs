//! Explicit task facts. Process activity and record modification are not results.
use serde_json::{json, Value};

fn entry<'a>(item: &'a Value, key: &str, label: &str) -> &'a str {
    if let Some(value) = item["work_status"][key]
        .as_str()
        .filter(|s| !s.trim().is_empty())
    {
        return value.trim();
    }
    item["note"]
        .as_str()
        .unwrap_or("")
        .lines()
        .find_map(|line| {
            let line = line.trim();
            line.strip_prefix(label)
                .and_then(|tail| tail.strip_prefix(':').or_else(|| tail.strip_prefix('：')))
                .map(str::trim)
                .filter(|s| !s.is_empty())
        })
        .unwrap_or("")
}

fn time(raw: &str) -> Option<String> {
    chrono::DateTime::parse_from_rfc3339(raw)
        .ok()
        .map(|t| t.to_rfc3339())
        .or_else(|| {
            ["%Y-%m-%d %H:%M:%S", "%Y-%m-%d %H:%M"]
                .iter()
                .find_map(|format| {
                    chrono::NaiveDateTime::parse_from_str(raw, format)
                        .ok()?
                        .and_local_timezone(chrono::FixedOffset::east_opt(9 * 3600)?)
                        .single()
                        .map(|t| t.to_rfc3339())
                })
        })
}

/// Callers establish privacy first and pass their existing text sanitizer.
pub fn project(item: &Value, private: bool, clean: impl Fn(&str) -> String) -> Value {
    if private {
        return json!({"summary":"", "lastResult":"内容は非表示", "lastResultAt":null,
            "currentStage":"内容は非表示", "stopReason":"内容は非表示", "nextAction":"内容は非表示",
            "dependencySummary":"内容は非表示", "dependencies":[]});
    }
    let short = |raw: &str| clean(raw).chars().take(180).collect::<String>();
    let summary = entry(item, "summary", "要約");
    let result = entry(item, "last_result", "最終成果");
    let result = if matches!(
        result.to_ascii_lowercase().as_str(),
        "running" | "thinking" | "実行中" | "稼働中" | "考え中" | "思考中"
    ) {
        ""
    } else {
        result
    };
    // Require a recorded achievement as well as its timestamp. Never substitute
    // updated, finished_at, last_event_at, running/thinking or token counts.
    let result_at = if result.is_empty() {
        None
    } else {
        time(entry(item, "last_result_at", "最終成果の時刻"))
    };
    let dependencies: Vec<_> = item["wait_on"]
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(Value::as_u64)
        .filter(|id| *id > 0)
        .collect();
    let dependency = entry(item, "dependencies", "依存先");
    let dependency = if !dependencies.is_empty() {
        dependencies
            .iter()
            .map(|id| format!("ToDo #{id}"))
            .collect::<Vec<_>>()
            .join("、")
    } else if dependency.is_empty() {
        "未記録".into()
    } else {
        short(dependency)
    };
    let reason = entry(item, "stop_reason", "停止理由");
    let reason = if reason.is_empty() {
        match item["status"].as_str().unwrap_or("") {
            "block" => "オーナーの判断待ち".into(),
            "wait" => format!("依存する作業の完了待ち: {dependency}"),
            _ => "未記録".into(),
        }
    } else {
        short(reason)
    };
    let or_unknown = |raw: &str| {
        if raw.is_empty() {
            "未記録".into()
        } else {
            short(raw)
        }
    };
    json!({"summary":short(summary), "lastResult":or_unknown(result), "lastResultAt":result_at,
        "currentStage":or_unknown(entry(item,"stage","現在の工程")), "stopReason":reason,
        "nextAction":or_unknown(entry(item,"next_action","次の行動")),
        "dependencySummary":dependency, "dependencies":dependencies})
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn recorded_facts_are_projected_without_counting_activity_as_achievement() {
        let item = json!({"status":"doing","updated":"2026-10-10 15:00","activity":"thinking",
            "note":"要約: 一覧で作業状況を読めるようにする\n最終成果: 音声の範囲取得検査が成功\n最終成果の時刻: 2026-10-09T23:17:56+09:00\n現在の工程: 一覧の項目を実装\n停止理由: なし\n次の行動: 関連検査を実行する\n依存先: なし"});
        let value = project(&item, false, str::to_owned);
        assert_eq!(value["lastResultAt"], "2026-10-09T23:17:56+09:00");
        assert_eq!(value["currentStage"], "一覧の項目を実装");
        assert_eq!(value["dependencySummary"], "なし");
        assert!(!value.to_string().contains("thinking"));
        let unrecorded = project(
            &json!({"status":"running","updated":"2026-10-10 15:00","finished_at":"2026-10-10T15:00:00+09:00","activity":"thinking"}),
            false,
            str::to_owned,
        );
        assert!(unrecorded["lastResultAt"].is_null());
        assert_eq!(unrecorded["lastResult"], "未記録");
        assert_eq!(unrecorded["currentStage"], "未記録");
    }
    #[test]
    fn dependencies_and_privacy_override_untrusted_note_claims() {
        let item = json!({"status":"wait","wait_on":[0,2155,"bad"],"note":"依存先: なし", "work_status":{"summary":"秘密","last_result":"秘密","last_result_at":"2026-10-10T10:00:00Z","stage":"検査"}});
        let value = project(&item, false, str::to_owned);
        assert_eq!(value["dependencies"], json!([2155]));
        assert_eq!(value["dependencySummary"], "ToDo #2155");
        assert!(value["stopReason"].as_str().unwrap().contains("2155"));
        let hidden = project(&item, true, |_| {
            panic!("private text must not reach sanitizer")
        });
        assert!(!hidden.to_string().contains("秘密"));
        assert!(hidden["lastResultAt"].is_null());
    }
    #[test]
    fn invalid_or_orphan_timestamp_never_becomes_a_result() {
        assert_eq!(
            project(
                &json!({"note":"実行中・thinking・起動しました"}),
                false,
                str::to_owned
            )["summary"],
            ""
        );
        for note in [
            "最終成果の時刻: 2026-10-10T10:00:00Z",
            "最終成果: 検査\n最終成果の時刻: now",
            "最終成果: thinking\n最終成果の時刻: 2026-10-10T10:00:00Z",
        ] {
            assert!(project(&json!({"note":note}), false, str::to_owned)["lastResultAt"].is_null());
        }
    }
}
