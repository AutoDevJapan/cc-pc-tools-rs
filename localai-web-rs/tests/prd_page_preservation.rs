//! The existing Prd root response is the independent acceptance baseline.
//! It was read from port 10012 and matched the renderer and assets at
//! 9a907d39c2f8d0ba810436f78464a4e6f7fa7ebc before changing asset selection.
//! The UI owners (3084/3110/3111) may replace the asset and this baseline only
//! after the owner's image approval for that scope, including shell/workload.
//! Re-measure the approved deployed response; do not bless a candidate hash.
#[test]
fn product_identity_delivery_keeps_the_existing_prd_page() {
    let html = localai_web_rs::console_page::index()
        .replace("\r\n", "\n")
        .replace('\r', "\n");
    let hash = ring::digest::digest(&ring::digest::SHA256, html.as_bytes());
    let actual: String = hash
        .as_ref()
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect();
    assert_eq!(
        actual, "335ad98840c97d0767b8e78bf8166f48c6044a1f2d1a8a25c3bdea831320abf9",
        "the backend identification repair must retain the existing Prd page"
    );
}
