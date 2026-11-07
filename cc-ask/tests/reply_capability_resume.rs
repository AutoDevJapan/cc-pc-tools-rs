mod common;
use common::*;
use serde_json::json;

#[test]
fn sessionless_capability_migration_retires_the_old_conversation_and_keeps_the_answer() {
    let mut h = Harness::new();
    let dir = h.ctx.jobs.join("jobs/original");
    std::fs::create_dir_all(&dir).unwrap();
    let job = json!({"status":"failed","model":"gpt-6.1-sol","effort":"max",
        "cwd":h.root.path(),"sandbox":"danger-full-access","mcp_profile":"minimal",
        "scope_source":"policy","discord_thread_id":"200","thread_id":"authors-session"});
    std::fs::write(dir.join("job.json"), job.to_string()).unwrap();
    std::fs::write(dir.join("prompt.md"), "元の仕事を続ける").unwrap();
    let mut post = message(
        "200",
        "a/bで返信してください\n-# ジョブ original。続きはこのスレッドに返信してください。",
        "BOT",
    );
    post["author"]["bot"] = json!(true);
    h.fake
        .borrow_mut()
        .messages
        .insert("200".into(), vec![post]);
    h.save(
        &json!({"questions":[{"thread_id":"200","after":"200","question_message":"200",
        "cwd":h.root.path(),"title":"継続","open":true,"source":"posted"}]}),
    );
    let mid = h.reply("aで進めてください", "200");
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    let fake = h.fake.borrow();
    assert_eq!(fake.launches.len(), 1);
    assert_eq!(fake.launches[0]["resume_session"], "");
    assert_eq!(fake.launches[0]["sandbox"], "inherit");
    assert_eq!(fake.launches[0]["mcp_profile"], "inherit");
    assert!(fake.launches[0]["prompt"]
        .as_str()
        .unwrap()
        .contains("aで進めてください"));
    assert_eq!(h.state()["answer_receipts"][mid]["handled_by"], "job1");
}
