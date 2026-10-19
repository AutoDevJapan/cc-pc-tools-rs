#[cfg(test)]
mod tests {
    use std::io::{Cursor, Write};
    use zip::{write::SimpleFileOptions, ZipArchive, ZipWriter};

    // Match the product's updater registration without creating a window or app runtime.
    #[test]
    fn updater_registration_api_is_compatible() {
        let _builder = tauri::Builder::<tauri::Wry>::default()
            .plugin(tauri_plugin_updater::Builder::new().build());
    }

    fn archive(name: &str) -> Cursor<Vec<u8>> {
        let mut writer = ZipWriter::new(Cursor::new(Vec::new()));
        writer.start_file(name, SimpleFileOptions::default()).unwrap();
        writer.write_all(b"verified updater fixture").unwrap();
        let mut result = writer.finish().unwrap();
        result.set_position(0);
        result
    }

    #[test]
    fn zip_extracts_stored_update_payload() {
        let dest = tempfile::tempdir().unwrap();
        let mut reader = ZipArchive::new(archive("nested/fixture.txt")).unwrap();
        reader.extract(dest.path()).unwrap();
        assert_eq!(std::fs::read(dest.path().join("nested/fixture.txt")).unwrap(),
                   b"verified updater fixture");
    }

    #[test]
    fn zip_rejects_parent_traversal_without_writing_outside() {
        let root = tempfile::tempdir().unwrap();
        let dest = root.path().join("destination");
        std::fs::create_dir(&dest).unwrap();
        let mut reader = ZipArchive::new(archive("../escape.txt")).unwrap();
        assert!(reader.extract(&dest).is_err());
        assert!(!root.path().join("escape.txt").exists());
    }

    #[test]
    fn plist_xml_and_binary_roundtrip() {
        let mut entries = plist::Dictionary::new();
        entries.insert("CFBundleIdentifier".into(), plist::Value::String("fixture.test".into()));
        let original = plist::Value::Dictionary(entries);
        for binary in [false, true] {
            let mut encoded = Vec::new();
            if binary { original.to_writer_binary(&mut encoded).unwrap(); }
            else { original.to_writer_xml(&mut encoded).unwrap(); }
            assert_eq!(plist::Value::from_reader(Cursor::new(encoded)).unwrap(), original);
        }
    }

    // Both versions are present in the actual product lock; default TLS verification stays enabled.
    #[test]
    fn reqwest_versions_complete_verified_https() {
        let endpoint = "https://crates.io/api/v1/crates/zip/9.0.5";
        let v12 = reqwest12::blocking::Client::builder()
            .timeout(std::time::Duration::from_secs(20)).build().unwrap();
        assert!(v12.get(endpoint).header("User-Agent", "PassShare-dependency-acceptance")
            .send().unwrap().status().is_success());
        let v13 = reqwest13::blocking::Client::builder()
            .timeout(std::time::Duration::from_secs(20)).build().unwrap();
        assert!(v13.get(endpoint).header("User-Agent", "PassShare-dependency-acceptance")
            .send().unwrap().status().is_success());
    }
}