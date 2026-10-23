//! The existing Prd root response is the independent acceptance baseline.
//! It was read from port 10012 and matched the renderer and assets at
//! 9a907d39c2f8d0ba810436f78464a4e6f7fa7ebc before changing asset selection.
//! PR302 remains a backend-only repair with the original rendered baseline.
//! The integrated PR300 also owns the explicit page repairs and ToDo3145's
//! approved Systems v2. Preserve the original asset independently; validate
//! actual navigation and the Operations structure here, then measure the new
//! Prd response separately at deployment. A candidate hash is not Prd evidence.
#[test]
fn approved_page_repair_retains_the_original_asset_and_operations_structure() {
    let original = include_str!("../src/index_prd.html")
        .replace("\r\n", "\n")
        .replace('\r', "\n");
    let hash = ring::digest::digest(&ring::digest::SHA256, original.as_bytes());
    let actual: String = hash
        .as_ref()
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect();
    assert_eq!(
        actual, "4afc4120f1315afa716b0eeea81e149cf31d4353903ecaeff224976bfe3be9ff",
        "retain the independent media-first asset; do not rewrite its evidence"
    );
    let html = localai_web_rs::console_page::index();
    for anchor in [
        "id=\"view-kanban\" class=\"view active operations\"",
        "id=\"ops-commander\"",
        "id=\"ops-departments\"",
        "id=\"ops-dialog\"",
        "id=\"view-progress\"",
        "id=\"view-workload\"",
        "id=\"systems-resources\"",
    ] {
        assert!(
            html.contains(anchor),
            "missing existing hierarchy/navigation: {anchor}"
        );
    }
    for retired in ["id=\"view-chat\"", "id=\"view-minimax\""] {
        assert!(
            !html.contains(retired),
            "retired page must not load: {retired}"
        );
    }
    let links = regex::Regex::new(r#"<a\b[^>]*href="([^"]+)"[^>]*data-view="([^"]+)""#).unwrap();
    let found: Vec<_> = links
        .captures_iter(&html)
        .map(|c| (c[2].to_owned(), c[1].to_owned()))
        .collect();
    assert!(found.contains(&("progress".into(), "/progress".into())));
    assert!(found.contains(&("music".into(), "/music".into())));
    for view in ["kanban", "workload", "systems"] {
        assert!(html.contains(&format!("<button data-view=\"{view}\"")));
    }
}
