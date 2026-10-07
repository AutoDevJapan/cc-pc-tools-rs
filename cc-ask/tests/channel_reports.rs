mod common;
use cc_ask::{commands::cmd_log, core::*};
use common::Harness;
use serde_json::json;

#[test]
fn legacy_channel_topic_and_canonical_topic_share_one_silent_thread() {
    let mut h = Harness::new();
    let old = cmd_log(
        &mut h.ctx,
        "【Skills化】motocrab-loveチャンネルの解析",
        "動画を解析しました。",
        None,
        true,
    )
    .unwrap();
    let next = cmd_log(
        &mut h.ctx,
        "Motocrab",
        "字幕とSkillを更新しました。",
        None,
        false,
    )
    .unwrap();
    assert_eq!(old["thread_id"], next["thread_id"]);
    assert_eq!(next["url"], "https://discord.com/channels/G/300");
    let fake = h.fake.borrow();
    assert_eq!(
        fake.http_calls
            .iter()
            .filter(|c| s(&c["url"]).ends_with("/channels/T/threads"))
            .count(),
        1
    );
    for (_, post) in &fake.posts {
        assert_eq!(post["flags"], SILENT);
        assert_eq!(post["allowed_mentions"], json!({"parse":[]}));
    }
}

#[test]
fn previously_saved_legacy_topic_is_reused_and_renamed() {
    let mut h = Harness::new();
    atomic_json(
        &h.root.path().join("log_threads.json"),
        &json!({"【健康チャンネル】動画の作り方と試作":"100"}),
        1,
    )
    .unwrap();
    let out = cmd_log(
        &mut h.ctx,
        "健康ナビ",
        "前回の38本の解析を引き継ぎます。",
        None,
        false,
    )
    .unwrap();
    assert_eq!(out["thread_id"], "100");
    assert!(h
        .fake
        .borrow()
        .http_calls
        .iter()
        .all(|c| !s(&c["url"]).ends_with("/channels/T/threads")));
    assert_eq!(
        h.fake.borrow().patches[0].1,
        json!({"archived":false,"name":"健康ナビ"})
    );
    assert_eq!(
        read_json(&h.root.path().join("log_threads.json")).unwrap()["健康ナビ"],
        "100"
    );
    cmd_log(
        &mut h.ctx,
        "健康ナビ",
        "次の測定を記録しました。",
        None,
        false,
    )
    .unwrap();
    assert_eq!(h.fake.borrow().patches[1].1, json!({"archived":false}));
}

#[test]
fn channel_topics_are_separate_from_each_other_and_cleanup() {
    let mut h = Harness::new();
    let topics = [
        "麻臭 a.k.a. mash",
        "XB Bittensor",
        "Motocrab",
        "健康ナビ",
        "EtymoMaster",
        "AutoDevJapan",
        "後片付けの記録",
    ];
    let mut known = json!({});
    for (index, topic) in topics.iter().enumerate() {
        let id = (500 + index).to_string();
        known[*topic] = json!(id);
        h.fake.borrow_mut().threads.insert(id.clone(), json!({"id":id,"type":11,"guild_id":"G","name":topic,"thread_metadata":{"archived":false}}));
        h.fake.borrow_mut().messages.insert(id, vec![]);
    }
    atomic_json(&h.root.path().join("log_threads.json"), &known, 1).unwrap();
    for (index, topic) in topics.iter().enumerate() {
        let out = cmd_log(&mut h.ctx, topic, "活動を記録しました。", None, false).unwrap();
        assert_eq!(out["thread_id"], (500 + index).to_string());
    }
    assert!(h
        .fake
        .borrow()
        .http_calls
        .iter()
        .all(|c| !s(&c["url"]).ends_with("/channels/T/threads")));
}
