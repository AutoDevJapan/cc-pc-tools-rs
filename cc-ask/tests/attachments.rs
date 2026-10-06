mod common;
use cc_ask::{attachments::*, commands::*, core::*};
use common::{Harness, SharedFake};
use serde_json::{json, Value};
use std::{cell::RefCell, io::Write, path::PathBuf, rc::Rc};

#[derive(Default)]
struct Capture {
    uploads: Vec<(String, String, Vec<u8>)>,
    runs: Vec<Vec<String>>,
    tier: u64,
    tier_error: bool,
    media_sizes: Vec<u64>,
    duration: f64,
    real_media: bool,
    media_error: bool,
    fail_upload: Option<usize>,
}
struct FakeHttp {
    base: SharedFake,
    capture: Rc<RefCell<Capture>>,
}
impl Effects for FakeHttp {
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        if url.ends_with("/guilds/G") {
            let c = self.capture.borrow();
            return if c.tier_error {
                Err("guild unavailable".into())
            } else {
                Ok(json!({"premium_tier":c.tier}))
            };
        }
        self.base.http(method, url, body, discord)
    }
    fn multipart(&mut self, url: &str, content_type: &str, body: &[u8]) -> Result<Value> {
        if self.capture.borrow().fail_upload == Some(self.capture.borrow().uploads.len()) {
            return Err("mock upload unavailable".into());
        }
        let payload = payload(body);
        self.capture
            .borrow_mut()
            .uploads
            .push((url.into(), content_type.into(), body.to_vec()));
        self.base.http("POST", url, Some(&payload), true)
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        if argv[0] == "git" {
            return self.base.run(argv, timeout);
        }
        let mut c = self.capture.borrow_mut();
        c.runs.push(argv.to_vec());
        if c.real_media {
            drop(c);
            return run_hidden(argv, timeout);
        }
        if c.media_error {
            return Ok(Output {
                code: 1,
                ..Default::default()
            });
        }
        if argv[0] == "ffprobe" {
            return Ok(Output {
                stdout: json!({"format":{"duration":c.duration.to_string()}}).to_string(),
                ..Default::default()
            });
        }
        let size = if c.media_sizes.is_empty() {
            100
        } else {
            c.media_sizes.remove(0)
        };
        let f = std::fs::File::create(argv.last().unwrap()).unwrap();
        f.set_len(size).unwrap();
        Ok(Output::default())
    }
}
fn payload(raw: &[u8]) -> Value {
    let start = raw.windows(4).position(|b| b == b"\r\n\r\n").unwrap() + 4;
    let end = raw[start..]
        .windows(4)
        .position(|b| b == b"\r\n--")
        .unwrap()
        + start;
    serde_json::from_slice(&raw[start..end]).unwrap()
}
fn fixture() -> (Harness, Rc<RefCell<Capture>>) {
    let mut h = Harness::new();
    h.fake.borrow_mut().threads.get_mut("100").unwrap()["guild_id"] = json!("G");
    let c = Rc::new(RefCell::new(Capture {
        duration: 300.,
        ..Default::default()
    }));
    h.ctx.effects = Box::new(FakeHttp {
        base: SharedFake(h.fake.clone()),
        capture: c.clone(),
    });
    (h, c)
}
fn attachment(h: &Harness, name: &str, size: u64) -> PathBuf {
    let p = h.root.path().join(name);
    let mut file = std::fs::File::create(&p).unwrap();
    file.write_all(b"sample attachment").unwrap();
    file.set_len(size).unwrap();
    p
}
fn body(h: &Harness, text: &str) -> PathBuf {
    let p = h.root.path().join("body.md");
    std::fs::write(&p, text).unwrap();
    p
}

