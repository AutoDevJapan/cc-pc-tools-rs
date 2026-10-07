use cc_channel_watch::*;
use serde_json::json;

#[test]
fn numeric_snapshot_excludes_unknown_counts_members_and_duplicate_tabs() {
    let c: Channel=serde_json::from_value(json!({"key":"xb","name":"XB Bittensor","url":"https://www.youtube.com/@xb-bittensor","knowledge_skill":"xb-channel-knowledge","report_folder":".","project":"channel-xb","seed_files":["old.json"]})).unwrap();
    let value = json!({"entries":[{"entries":[{"id":"public00001","title":"新しい動画","view_count":12},{"id":"unknown0001","title":"数値なし"},{"id":"member00001","availability":"subscriber_only","view_count":999}]},{"entries":[{"id":"public00001","view_count":12},{"id":"public00002","view_count":0}]}]});
    let text = metrics_report(&c, &value, "2026-10-07T21:00:00+09:00").unwrap();
    assert!(text.contains("2本の累計: 12回"));
    assert!(text.contains("新しい動画: 12回"));
    assert!(text.contains("watch?v=public00001"));
    assert!(text.contains("2026-10-07T21:00:00+09:00"));
    assert!(!text.contains("member00001"));
    assert!(metrics_report(&c, &json!({"entries":[{"id":"unknown0001"}]}), "now").is_none());
}

#[test]
fn failed_measurements_are_coalesced_and_only_one_report_is_sent_per_day() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("reports.json");
    assert!(
        queue_report(&path, Some("前回の測定"), "2026-10-06", |_| Err(
            "Discord down".into()
        ))
        .is_err()
    );
    let mut sent = Vec::new();
    queue_report(&path, Some("今回の測定"), "2026-10-07", |body| {
        sent.push(body.to_owned());
        Ok(())
    })
    .unwrap();
    assert_eq!(sent, vec!["今回の測定"]);
    queue_report(
        &path,
        Some("同日の新しい測定"),
        "2026-10-07",
        |_| panic!("second report today"),
    )
    .unwrap();
    queue_report(&path, Some("翌日の測定"), "2026-10-08", |body| {
        sent.push(body.to_owned());
        Ok(())
    })
    .unwrap();
    assert_eq!(sent, vec!["今回の測定", "翌日の測定"]);
    queue_report(&path, None, "2026-10-08", |_| panic!("already sent")).unwrap();
}
