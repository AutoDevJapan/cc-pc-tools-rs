use cc_channel_watch::*;
use serde_json::json;
use std::fs;
use std::path::Path;

#[derive(Default)]
struct FakeTodo {
    added: Vec<Batch>,
    committed: Option<(String, i64)>,
    fail_after_add: bool,
}
impl Todo for FakeTodo {
    fn find(&mut self, marker: &str, _: &str) -> Result<Option<i64>> {
        Ok(self
            .committed
            .as_ref()
            .filter(|(m, _)| m == marker)
            .map(|(_, id)| *id))
    }
    fn add(&mut self, _: &Channel, batch: &Batch) -> Result<i64> {
        self.added.push(batch.clone());
        self.committed = Some((batch.marker.clone(), 123));
        if self.fail_after_add {
            return Err("interrupted after ToDo persisted".into());
        }
        Ok(123)
    }
}
fn channel(root: &Path) -> Channel {
    Channel {
        key: "test".into(),
        name: "テスト".into(),
        url: "https://www.youtube.com/@test".into(),
        knowledge_skill: "test-knowledge".into(),
        report_folder: root.into(),
        report_thread: "123456".into(),
        project: "test".into(),
        seed_files: vec![root.join("old.json")],
    }
}
fn seed(root: &Path) -> (Channel, Ledger) {
    fs::write(
        root.join("old.json"),
        r#"[{"id":"oldVideo001","title":"解析済み"}]"#,
    )
    .unwrap();
    let c = channel(root);
    let ledger = load_ledger(&root.join("ledger.json"), &c).unwrap();
    (c, ledger)
}
fn fixture() -> Vec<Video> {
    videos(&serde_json::from_str(include_str!("fixtures/uploads.json")).unwrap())
}

#[test]
fn detects_new_public_videos_across_tabs_and_deduplicates() {
    let dir = tempfile::tempdir().unwrap();
    let (_, ledger) = seed(dir.path());
    let listed = fixture();
    assert_eq!(listed.len(), 5);
    assert_eq!(
        new_videos(&ledger, &listed)
            .iter()
            .map(|v| v.id.as_str())
            .collect::<Vec<_>>(),
        vec!["newVideo001", "shortVid001"]
    );
}

#[test]
fn ledger_survives_restart_and_noop_day_creates_nothing() {
    let dir = tempfile::tempdir().unwrap();
    let (c, mut ledger) = seed(dir.path());
    let path = dir.path().join("ledger.json");
    let mut todo = FakeTodo::default();
    let first = process_channel(
        &c,
        &path,
        &dir.path().join("specs"),
        &mut ledger,
        &fixture(),
        &mut todo,
    )
    .unwrap();
    assert_eq!(first.new, 2);
    assert_eq!(first.todo_ids, vec![123]);
    assert_eq!(todo.added.len(), 1);
    assert_eq!(ledger.seen.len(), 3);
    assert_eq!(ledger.seen["newVideo001"].todo_id, Some(123));
    let prompt = fs::read_to_string(&todo.added[0].spec).unwrap();
    assert!(prompt.contains("newVideo001"));
    assert!(prompt.contains("shortVid001"));
    assert!(!prompt.contains("oldVideo001"));
    assert!(prompt.contains("--notifyなし"));
    assert!(prompt.contains("資金移動"));
    assert!(prompt.contains("log --topic \"テスト\""));
    assert!(!prompt.contains("post --thread"));
    assert!(prompt.contains("動画の題名・URL"));
    let mut restored = load_ledger(&path, &c).unwrap();
    let second = process_channel(
        &c,
        &path,
        &dir.path().join("specs"),
        &mut restored,
        &fixture(),
        &mut todo,
    )
    .unwrap();
    assert_eq!(second.new, 0);
    assert!(second.todo_ids.is_empty());
    assert_eq!(todo.added.len(), 1);
}