#[test]
fn multipart_exact_fields_binary_bytes_and_silent_post() {
    let (mut h, c) = fixture();
    let p = h.root.path().join("日本語.png");
    let bytes = b"\x89PNG\r\n\0\xffimage";
    std::fs::write(&p, bytes).unwrap();
    let b = body(&h, "添付の確認です。 @everyone");
    let result = cmd_post(&mut h.ctx, "100", &b, &[p], false).unwrap();
    assert_eq!(result["message_ids"], json!(["1001"]));
    let c = c.borrow();
    assert_eq!(c.uploads.len(), 1);
    let (url, content_type, raw) = &c.uploads[0];
    assert_eq!(url, "https://discord.com/api/v10/channels/100/messages");
    let boundary = content_type
        .strip_prefix("multipart/form-data; boundary=")
        .unwrap();
    let metadata = payload(raw);
    assert_eq!(metadata["flags"], SILENT);
    assert_eq!(metadata["allowed_mentions"], json!({"parse":[]}));
    assert_eq!(
        metadata["attachments"],
        json!([{"id":0,"filename":"日本語.png"}])
    );
    assert!(raw.windows(bytes.len()).any(|s| s == bytes));
    let text = String::from_utf8_lossy(raw);
    assert_eq!(text.matches("name=\"payload_json\"").count(), 1);
    assert_eq!(
        text.matches("name=\"files[0]\"; filename=\"日本語.png\"")
            .count(),
        1
    );
    assert!(text.contains("Content-Type: image/png\r\n\r\n"));
    assert!(raw.ends_with(format!("--{boundary}--\r\n").as_bytes()));
}

#[test]
fn twenty_one_files_split_ten_ten_one_with_local_attachment_ids() {
    let (mut h, c) = fixture();
    let files: Vec<_> = (0..21)
        .map(|i| attachment(&h, &format!("file{i}.txt"), 30))
        .collect();
    let b = body(&h, "");
    cmd_post(&mut h.ctx, "100", &b, &files, false).unwrap();
    let c = c.borrow();
    assert_eq!(c.uploads.len(), 3);
    for (upload, count) in c.uploads.iter().zip([10, 10, 1]) {
        let p = payload(&upload.2);
        assert_eq!(p["attachments"].as_array().unwrap().len(), count);
        for (i, a) in p["attachments"].as_array().unwrap().iter().enumerate() {
            assert_eq!(a["id"], i);
        }
        assert_eq!(p["flags"], SILENT);
    }
    let names: Vec<_> = c
        .uploads
        .iter()
        .flat_map(|u| {
            payload(&u.2)["attachments"]
                .as_array()
                .unwrap()
                .iter()
                .map(|a| s(&a["filename"]).to_owned())
                .collect::<Vec<_>>()
        })
        .collect();
    assert_eq!(
        names,
        (0..21).map(|i| format!("file{i}.txt")).collect::<Vec<_>>()
    );
}

#[test]
fn forum_ask_nests_metadata_and_saves_last_attachment_message() {
    let (mut h, c) = fixture();
    let files: Vec<_> = (0..11)
        .map(|i| attachment(&h, &format!("f{i}.pdf"), 30))
        .collect();
    let b = body(&h, "方針を教えてください。");
    let args = AskArgs {
        title: "質問".into(),
        body_file: b,
        attach: files,
        ..Default::default()
    };
    cmd_ask(&mut h.ctx, &args).unwrap();
    let c = c.borrow();
    assert_eq!(c.uploads.len(), 2);
    assert!(c.uploads[0].0.ends_with("/channels/T/threads"));
    let first = payload(&c.uploads[0].2);
    assert!(first.get("attachments").is_none());
    assert_eq!(
        first["message"]["attachments"].as_array().unwrap().len(),
        10
    );
    assert_eq!(
        first["message"]["allowed_mentions"],
        json!({"parse":[],"users":["OWN"]})
    );
    assert_eq!(
        payload(&c.uploads[1].2)["attachments"]
            .as_array()
            .unwrap()
            .len(),
        1
    );
    assert_eq!(h.state()["questions"][0]["after"], "1001");
}

#[test]
fn log_reuses_one_thread_and_notify_is_explicit() {
    let (mut h, c) = fixture();
    let p = attachment(&h, "sample.txt", 30);
    let first = cmd_log_attached(
        &mut h.ctx,
        "後片付けの記録",
        "確認",
        None,
        false,
        std::slice::from_ref(&p),
    )
    .unwrap();
    let second = cmd_log_attached(&mut h.ctx, "後片付けの記録", "確認", None, true, &[p]).unwrap();
    assert_eq!(first["thread_id"], second["thread_id"]);
    assert_eq!(c.borrow().uploads.len(), 2);
    assert_eq!(payload(&c.borrow().uploads[0].2)["flags"], SILENT);
    assert_eq!(payload(&c.borrow().uploads[1].2)["flags"], 0);
    assert_eq!(
        payload(&c.borrow().uploads[1].2)["allowed_mentions"],
        json!({"parse":[]})
    );
}

