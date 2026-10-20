use std::fs;

#[test]
fn product_heading_keeps_name_and_count_in_separate_grid_rows() {
    let html = fs::read_to_string(concat!(env!("CARGO_MANIFEST_DIR"), "/src/index.html")).unwrap();
    // Real-browser widths (1024-1920px) were measured separately; this guards the structural cause:
    // a flex row where the long count text squeezed the product name to a few pixels.
    let heading = html.lines().find(|l| l.starts_with(".ops-product-heading{")).unwrap();
    assert!(heading.contains("display:grid") && heading.contains("minmax(0,1fr)"));
    assert!(!heading.contains("display:flex"));
    let name = html.lines().find(|l| l.starts_with(".ops-product-heading h4{")).unwrap();
    assert!(name.contains("grid-row:1") && name.contains("min-width:0"));
    let count = html.lines().find(|l| l.starts_with(".ops-product-heading>.ops-count{")).unwrap();
    assert!(count.contains("grid-row:2") && count.contains("white-space:normal"));
}
