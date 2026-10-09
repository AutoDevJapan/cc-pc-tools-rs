use super::*;
#[test]
fn intervals_merge_reverse_order_bridges_and_adjacent_events() {
    let mut intervals = vec![(10, 20), (40, 50), (70, 80)];
    merge_interval(&mut intervals, 25, 30);
    assert_eq!(intervals, vec![(10, 20), (25, 30), (40, 50), (70, 80)]);
    merge_interval(&mut intervals, 20, 70);
    assert_eq!(intervals, vec![(10, 80)]);
    merge_interval(&mut intervals, 5, 10);
    merge_interval(&mut intervals, 80, 90);
    merge_interval(&mut intervals, 12, 15);
    assert_eq!(intervals, vec![(5, 90)]);
}
#[test]
fn excluded_source_removes_grouped_work_slots_and_summaries() {
    let mut c = Cache::default();
    let mut p = ProjectDay {
        path: "D:/Tools".into(),
        source_paths: vec!["D:/Tools/console".into()],
        ..ProjectDay::default()
    };
    p.results.push(ResultLine {
        kind: "job".into(),
        text: "画面を追加しました。".into(),
        url: None,
    });
    c.days.insert("2026-10-02".into(), vec![p]);
    c.slots.insert(
        "2026-10-02".into(),
        BTreeMap::from([("d:/tools".into(), vec![900_000; 96])]),
    );
    c.summaries
        .insert("2026-10-02|d:/tools".into(), Summary::default());
    purge_repo(&mut c, "D:/Tools/console");
    assert!(c.days["2026-10-02"].is_empty());
    assert!(c.slots["2026-10-02"].is_empty());
    assert!(c.summaries.is_empty());
    assert!(c.prohibited.contains("d:/tools/console"));
}
#[test]
fn summary_accepts_display_folder_only_when_original_repositories_exist() {
    let mut c = Cache::default();
    c.repos.insert("d:/demo/tools/app".into(), Repo::default());
    c.days.insert(
        "2026-10-02".into(),
        vec![ProjectDay {
            path: "D:/demo/tools".into(),
            source_paths: vec!["D:/demo/tools/app".into()],
            ..ProjectDay::default()
        }],
    );
    let key = "2026-10-02|d:/demo/tools";
    assert!(summary_target_exists(&c, key));
    assert!(!summary_target_exists(&c, "2026-10-02|d:/other"));
    c.prohibited.insert("d:/demo/tools/app".into());
    assert!(!summary_target_exists(&c, key));
    c.prohibited.clear();
    c.repos.clear();
    assert!(!summary_target_exists(&c, key));
}
#[test]
fn late_summary_cannot_restore_excluded_source_and_other_day_is_kept() {
    let mut c = Cache::default();
    for source in ["D:/Tools/a", "D:/Tools/b"] {
        c.repos.insert(normalize(source), Repo::default());
    }
    let allowed = ProjectDay {
        path: "D:/Tools".into(),
        source_paths: vec!["D:/Tools/a".into()],
        results: vec![ResultLine {
            kind: "commit".into(),
            text: "画面を追加する".into(),
            url: None,
        }],
        ..ProjectDay::default()
    };
    let mut combined = allowed.clone();
    combined.source_paths.push("D:/Tools/b".into());
    let old_hash = summary_input_hash(&combined);
    let old_sources = combined.source_paths.clone();
    c.days.insert("2026-10-01".into(), vec![allowed.clone()]);
    c.days.insert("2026-10-02".into(), vec![combined]);
    for date in ["2026-10-01", "2026-10-02"] {
        c.slots.insert(
            date.into(),
            BTreeMap::from([("d:/tools".into(), vec![900_000; 96])]),
        );
        c.summaries
            .insert(format!("{date}|d:/tools"), Summary::default());
    }
    let key = "2026-10-02|d:/tools";
    assert!(summary_request_matches(&c, key, &old_hash, &old_sources));
    purge_repo(&mut c, "D:/Tools/b");
    assert_eq!(c.slots["2026-10-01"]["d:/tools"][40], 900_000);
    assert!(c.summaries.contains_key("2026-10-01|d:/tools"));
    assert!(c.slots["2026-10-02"].is_empty());
    assert!(!c.summaries.contains_key(key));
    c.days.insert("2026-10-02".into(), vec![allowed.clone()]);
    assert!(!summary_request_matches(&c, key, &old_hash, &old_sources));
    let allowed_hash = summary_input_hash(&allowed);
    assert!(summary_request_matches(
        &c,
        key,
        &allowed_hash,
        &allowed.source_paths
    ));
    c.days.get_mut("2026-10-02").unwrap()[0].results.clear();
    assert!(!summary_request_matches(
        &c,
        key,
        &allowed_hash,
        &allowed.source_paths
    ));
}
#[test]
fn discarded_summary_keeps_ten_minute_throttle_after_restart() {
    let f = Fixture::new();
    let mut cache = Cache::default();
    let key = "2026-10-02|d:/demo";
    let now = at("2026-10-02T12:00:00+09:00");
    let p = ProjectDay {
        path: "D:/demo".into(),
        source_paths: vec!["D:/demo".into()],
        results: vec![ResultLine {
            kind: "commit".into(),
            text: "画面を追加する".into(),
            url: None,
        }],
        ..ProjectDay::default()
    };
    let old_hash = summary_input_hash(&p);
    let sources = p.source_paths.clone();
    cache.repos.insert("d:/demo".into(), Repo::default());
    cache.days.insert("2026-10-02".into(), vec![p]);
    assert!(summary_request_matches(&cache, key, &old_hash, &sources));
    assert!(reserve_summary(&mut cache, key, &old_hash, now));
    cache.days.get_mut("2026-10-02").unwrap()[0].results.clear();
    assert!(!summary_request_matches(&cache, key, &old_hash, &sources));
    assert_eq!(cache.summary_attempts[key], now);
    assert!(!reserve_summary(
        &mut cache,
        key,
        "changed-input",
        now + 599_999
    ));
    assert!(cache.summaries.is_empty());
    save(&f.sources, &cache).unwrap();
    let mut restored = load_cache(&f.sources.cache).unwrap();
    assert!(!reserve_summary(
        &mut restored,
        key,
        "changed-again",
        now + 599_999
    ));
    assert!(reserve_summary(
        &mut restored,
        key,
        "changed-again",
        now + 600_000
    ));
    assert_eq!(restored.summary_attempts[key], now + 600_000);
}
#[test]
fn title_fallback_has_only_recorded_work_and_short_links() {
    let mut p = ProjectDay::default();
    assert!(fallback_summary(&p).is_empty());
    p.results.push(ResultLine {
        kind: "commit".into(),
        text: "画面を追加する".into(),
        url: None,
    });
    assert_eq!(fallback_summary(&p).len(), 1);
    assert_eq!(fallback_summary(&p)[0].text, "作業内容：画面を追加する");
    assert_eq!(
        first_url("https://github.com/example/demo/pull/12)はマージ済み。"),
        Some("https://github.com/example/demo/pull/12".into())
    );
}
#[test]
fn large_cache_restores_offsets_without_generic_json_copy() {
    let f = Fixture::new();
    let mut cache = Cache {
        version: 1,
        ..Cache::default()
    };
    cache.cursors.insert(
        "fixture.jsonl".into(),
        Cursor {
            offset: 12345,
            ..Cursor::default()
        },
    );
    // Reproduce the old 32 MiB limit with legal JSON whitespace, not live transcripts.
    let mut file = BufWriter::new(File::create(&f.sources.cache).unwrap());
    file.write_all(&vec![b' '; 33 * 1024 * 1024]).unwrap();
    serde_json::to_writer(&mut file, &cache).unwrap();
    file.flush().unwrap();
    drop(file);
    assert_eq!(
        load_cache(&f.sources.cache).unwrap().cursors["fixture.jsonl"].offset,
        12345
    );
}
#[test]
fn copies_and_nested_tools_share_counts_without_duplicate_git_results() {
    let f = Fixture::new();
    let now = at("2026-10-02T12:00:00+09:00");
    let started = at("2026-10-02T10:00:00+09:00");
    let roots = [
        "D:/demo/GoDD-Demo",
        "D:/copies/demo-review",
        "D:/Tools",
        "D:\\\\Tools\\\\console",
    ];
    fs::write(
        &f.sources.todo,
        json!({"projects": {
        "GoDD-Demo":roots[0], "godd-demo":roots[1], "tools":roots[2], "console":roots[3]}, "items": []})
        .to_string(),
    )
    .unwrap();
    let mut cache = Cache {
        version: 1,
        exclusion_hash: hash(&load_exclusions(&f.sources).unwrap().0.join("\n")),
        ..Cache::default()
    };
    for (i, root) in roots.iter().enumerate() {
        cache.mappings.insert(normalize(root), (*root).into());
        cache.repos.insert(normalize(root), Repo {
            path: (*root).into(), name: format!("alias-{i}"),
            remote: (i < 2).then(|| if i == 0 {"example/GoDD-Demo"} else {"EXAMPLE/godd-demo"}.into()),
            commits: vec![("abc123".into(), started, "画面を追加する".into())],
            prs: if i < 2 {vec![json!({"mergedAt":"2026-10-02T10:00:00+09:00", "url":"https://github.com/example/GoDD-Demo/pull/1", "title":"画面を追加する"})]} else {vec![]},
            ..Repo::default()
        });
        let folder = f.sources.jobs.join(format!("job-{i}"));
        fs::create_dir(&folder).unwrap();
        fs::write(folder.join("job.json"), json!({"cwd":root,"created_at":"2026-10-02T10:00:00+09:00","started_at":"2026-10-02T10:00:00+09:00","status":"running","tokens":{"total":10}}).to_string()).unwrap();
    }
    let c = aggregate(&f.sources, cache, now);
    let projects = &c.days["2026-10-02"];
    assert_eq!(projects.len(), 2);
    for name in ["GoDD-Demo", "Tools"] {
        let p = projects.iter().find(|p| p.name == name).unwrap();
        assert_eq!(p.numbers.jobs, 2);
        assert_eq!(p.numbers.tokens, 20);
        assert_eq!(p.numbers.active_ms, 2 * 3_600_000);
        assert_eq!(p.numbers.run_ms, 4 * 3_600_000);
        assert_eq!(p.numbers.commits, 1);
        assert_eq!(p.numbers.merged_prs, u64::from(name == "GoDD-Demo"));
        assert_eq!(c.slots["2026-10-02"][&normalize(&p.path)][40], 900_000);
    }
}
#[test]
fn summaries_mask_credential_syntax_and_preserve_markdown_evidence_links() {
    let text = clean("token=fixture-private-value sk-abcdefgh0123456789 を表示しない");
    assert!(!text.contains("fixture-private-value"));
    assert!(!text.contains("sk-abcdefgh"));
    assert!(text.contains("[秘密]"));
    assert_eq!(
        first_url("[変更内容](https://github.com/example/demo/pull/12)"),
        Some("https://github.com/example/demo/pull/12".into())
    );
}
fn read_session(
    path: &Path,
    c: &mut Cache,
    r: &BTreeMap<String, String>,
    e: &Exclusions,
    oldest: i64,
    now: i64,
) -> bool {
    read_session_limited(path, c, r, e, oldest, now, READ_BUDGET)
}
#[test]
fn combined_fractional_files_never_exceed_total_read_budget() {
    let f = Fixture::new();
    let large = format!(
        "{{\"pad\":\"{}\"}}\n",
        "x".repeat(READ_BUDGET as usize + 1024)
    );
    for i in 0..9 {
        fs::write(f.sources.claude.join(format!("large-{i}.jsonl")), &large).unwrap();
    }
    fs::write(
        f.sources.claude.join("small.jsonl"),
        format!("{{\"pad\":\"{}\"}}\n", "x".repeat(1024 * 1024 + 111)),
    )
    .unwrap();
    let now = Utc::now().timestamp_millis();
    let c = aggregate(&f.sources, Cache::default(), now);
    assert!(c.refresh_read_bytes <= REFRESH_READ_BUDGET);
    assert_eq!(c.refresh_read_bytes, REFRESH_READ_BUDGET);
    let offsets: u64 = c.cursors.values().map(|c| c.offset).sum();
    let c = aggregate(&f.sources, c, now);
    assert!(c.refresh_read_bytes <= REFRESH_READ_BUDGET);
    assert!(c.cursors.values().map(|c| c.offset).sum::<u64>() > offsets);
}
#[test]
fn records_arriving_during_collection_are_left_for_next_refresh() {
    let f = Fixture::new();
    let path = f.sources.claude.join("race.jsonl");
    fs::write(
        &path,
        include_str!("../tests/fixtures/workload/claude.jsonl"),
    )
    .unwrap();
    let mut c = Cache::default();
    let r = BTreeMap::new();
    let e = exclusions();
    let oldest = date_ms("2026-10-01").unwrap();
    assert!(read_session(
        &path,
        &mut c,
        &r,
        &e,
        oldest,
        at("2026-10-02T00:00:00+09:00")
    ));
    let key = path.to_string_lossy().into_owned();
    assert!(!c.cursors[&key].days.contains_key("2026-10-02"));
    read_session(
        &path,
        &mut c,
        &r,
        &e,
        oldest,
        at("2026-10-02T02:00:00+09:00"),
    );
    assert_eq!(c.cursors[&key].days["2026-10-02"].tokens, 205);
}
#[test]
fn migrated_old_transcript_is_identified_without_loading_body() {
    let f = Fixture::new();
    let path = f.sources.codex.join("old.jsonl");
    fs::write(
        &path,
        include_str!("../tests/fixtures/workload/codex.jsonl"),
    )
    .unwrap();
    let len = path.metadata().unwrap().len();
    let (old, bytes) = old_transcript(&path, len, date_ms("2026-10-02").unwrap(), 4096);
    assert_eq!(old.unwrap().1, 300);
    assert!(bytes <= 4096);
    let (cwd, id) = metadata_prefix(&path);
    assert_eq!(cwd, "D:/work/demo");
    assert_eq!(id, "fixture-session");
}
#[test]
fn tail_probe_without_codex_baseline_replays_before_resumed_usage() {
    let f = Fixture::new();
    let path = f.sources.codex.join("resume.jsonl");
    let old = "2026-10-01T09:00:00+09:00";
    let records = [
        json!({"type":"session_meta","timestamp":old,"payload":{"id":"resume-fixture","cwd":"D:/work/demo"}}),
        json!({"type":"event_msg","timestamp":old,"payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":1000}}}}),
        json!({"type":"response_item","timestamp":old,"payload":{"type":"message","content":"x".repeat(6000)}}),
        json!({"type":"event_msg","timestamp":old,"payload":{"type":"task_complete"}}),
    ];
    fs::write(
        &path,
        records.iter().map(|v| format!("{v}\n")).collect::<String>(),
    )
    .unwrap();
    fs::OpenOptions::new()
        .write(true)
        .open(&path)
        .unwrap()
        .set_modified(
            std::time::UNIX_EPOCH + Duration::from_millis(at("2026-10-02T12:00:00+09:00") as u64),
        )
        .unwrap();
    let oldest = date_ms("2026-10-02").unwrap();
    assert!(
        old_transcript(&path, path.metadata().unwrap().len(), oldest, 4096)
            .0
            .is_none()
    );
    let mut c = Cache::default();
    let e = exclusions();
    let r = BTreeMap::new();
    let now = at("2026-10-02T12:00:00+09:00");
    read_session(&path, &mut c, &r, &e, oldest, now);
    let mut file = fs::OpenOptions::new().append(true).open(&path).unwrap();
    writeln!(file,"{}",json!({"type":"event_msg","timestamp":"2026-10-02T09:00:00+09:00","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":1100}}}})).unwrap();
    drop(file);
    read_session(&path, &mut c, &r, &e, oldest, now);
    assert_eq!(
        c.cursors[&path.to_string_lossy().into_owned()].days["2026-10-02"].tokens,
        100
    );
}
#[test]
fn partial_new_record_cannot_be_marked_as_old_transcript() {
    let f = Fixture::new();
    let path = f.sources.claude.join("partial-old.jsonl");
    let old = include_str!("../tests/fixtures/workload/claude.jsonl")
        .lines()
        .next()
        .unwrap();
    let next = include_str!("../tests/fixtures/workload/claude.jsonl")
        .lines()
        .nth(1)
        .unwrap();
    fs::write(&path, format!("{old}\n{}", &next[..40])).unwrap();
    fs::OpenOptions::new()
        .write(true)
        .open(&path)
        .unwrap()
        .set_modified(
            std::time::UNIX_EPOCH + Duration::from_millis(at("2026-10-02T12:00:00+09:00") as u64),
        )
        .unwrap();
    let oldest = date_ms("2026-10-02").unwrap();
    assert!(
        old_transcript(&path, path.metadata().unwrap().len(), oldest, 4096)
            .0
            .is_none()
    );
    let mut c = Cache::default();
    let e = exclusions();
    let r = BTreeMap::new();
    let now = at("2026-10-02T12:00:00+09:00");
    read_session(&path, &mut c, &r, &e, oldest, now);
    fs::write(&path, format!("{old}\n{next}\n")).unwrap();
    read_session(&path, &mut c, &r, &e, oldest, now);
    assert_eq!(
        c.cursors[&path.to_string_lossy().into_owned()].days["2026-10-02"].tokens,
        200
    );
}