#[test]
fn limits_tiers_exact_boundary_generic_oversize_and_lookup_failure() {
    assert_eq!(
        [
            tier_limit(0),
            tier_limit(1),
            tier_limit(2),
            tier_limit(3),
            tier_limit(99)
        ],
        [
            BASE_LIMIT,
            BASE_LIMIT,
            50 * 1024 * 1024,
            100 * 1024 * 1024,
            BASE_LIMIT
        ]
    );
    let (mut h, c) = fixture();
    let p = attachment(&h, "exact.bin", BASE_LIMIT);
    assert_eq!(
        prepare(&mut h.ctx, &[p]).unwrap().unwrap().files[0].size,
        BASE_LIMIT
    );
    let p = attachment(&h, "oversize.bin", BASE_LIMIT + 1);
    assert!(prepare(&mut h.ctx, std::slice::from_ref(&p)).is_err());
    assert!(c.borrow().uploads.is_empty());
    c.borrow_mut().tier = 2;
    assert_eq!(
        prepare(&mut h.ctx, std::slice::from_ref(&p))
            .unwrap()
            .unwrap()
            .limit,
        50 * 1024 * 1024
    );
    c.borrow_mut().tier_error = true;
    assert!(prepare(&mut h.ctx, &[p]).is_err());
}

#[test]
fn oversized_image_retries_scaling_and_quality_until_actual_size_fits() {
    let (mut h, c) = fixture();
    let p = attachment(&h, "large.png", BASE_LIMIT + 1);
    c.borrow_mut().media_sizes = vec![BASE_LIMIT + 10, BASE_LIMIT + 1, 200];
    let prepared = prepare(&mut h.ctx, &[p]).unwrap().unwrap();
    assert_eq!(prepared.files.len(), 1);
    assert_eq!(prepared.files[0].name, "large.webp");
    assert_eq!(prepared.files[0].size, 200);
    let c = c.borrow();
    assert_eq!(c.runs.len(), 3);
    assert!(c.runs[0].iter().any(|s| s == "libwebp"));
    assert!(c.runs[2].iter().any(|s| s == "40"));
    assert!(c.uploads.is_empty());
}

#[test]
fn video_compresses_h264_then_splits_covering_full_duration() {
    let (mut h, c) = fixture();
    let p = attachment(&h, "large.mp4", BASE_LIMIT + 1);
    c.borrow_mut().media_sizes = vec![
        BASE_LIMIT + 1,
        BASE_LIMIT + 1,
        BASE_LIMIT + 1,
        100,
        100,
        100,
        100,
    ];
    let prepared = prepare(&mut h.ctx, &[p]).unwrap().unwrap();
    assert_eq!(prepared.files.len(), 4);
    for (i, f) in prepared.files.iter().enumerate() {
        assert_eq!(f.name, format!("large-part{:03}.mp4", i + 1));
        assert!(f.size <= BASE_LIMIT);
    }
    let c = c.borrow();
    let successful: Vec<_> = c.runs.iter().filter(|r| r[0] == "ffmpeg").skip(3).collect();
    let mut end = 0.;
    for argv in successful {
        let arg = |key| {
            argv[argv.iter().position(|s| s == key).unwrap() + 1]
                .parse::<f64>()
                .unwrap()
        };
        assert!((arg("-ss") - end).abs() < 0.00001);
        end += arg("-t");
        assert!(argv.iter().any(|s| s == "libx264"));
        assert!(argv.iter().any(|s| s == "yuv420p"));
    }
    assert!((end - 300.).abs() < 0.00001);
}

#[test]
fn no_post_if_any_file_is_client_secret_missing_or_conversion_fails() {
    for kind in ["client", "secret", "missing", "convert"] {
        let (mut h, c) = fixture();
        let good = attachment(&h, "good.txt", 30);
        let bad = match kind {
            "client" => {
                std::fs::write(&h.ctx.client_projects, "ClientCase").unwrap();
                attachment(&h, "CLIENTCASE-report.txt", 30)
            }
            "secret" => {
                let p = attachment(&h, "report.txt", 30);
                std::fs::write(&p, "password=do-not-upload").unwrap();
                p
            }
            "missing" => h.root.path().join("missing.txt"),
            _ => {
                c.borrow_mut().media_error = true;
                attachment(&h, "large.png", BASE_LIMIT + 1)
            }
        };
        let b = body(&h, "確認");
        assert!(cmd_post(&mut h.ctx, "100", &b, &[good, bad], false).is_err());
        assert!(c.borrow().uploads.is_empty());
        assert!(h.fake.borrow().posts.is_empty());
    }
}

