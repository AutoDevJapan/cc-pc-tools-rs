use localai_web_rs::operations::{metadata_display_snapshot, Operations};
use serde_json::json;
use std::fs;

#[test]
fn display_panels_reuse_the_same_protected_workforce_generation_after_sources_disappear() {
    let temp = tempfile::tempdir().unwrap();
    let root = temp.path();
    fs::create_dir_all(root.join("todo")).unwrap();
    fs::create_dir_all(root.join("codex-jobs/jobs")).unwrap();
    fs::write(root.join("todo/todo.json"), json!({"projects":{"tools":"D:/fixture"},"items":[
        {"id":3050,"project":"tools","title":"fixture-private-title","status":"doing","progress":null},
        {"id":3051,"project":"tools","status":"wait","wait_on":[3050],"progress":0}
    ]}).to_string()).unwrap();
    let board = Operations::new(root.into(), root.join("skills"));
    board.refresh();
    let snapshot = board.snapshot();
    assert_eq!(snapshot["metadataOk"], true);
    assert_eq!(snapshot["workforce"]["inventory"]["taskRecords"], 2);
    assert_eq!(snapshot["workforce"]["states"]["dependency_wait"], 1);
    assert!(snapshot["workforce"]["processing"]["verifiedCount"].is_null());
    fs::remove_file(root.join("todo/todo.json")).unwrap();
    for kind in ["todo", "jobs", "handoff"] {
        let panel = metadata_display_snapshot(&snapshot, kind).unwrap();
        assert_eq!(panel["fetchedAt"], snapshot["fetchedAt"]);
        assert_eq!(panel["workforce"], snapshot["workforce"]);
        assert!(!panel.to_string().contains("fixture-private-title"));
    }
    board.refresh();
    let failed = board.snapshot();
    assert_eq!(failed["metadataOk"], false);
    assert_eq!(failed["workforce"]["state"], "unavailable");
    assert!(failed["workforce"]["inventory"]["taskRecords"].is_null());
    assert_eq!(failed["previousDataAt"], snapshot["fetchedAt"]);
    assert_eq!(
        failed["departments"][2]["products"][0]["workers"]
            .as_array()
            .unwrap()
            .len(),
        2
    );
    assert!(metadata_display_snapshot(&failed, "todo").is_err());
}