#[tokio::test]
#[ignore = "explicit local-source smoke check, not part of fixture tests"]
async fn production_sources_smoke_check() {
    let mut sources = Sources::production();
    sources.cache = PathBuf::from("D:/CC/Tools/localai-web-rs/target/workload-probe-cache.json");
    let previous = json_file(&sources.cache, 32 * 1024 * 1024)
        .and_then(|v| serde_json::from_value(v).ok())
        .unwrap_or_default();
    let started = Instant::now();
    let now = Utc::now().timestamp_millis();
    let mut cache = aggregate(&sources, previous, now);
    let e = load_exclusions(&sources).unwrap();
    cache.repos = cache
        .repos
        .into_iter()
        .map(|(key, r)| {
            (
                key,
                refresh_repo(r, midnight(now) - (KEEP_DAYS - 1) * DAY, now, &e),
            )
        })
        .collect();
    cache = aggregate(&sources, cache, now);
    save(&sources, &cache).unwrap();
    let workload = Workload {
        sources: sources.clone(),
        cache: RwLock::new(cache.clone()),
        phase: RwLock::new(String::new()),
    };
    let snapshot = workload.snapshot(&day(now));
    println!(
        "{}",
        json!({"elapsed_ms":started.elapsed().as_millis(),"sessions":cache.cursors.len(),"repositories":cache.repos.len(),"cache_bytes":sources.cache.metadata().unwrap().len(),"totals":snapshot["totals"],"warnings":snapshot["warnings"],"projects":snapshot["projects"].as_array().map(|p|p.len())})
    );
    if let Some(p) = cache
        .days
        .get(&day(now))
        .and_then(|ps| ps.iter().find(|p| !p.results.is_empty()))
    {
        let e = load_exclusions(&sources).unwrap();
        let client = reqwest::Client::builder()
            .no_proxy()
            .redirect(reqwest::redirect::Policy::none())
            .timeout(Duration::from_secs(25))
            .build()
            .unwrap();
        let lines = model_summary(&client, p, &e).await;
        println!(
            "{}",
            json!({"model_summary_succeeded":lines.is_some(),"lines":lines.map(|x|x.len())})
        );
    }
}