#[test]
fn secrets_across_buffer_boundary_utf16_and_zip_are_refused() {
    let (mut h, c) = fixture();
    for data in [
        format!("{}password=private", "x".repeat(65532)).into_bytes(),
        "refresh_token=private"
            .encode_utf16()
            .flat_map(u16::to_le_bytes)
            .collect(),
    ] {
        let p = h.root.path().join("report.txt");
        std::fs::write(&p, data).unwrap();
        assert!(prepare(&mut h.ctx, &[p]).is_err());
    }
    let p = h.root.path().join("report.zip");
    let mut zip = zip::ZipWriter::new(std::fs::File::create(&p).unwrap());
    zip.start_file(
        "notes.txt",
        zip::write::SimpleFileOptions::default()
            .compression_method(zip::CompressionMethod::Deflated),
    )
    .unwrap();
    zip.write_all(b"api_key=private").unwrap();
    zip.finish().unwrap();
    assert!(prepare(&mut h.ctx, &[p]).is_err());
    assert!(c.borrow().uploads.is_empty());
}

#[test]
fn prepared_snapshot_survives_source_changes_and_cleans_itself() {
    let (mut h, c) = fixture();
    let p = attachment(&h, "sample.txt", 30);
    let prepared = prepare(&mut h.ctx, std::slice::from_ref(&p))
        .unwrap()
        .unwrap();
    let temp = prepared.files[0].path.clone();
    std::fs::write(&p, "password=private").unwrap();
    cc_ask::attachments::post(&mut h.ctx, "100", "確認", Some(&prepared), false).unwrap();
    assert!(!String::from_utf8_lossy(&c.borrow().uploads[0].2).contains("private"));
    drop(prepared);
    assert!(!temp.exists());
}

#[test]
fn total_request_size_splits_even_below_ten_attachments() {
    let files: Vec<_> = (0..3)
        .map(|i| File {
            path: PathBuf::new(),
            name: i.to_string(),
            mime: "application/octet-stream",
            size: 100 * 1024 * 1024,
        })
        .collect();
    assert_eq!(
        batches(&files).iter().map(|b| b.len()).collect::<Vec<_>>(),
        vec![1, 1, 1]
    );
}

#[test]
fn failed_followup_keeps_question_registered_with_last_successful_message() {
    let (mut h, c) = fixture();
    c.borrow_mut().fail_upload = Some(1);
    let files: Vec<_> = (0..11)
        .map(|i| attachment(&h, &format!("f{i}.pdf"), 30))
        .collect();
    let b = body(&h, "質問です。");
    let args = AskArgs {
        title: "質問".into(),
        body_file: b,
        attach: files,
        ..Default::default()
    };
    assert!(cmd_ask(&mut h.ctx, &args).is_err());
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
    assert_eq!(h.state()["questions"][0]["after"], "300");
    assert_eq!(h.state()["questions"][0]["open"], true);
}

#[cfg(windows)]
#[test]
fn junction_aliases_and_real_locations_of_denied_junctions_are_blocked() {
    let (mut h, c) = fixture();
    let target = h.root.path().join("CustomerCase");
    std::fs::create_dir(&target).unwrap();
    std::fs::write(target.join("report.txt"), "customer data").unwrap();
    let alias = h.root.path().join("alias");
    let out = run_hidden(
        &[
            "cmd.exe".into(),
            "/c".into(),
            "mklink".into(),
            "/J".into(),
            alias.to_string_lossy().into_owned(),
            target.to_string_lossy().into_owned(),
        ],
        10,
    )
    .unwrap();
    assert_eq!(out.code, 0, "{}", out.stderr);
    std::fs::write(&h.ctx.client_projects, "customercase").unwrap();
    let first = prepare(&mut h.ctx, &[alias.join("report.txt")]);
    std::fs::write(&h.ctx.client_projects, alias.to_string_lossy().as_bytes()).unwrap();
    let second = prepare(&mut h.ctx, &[target.join("report.txt")]);
    // Remove only the junction itself before TempDir drops.
    std::fs::remove_dir(&alias).unwrap();
    assert!(first.is_err());
    assert!(second.is_err());
    assert!(c.borrow().uploads.is_empty());
}