#[test]
fn members_only_is_not_queued_and_is_noted_even_on_member_only_day() {
    let dir = tempfile::tempdir().unwrap();
    let (c, mut ledger) = seed(dir.path());
    let mut todo = FakeTodo::default();
    let listed = videos(
        &json!({"entries":[{"id":"memberVid01","title":"会員限定","availability":"subscriber_only"}]}),
    );
    let out = process_channel(
        &c,
        &dir.path().join("ledger.json"),
        &dir.path().join("specs"),
        &mut ledger,
        &listed,
        &mut todo,
    )
    .unwrap();
    assert_eq!(out.skipped, 1);
    assert_eq!(out.new, 0);
    assert!(todo.added.is_empty());
    assert!(!ledger.seen.contains_key("memberVid01"));
    assert_eq!(
        ledger.skipped["memberVid01"].reason,
        "会員限定のため本文未解析"
    );
    assert!(!dir.path().join("specs").exists());
}

#[test]
fn member_video_becoming_public_is_detected() {
    let dir = tempfile::tempdir().unwrap();
    let (c, mut ledger) = seed(dir.path());
    let mut todo = FakeTodo::default();
    let path = dir.path().join("ledger.json");
    let restricted =
        videos(&json!({"entries":[{"id":"memberVid01","availability":"subscriber_only"}]}));
    process_channel(
        &c,
        &path,
        &dir.path().join("specs"),
        &mut ledger,
        &restricted,
        &mut todo,
    )
    .unwrap();
    let public = videos(&json!({"entries":[{"id":"memberVid01","availability":"public"}]}));
    let out = process_channel(
        &c,
        &path,
        &dir.path().join("specs"),
        &mut ledger,
        &public,
        &mut todo,
    )
    .unwrap();
    assert_eq!(out.new, 1);
    assert_eq!(todo.added.len(), 1);
    assert!(!ledger.skipped.contains_key("memberVid01"));
}

#[test]
fn interrupted_add_is_reconciled_without_duplicate_or_lost_video() {
    let dir = tempfile::tempdir().unwrap();
    let (c, mut ledger) = seed(dir.path());
    let path = dir.path().join("ledger.json");
    let mut todo = FakeTodo {
        fail_after_add: true,
        ..Default::default()
    };
    assert!(
        process_channel(
            &c,
            &path,
            &dir.path().join("specs"),
            &mut ledger,
            &fixture(),
            &mut todo
        )
        .is_err()
    );
    assert_eq!(ledger.seen.len(), 1);
    assert!(ledger.pending.is_some());
    let mut restored = load_ledger(&path, &c).unwrap();
    let out = process_channel(
        &c,
        &path,
        &dir.path().join("specs"),
        &mut restored,
        &fixture(),
        &mut todo,
    )
    .unwrap();
    assert_eq!(out.todo_ids, vec![123]);
    assert_eq!(todo.added.len(), 1);
    assert!(restored.pending.is_none());
    assert_eq!(restored.seen.len(), 3);
}

#[test]
fn seed_reads_existing_json_and_markdown_without_seeding_members() {
    let dir = tempfile::tempdir().unwrap();
    let mut c = channel(dir.path());
    fs::write(
        &c.seed_files[0],
        r#"[{"id":"oldVideo001"},{"id":"memberVid01","access":"subscriber_only"}]"#,
    )
    .unwrap();
    fs::write(dir.path().join("index.md"),"[動画](https://www.youtube.com/watch?v=shortVid001)\n[会員](https://www.youtube.com/watch?v=memberVid01)").unwrap();
    c.seed_files.push(dir.path().join("index.md"));
    let path = dir.path().join("ledger.json");
    let ledger = load_ledger(&path, &c).unwrap();
    assert_eq!(
        ledger.seen.keys().map(String::as_str).collect::<Vec<_>>(),
        vec!["oldVideo001", "shortVid001"]
    );
    assert_eq!(ledger.skipped.len(), 1);
    // Later report updates must not overwrite the established ledger.
    fs::write(&c.seed_files[0], "[]").unwrap();
    assert_eq!(load_ledger(&path, &c).unwrap().seen.len(), 2);
}

#[test]
fn corrupt_or_missing_baseline_is_an_error_instead_of_full_channel_dispatch() {
    let dir = tempfile::tempdir().unwrap();
    let c = channel(dir.path());
    assert!(load_ledger(&dir.path().join("ledger.json"), &c).is_err());
    fs::write(&c.seed_files[0], "[]").unwrap();
    assert!(load_ledger(&dir.path().join("ledger.json"), &c).is_err());
    fs::write(dir.path().join("ledger.json"), "invalid JSON").unwrap();
    assert!(load_ledger(&dir.path().join("ledger.json"), &c).is_err());
}