fn at(s: &str) -> i64 {
    timestamp(s).unwrap()
}
fn exclusions() -> Exclusions {
    Exclusions(vec!["imaginary-client-product".into()])
}
fn fixture_lines(s: &str) -> Vec<Value> {
    s.lines()
        .map(|s| serde_json::from_str(s).unwrap())
        .collect()
}
#[test]
fn jst_boundaries_and_calendar_validation() {
    assert_eq!(day(at("2026-10-01T14:59:59Z")), "2026-10-01");
    assert_eq!(day(at("2026-10-01T15:00:00Z")), "2026-10-02");
    assert_eq!(date_ms("2026-10-02"), Some(at("2026-10-01T15:00:00Z")));
    assert!(date_ms("2026-02-30").is_none());
    assert!(date_ms("2026-1-1").is_none());
}
#[test]
fn worktree_mapping_uses_repository_and_registered_subfolders() {
    let registered = BTreeMap::from([
        ("demo".into(), "D:/work/demo".into()),
        ("civ6".into(), "D:/work/civ6-banseiki".into()),
    ]);
    assert_eq!(
        fallback_root("D:\\work\\civ6-banseiki-wt\\feature\\src", &registered),
        "D:/work/civ6-banseiki"
    );
    assert_eq!(
        fallback_root("D:/work/demo-wt/build", &registered),
        "D:/work/demo"
    );
    assert_eq!(
        fallback_root("D:/work/demo/src", &registered),
        "D:/work/demo"
    );
    assert_eq!(
        fallback_root("D:/work/demolition", &registered),
        "D:/work/demolition"
    );
}
#[test]
fn arbitrary_worktree_uses_git_common_directory_without_subprocesses() {
    let f = Fixture::new();
    let root = f.root.join("repo");
    let metadata = root.join(".git/worktrees/topic");
    let checkout = f.root.join("unrelated-checkout");
    fs::create_dir_all(&metadata).unwrap();
    fs::create_dir_all(checkout.join("src")).unwrap();
    fs::write(metadata.join("commondir"), "../..\n").unwrap();
    fs::write(
        checkout.join(".git"),
        format!("gitdir: {}\n", metadata.to_string_lossy()),
    )
    .unwrap();
    assert_eq!(
        normalize(&repository_root(&checkout.join("src")).unwrap()),
        normalize(&root.to_string_lossy())
    );
    assert_eq!(
        normalize(&repository_root(&root).unwrap()),
        normalize(&root.to_string_lossy())
    );
}
#[test]
fn claude_fixture_caps_gaps_splits_midnight_and_dedupes_streaming_usage() {
    let mut cursor = Cursor::default();
    let now = at("2026-10-02T02:00:00+09:00");
    for v in fixture_lines(include_str!("../tests/fixtures/workload/claude.jsonl")) {
        session_record(&mut cursor, &v, date_ms("2026-10-01").unwrap(), now);
    }
    let first = &cursor.days["2026-10-01"];
    let next = &cursor.days["2026-10-02"];
    assert_eq!(
        first.intervals.iter().map(|(s, e)| e - s).sum::<i64>(),
        60_000
    );
    assert_eq!(
        next.intervals.iter().map(|(s, e)| e - s).sum::<i64>(),
        361_000
    );
    assert_eq!(next.tokens, 205);
    assert_eq!(next.tools, 1);
}
#[test]
fn codex_cumulative_usage_is_not_counted_twice() {
    let mut cursor = Cursor::default();
    for v in fixture_lines(include_str!("../tests/fixtures/workload/codex.jsonl")) {
        session_record(
            &mut cursor,
            &v,
            date_ms("2026-10-01").unwrap(),
            at("2026-10-02T00:00:00+09:00"),
        );
    }
    assert_eq!(cursor.session_id, "fixture-session");
    assert_eq!(cursor.days["2026-10-01"].tokens, 300);
    assert_eq!(cursor.days["2026-10-01"].tools, 1);
    assert_eq!(
        cursor.days["2026-10-01"]
            .intervals
            .iter()
            .map(|(s, e)| e - s)
            .sum::<i64>(),
        300_000
    );
}
#[test]
fn codex_old_usage_seeds_baseline_without_charging_current_day() {
    let mut c = Cursor::default();
    let oldest = date_ms("2026-10-01").unwrap();
    let now = at("2026-10-02T00:00:00+09:00");
    for (date, total) in [
        ("2026-09-30T23:59:00+09:00", 1000),
        ("2026-10-01T00:01:00+09:00", 1100),
    ] {
        session_record(
            &mut c,
            &json!({"type":"event_msg","timestamp":date,"payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":total}}}}),
            oldest,
            now,
        );
    }
    assert_eq!(c.days["2026-10-01"].tokens, 100);
    assert!(!c.days.contains_key("2026-09-30"));
}
#[test]
fn old_summary_is_removed_when_inputs_change_or_disappear() {
    let p = ProjectDay {
        name: "demo".into(),
        path: "D:/demo".into(),
        results: vec![ResultLine {
            kind: "commit".into(),
            text: "画面を追加する".into(),
            url: None,
        }],
        ..ProjectDay::default()
    };
    let fingerprint = summary_input_hash(&p);
    let mut c = Cache {
        days: BTreeMap::from([("2026-10-01".into(), vec![p])]),
        summaries: BTreeMap::from([(
            "2026-10-01|d:/demo".into(),
            Summary {
                input_hash: fingerprint,
                lines: vec![SummaryLine {
                    text: "画面を追加しました。".into(),
                    url: None,
                }],
                source: "model".into(),
                ..Summary::default()
            },
        )]),
        ..Cache::default()
    };
    apply_summaries(&mut c);
    assert_eq!(c.days["2026-10-01"][0].summary_source, "model");
    c.days.get_mut("2026-10-01").unwrap()[0].results.clear();
    apply_summaries(&mut c);
    assert_eq!(c.days["2026-10-01"][0].summary_source, "titles");
    assert!(
        !c.days["2026-10-01"][0]
            .summary
            .iter()
            .any(|l| l.text == "画面を追加しました。")
    );
}
#[test]
fn excluded_registration_protects_unrelated_directory_names() {
    let f = Fixture::new();
    fs::write(
        &f.sources.todo,
        json!({"projects":{"imaginary-client-product":"D:/neutral-folder"},"items":[]}).to_string(),
    )
    .unwrap();
    let e = load_exclusions(&f.sources).unwrap();
    assert!(e.contains("D:/neutral-folder/src"));
    assert!(e.contains("D:/neutral-folder-wt/build"));
    assert!(!e.contains("D:/public-folder"));
    assert!(e.contains("C:/claude/projects/D--neutral-folder/session.jsonl"));
}
#[test]
fn unavailable_project_aliases_fail_closed() {
    let f = Fixture::new();
    fs::write(&f.sources.todo, b"{").unwrap();
    assert!(load_exclusions(&f.sources).is_err());
}
#[test]
fn same_length_log_replacement_rebuilds_its_usage() {
    let f = Fixture::new();
    let path = f.sources.claude.join("replace.jsonl");
    let text = include_str!("../tests/fixtures/workload/claude.jsonl");
    fs::write(&path, text).unwrap();
    let mut c = Cache::default();
    let e = exclusions();
    let r = BTreeMap::new();
    let oldest = date_ms("2026-10-01").unwrap();
    let now = at("2026-10-03T00:00:00+09:00");
    read_session(&path, &mut c, &r, &e, oldest, now);
    let key = path.to_string_lossy().into_owned();
    assert_eq!(c.cursors[&key].days["2026-10-02"].tokens, 205);
    fs::write(
        &path,
        text.replace("\"input_tokens\":100", "\"input_tokens\":300"),
    )
    .unwrap();
    fs::OpenOptions::new()
        .write(true)
        .open(&path)
        .unwrap()
        .set_modified(std::time::SystemTime::now() + Duration::from_secs(1))
        .unwrap();
    read_session(&path, &mut c, &r, &e, oldest, now);
    assert_eq!(c.cursors[&key].days["2026-10-02"].tokens, 405);
}
#[test]
fn running_job_uses_now_and_shares_runtime_across_jst_days() {
    let job: Value =
        serde_json::from_str(include_str!("../tests/fixtures/workload/job.json")).unwrap();
    let parts = job_numbers(&job, at("2026-10-02T00:05:00+09:00"));
    assert_eq!(parts.len(), 2);
    assert_eq!(parts[0].0, "2026-10-01");
    assert_eq!(parts[1].0, "2026-10-02");
    assert_eq!(parts[0].3, 300_000);
    assert_eq!(parts[1].3, 300_000);
    assert_eq!(parts[0].4 + parts[1].4, 1000);
}
#[test]
fn commit_fixture_dedupes_sha_and_filters_author_date_and_client() {
    let text = include_str!("../tests/fixtures/workload/git.txt").replace("<SEP>", "\u{1f}");
    let commits = commit_output(&text, date_ms("2026-10-01").unwrap(), &exclusions());
    assert_eq!(commits.len(), 1);
    assert_eq!(commits[0].0, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
    assert_eq!(commits[0].2, "画面を追加する");
}
#[test]
fn overlapping_job_and_session_time_is_counted_once_per_project() {
    let mut intervals = Vec::new();
    merge_interval(&mut intervals, 100, 500);
    merge_interval(&mut intervals, 200, 800);
    merge_interval(&mut intervals, 900, 1000);
    merge_interval(&mut intervals, 700, 950);
    assert_eq!(intervals, vec![(100, 1000)]);
}
#[test]
fn exclusions_apply_case_insensitive_to_paths_names_remotes_and_titles() {
    let e = exclusions();
    for text in [
        "D:/work/IMAGINARY-client-product-wt/test",
        "task-imaginary-client-product-fix",
        "https://github.com/example/Imaginary-Client-Product.git",
        "別の作業からimaginary-client-productを参照する",
    ] {
        assert!(e.contains(text));
    }
    let mut p = ProjectDay::default();
    result(
        &mut p,
        "commit",
        "imaginary-client-productを修正する",
        None,
        &e,
    );
    result(
        &mut p,
        "job",
        "画面を追加する",
        Some("https://github.com/example/imaginary-client-product/commit/abc".into()),
        &e,
    );
    assert!(p.results.is_empty());
    assert!(safe_url("https://github.com.evil.invalid/abc").is_none());
    assert!(github_remote("https://user:secret@github.com/example/demo.git").is_none());
}

struct Fixture {
    root: PathBuf,
    sources: Sources,
}
impl Fixture {
    fn new() -> Self {
        let root = std::env::temp_dir().join(format!(
            "console-workload-test-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        fs::create_dir_all(&root).unwrap();
        let sources = Sources {
            jobs: root.join("jobs"),
            claude: root.join("claude"),
            codex: root.join("codex"),
            todo: root.join("todo.json"),
            decisions: root.join("decisions.json"),
            questions: root.join("questions.json"),
            exclusion: root.join("excluded.txt"),
            cache: root.join("cache.json"),
        };
        for p in [&sources.jobs, &sources.claude, &sources.codex] {
            fs::create_dir_all(p).unwrap();
        }
        fs::write(&sources.exclusion, "imaginary-client-product\n").unwrap();
        fs::write(&sources.todo, r#"{"projects":{},"items":[]}"#).unwrap();
        Self { root, sources }
    }
}
impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.root);
    }
}
#[test]
fn incremental_offsets_survive_restart_and_partial_lines() {
    let f = Fixture::new();
    let file = f.sources.claude.join("session.jsonl");
    let lines: Vec<_> = include_str!("../tests/fixtures/workload/claude.jsonl")
        .lines()
        .collect();
    fs::write(&file, format!("{}\n{}", lines[0], &lines[1][..40])).unwrap();
    let now = at("2026-10-02T02:00:00+09:00");
    let mut c = Cache::default();
    let r = BTreeMap::new();
    let e = exclusions();
    assert!(read_session(
        &file,
        &mut c,
        &r,
        &e,
        date_ms("2026-10-01").unwrap(),
        now
    ));
    let key = file.to_string_lossy().into_owned();
    assert_eq!(c.cursors[&key].offset, (lines[0].len() + 1) as u64);
    save(&f.sources, &c).unwrap();
    let mut c: Cache =
        serde_json::from_value(json_file(&f.sources.cache, 1024 * 1024).unwrap()).unwrap();
    fs::write(&file, format!("{}\n{}\n", lines[0], lines[1])).unwrap();
    assert!(!read_session(
        &file,
        &mut c,
        &r,
        &e,
        date_ms("2026-10-01").unwrap(),
        now
    ));
    assert_eq!(c.cursors[&key].days["2026-10-02"].tokens, 200);
    let before = c.cursors[&key].offset;
    assert!(!read_session(
        &file,
        &mut c,
        &r,
        &e,
        date_ms("2026-10-01").unwrap(),
        now
    ));
    assert_eq!(c.cursors[&key].offset, before);
    assert_eq!(c.cursors[&key].days["2026-10-02"].tokens, 200);
}
#[test]
fn historical_streaming_usage_survives_an_incremental_read_boundary() {
    let f = Fixture::new();
    let file = f.sources.claude.join("stream.jsonl");
    let lines: Vec<_> = include_str!("../tests/fixtures/workload/claude.jsonl")
        .lines()
        .collect();
    let first = format!("{}\n", lines[1]);
    let next = format!("{}\n", lines[2]);
    let overhead = "{\"type\":\"unknown\",\"pad\":\"\"}\n".len();
    let pad = (READ_BUDGET as usize) - overhead - first.len();
    fs::write(
        &file,
        format!(
            "{{\"type\":\"unknown\",\"pad\":\"{}\"}}\n{first}{next}",
            "x".repeat(pad)
        ),
    )
    .unwrap();
    let mut c = Cache::default();
    let r = BTreeMap::new();
    let e = exclusions();
    let now = at("2026-10-03T00:00:00+09:00");
    let oldest = date_ms("2026-10-01").unwrap();
    assert!(read_session(&file, &mut c, &r, &e, oldest, now));
    let key = file.to_string_lossy().into_owned();
    assert_eq!(c.cursors[&key].offset, READ_BUDGET);
    assert_eq!(c.cursors[&key].days["2026-10-02"].tokens, 200);
    assert!(!read_session(&file, &mut c, &r, &e, oldest, now));
    assert_eq!(c.cursors[&key].days["2026-10-02"].tokens, 205);
    assert_eq!(c.cursors[&key].days["2026-10-02"].tools, 1);
}
#[test]
fn huge_unterminated_record_obeys_budget_and_resumes_discarding() {
    let f = Fixture::new();
    let path = f.sources.claude.join("large.jsonl");
    fs::write(
        &path,
        format!("{{\"pad\":\"{}", "x".repeat(READ_BUDGET as usize + 500)),
    )
    .unwrap();
    let mut c = Cache::default();
    let r = BTreeMap::new();
    let e = exclusions();
    let now = at("2026-10-02T02:00:00+09:00");
    let oldest = date_ms("2026-10-01").unwrap();
    assert!(read_session(&path, &mut c, &r, &e, oldest, now));
    let key = path.to_string_lossy().into_owned();
    assert_eq!(c.last_read_bytes, READ_BUDGET);
    assert_eq!(c.cursors[&key].offset, READ_BUDGET);
    assert!(c.cursors[&key].discarding);
    let mut file = fs::OpenOptions::new().append(true).open(&path).unwrap();
    writeln!(file, "\"}}").unwrap();
    writeln!(
        file,
        "{}",
        include_str!("../tests/fixtures/workload/claude.jsonl")
            .lines()
            .nth(1)
            .unwrap()
    )
    .unwrap();
    drop(file);
    assert!(!read_session(&path, &mut c, &r, &e, oldest, now));
    assert!(!c.cursors[&key].discarding);
    assert_eq!(c.cursors[&key].days["2026-10-02"].tokens, 200);
    assert!(c.last_read_bytes < READ_BUDGET);
}
#[test]
fn changed_exclusions_prevent_cache_publication() {
    let f = Fixture::new();
    let c = Cache {
        version: 1,
        exclusion_hash: hash(&load_exclusions(&f.sources).unwrap().0.join("\n")),
        ..Cache::default()
    };
    fs::write(&f.sources.exclusion, "different-made-up-name").unwrap();
    assert!(save(&f.sources, &c).is_err());
    assert!(!f.sources.cache.exists());
    assert!(!f.sources.cache.with_extension("json.tmp").exists());
}
#[test]
fn exclusion_file_failure_clears_cached_and_current_results() {
    let f = Fixture::new();
    let c = Cache {
        days: BTreeMap::from([(
            "2026-10-01".into(),
            vec![ProjectDay {
                name: "demo".into(),
                ..ProjectDay::default()
            }],
        )]),
        ..Cache::default()
    };
    fs::remove_file(&f.sources.exclusion).unwrap();
    let c = aggregate(&f.sources, c, at("2026-10-01T12:00:00+09:00"));
    assert!(c.days.is_empty());
    assert_eq!(c.warnings.len(), 1);
}
#[test]
fn end_to_end_excluded_job_never_enters_cache_or_summary() {
    let f = Fixture::new();
    let dir = f.sources.jobs.join("job");
    fs::create_dir(&dir).unwrap();
    let mut job: Value =
        serde_json::from_str(include_str!("../tests/fixtures/workload/job.json")).unwrap();
    job["name"] = json!("imaginary-client-product");
    fs::write(dir.join("job.json"), job.to_string()).unwrap();
    let c = aggregate(
        &f.sources,
        Cache::default(),
        at("2026-10-02T00:05:00+09:00"),
    );
    assert!(c.days.is_empty());
    assert!(
        !serde_json::to_string(&c)
            .unwrap()
            .contains("imaginary-client-product")
    );
}
#[tokio::test]
async fn workload_route_returns_versioned_json_and_rejects_invalid_date() {
    use tower::ServiceExt;
    let f = Fixture::new();
    let mut state = crate::AppState::new(10012, 10013);
    let cache = Cache {
        version: 1,
        exclusion_hash: hash(&exclusions().0.join("\n")),
        updated_at: at("2026-10-01T12:00:00+09:00"),
        ..Cache::default()
    };
    state.workload = Arc::new(Workload {
        sources: f.sources.clone(),
        cache: RwLock::new(cache),
        phase: RwLock::new(String::new()),
    });
    let router = crate::app(state);
    let response = router
        .clone()
        .oneshot(
            axum::http::Request::builder()
                .uri("/api/agent/workload?date=2026-10-01")
                .header("host", "127.0.0.1:10012")
                .body(axum::body::Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(response.status(), StatusCode::OK);
    let bytes = axum::body::to_bytes(response.into_body(), 1024 * 1024)
        .await
        .unwrap();
    let v: Value = serde_json::from_slice(&bytes).unwrap();
    assert_eq!(v["ok"], true);
    assert_eq!(v["data"]["date"], "2026-10-01");
    assert_eq!(v["data"]["timeline"].as_array().unwrap().len(), 96);
    let response = router
        .oneshot(
            axum::http::Request::builder()
                .uri("/api/agent/workload?date=2026-02-30")
                .header("host", "127.0.0.1:10012")
                .body(axum::body::Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(response.status(), StatusCode::BAD_REQUEST);
}
