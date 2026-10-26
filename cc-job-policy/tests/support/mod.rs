#![allow(dead_code)]
pub mod initial_review;
use cc_job_policy::{subwork, work_owner};
use serde_json::{Value, json};
use std::{
    fs,
    path::{Path, PathBuf},
};

pub struct Fixture {
    pub dir: tempfile::TempDir,
    pub repo: PathBuf,
    pub db: Value,
    pub jobs: Vec<Value>,
}
pub fn tree(repo: &Path, name: &str, branch: &str) -> PathBuf {
    let path = repo.with_file_name("repo-wt").join(name);
    let git = repo.join(".git/worktrees").join(name);
    fs::create_dir_all(&path).unwrap();
    fs::create_dir_all(&git).unwrap();
    fs::write(path.join(".git"), format!("gitdir: {}", git.display())).unwrap();
    fs::write(git.join("commondir"), "../..").unwrap();
    fs::write(git.join("HEAD"), format!("ref: refs/heads/{branch}")).unwrap();
    path
}
impl Fixture {
    pub fn new() -> Self {
        let dir = tempfile::tempdir().unwrap();
        let repo = dir.path().join("repo");
        fs::create_dir_all(repo.join(".git")).unwrap();
        fs::write(
            repo.join(".git/config"),
            "[remote \"origin\"]\nurl = https://github.com/adj/game.git\n",
        )
        .unwrap();
        fs::write(repo.join(".git/HEAD"), "ref: refs/heads/main").unwrap();
        let parent_tree = tree(&repo, "parent", "work/pr86");
        let child_tree = tree(&repo, "todo-2-check", "scheduler/todo-2-check");
        let spec = dir.path().join("spec.md");
        fs::write(&spec, "Inspect the fixed change and return the result.").unwrap();
        let mut owner = work_owner::checkout(parent_tree.to_str().unwrap());
        owner["pr"] = json!(86);
        let jobs = vec![
            json!({"id":"parent","todo_id":1,"status":"running","subwork_live":true,"model":"gpt-6.1-sol","requested_model":"gpt-6.1-sol","effort":"max",
            "cwd":parent_tree,"work_owner":owner,"created_at":"2026-10-10T01:00:00+09:00"}),
        ];
        let db = json!({"projects":{"demo":repo,"alias":repo},"next_id":3,"items":[
            {"id":1,"project":"demo","status":"doing","title":"PR86","jobs":["parent"],
             "scheduler":{"job":"parent","worktree":parent_tree}},
            {"id":2,"project":"alias","status":"todo","title":"PR86 check","jobs":[],"note":"evidence", "spec":spec,
             "dispatchable":true,"needs":[],"scheduler":{"worktree":child_tree,"branch":"scheduler/todo-2-check","key":"22222222222222222222222222222222"}}
        ]});
        Self {
            dir,
            repo,
            db,
            jobs,
        }
    }
    pub fn register(&mut self) {
        self.db["items"][1]["subwork"] = subwork::register(
            &self.db,
            &self.db["items"][1],
            1,
            "parent",
            "check",
            &self.jobs,
            false,
        )
        .unwrap();
        self.db["items"][1]["scheduler"]["subwork_admission"] =
            subwork::admission(&self.db["items"][1]);
    }
    pub fn child_job(&self) -> Value {
        let row = &self.db["items"][1];
        let mut owner = work_owner::checkout(row["scheduler"]["worktree"].as_str().unwrap());
        owner["pr"] = json!(86);
        json!({"id":"child","todo_id":2,"status":"queued","cwd":row["scheduler"]["worktree"],
            "launch_key":row["scheduler"]["key"],"subwork":row["subwork"],"subwork_spec":row["spec"],
            "assigned_needs":row["needs"],"work_owner":owner,"created_at":"2026-10-10T01:01:00+09:00"})
    }
}
