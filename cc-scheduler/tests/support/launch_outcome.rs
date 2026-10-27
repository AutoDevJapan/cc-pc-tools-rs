use super::*;

fn terminal_same_key(f: &mut Fixture) -> Value {
    f.put(item(1, "civ6", "todo", &[]));
    let prepared = f.e.prepare(&f.get(1), f.repo.to_str().unwrap()).unwrap().unwrap();
    let mut old = job("old-terminal", 999, "failed");
    old["launch_key"] = prepared["scheduler"]["key"].clone();
    old["todo_id"] = json!(1);
    old["cwd"] = prepared["scheduler"]["worktree"].clone();
    old["queue_restart_pending"] = json!(false);
    old["reboot_launch_pending"] = json!(false);
    f.e.backend.records.push(old.clone());
    old
}

#[test]
fn terminal_same_key_no_action_does_not_mark_launched_or_consume_spacing() {
    let mut f = Fixture::new();
    terminal_same_key(&mut f);
    let before = f.e.state.clone();
    f.dispatch();
    assert!(f.e.report["launched"].is_null());
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(f.e.state["last_launch"], before["last_launch"]);
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn terminal_same_key_no_action_allows_next_qualified_candidate() {
    let mut f = Fixture::new();
    terminal_same_key(&mut f);
    f.put(item(2, "other", "todo", &[]));
    f.dispatch();
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(f.get(2)["status"], "doing");
    assert_eq!(f.e.backend.launched.len(), 1);
    assert_eq!(f.e.backend.launched[0]["item"]["id"], 2);
}