#[test]
fn database_urls_dsn_passwords_private_keys_and_unlabelled_uri_credentials_are_blocked() {
    let (mut h, c) = fixture();
    for text in [
        "DATABASE_URL=postgres://app:s3cr3t@db/app",
        "DB=redis://:s3cr3t@db/0",
        "url=mongodb+srv://app:p%40ss@db/app",
        "DSN=Driver={SQL Server};Server=db;Uid=app;Pwd=s3cr3t;",
        "passwd: s3cr3t",
        "-----BEGIN PGP PRIVATE KEY BLOCK-----\nprivate",
        "client-key-data: cHJpdmF0ZQ==",
        "token=private",
    ] {
        let p = h.root.path().join("report.txt");
        std::fs::write(&p, text).unwrap();
        assert!(
            prepare(&mut h.ctx, &[p]).is_err(),
            "missed credentials: {text}"
        );
    }
    assert!(c.borrow().uploads.is_empty());
    let p = attachment(&h, ".pgpass", 30);
    assert!(prepare(&mut h.ctx, &[p]).is_err());
}

#[test]
fn zip_with_client_member_path_is_refused_but_clean_public_zip_is_supported() {
    let (mut h, c) = fixture();
    std::fs::write(&h.ctx.client_projects, "ClientCase").unwrap();
    for (name, allowed) in [
        ("ClientCase/report.txt", false),
        ("public/report.txt", true),
    ] {
        let p = h.root.path().join("report.zip");
        let mut zip = zip::ZipWriter::new(std::fs::File::create(&p).unwrap());
        zip.start_file(name, zip::write::SimpleFileOptions::default())
            .unwrap();
        zip.write_all(b"public sample data").unwrap();
        zip.finish().unwrap();
        assert_eq!(prepare(&mut h.ctx, &[p]).is_ok(), allowed);
    }
    assert!(c.borrow().uploads.is_empty());
}

#[test]
fn prepended_zip_stubs_cannot_hide_secrets_or_client_paths() {
    let (mut h, c) = fixture();
    std::fs::write(&h.ctx.client_projects, "ClientCase").unwrap();
    for (name, contents, stub, allowed) in [
        (
            "ClientCase/report.txt",
            "public sample",
            "MZ self extracting stub\0",
            false,
        ),
        (
            "public/report.txt",
            "password=private",
            "MZ self extracting stub\0",
            false,
        ),
        (
            "public/report.txt",
            "public sample",
            "MZ password=private\0",
            false,
        ),
        (
            "public/report.txt",
            "public sample",
            "MZ self extracting stub\0",
            true,
        ),
    ] {
        let cursor = std::io::Cursor::new(Vec::new());
        let mut zip = zip::ZipWriter::new(cursor);
        zip.start_file(
            name,
            zip::write::SimpleFileOptions::default()
                .compression_method(zip::CompressionMethod::Deflated),
        )
        .unwrap();
        zip.write_all(contents.as_bytes()).unwrap();
        let data = zip.finish().unwrap().into_inner();
        let p = h.root.path().join("package.bin");
        let mut full = stub.as_bytes().to_vec();
        full.extend_from_slice(&data);
        std::fs::write(&p, &full).unwrap();
        assert_eq!(prepare(&mut h.ctx, &[p]).is_ok(), allowed);
        // A compressed outer archive must still inspect the nested ZIP's stub.
        let p = h.root.path().join("outer.zip");
        let mut outer = zip::ZipWriter::new(std::fs::File::create(&p).unwrap());
        outer
            .start_file(
                "nested-package.bin",
                zip::write::SimpleFileOptions::default()
                    .compression_method(zip::CompressionMethod::Deflated),
            )
            .unwrap();
        outer.write_all(&full).unwrap();
        outer.finish().unwrap();
        assert_eq!(prepare(&mut h.ctx, &[p]).is_ok(), allowed);
    }
    assert!(c.borrow().uploads.is_empty());
}
