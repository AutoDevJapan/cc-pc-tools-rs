//! Effective launch capabilities. Saved routing hints never override a fixed reader.
use crate::*;
use std::path::{Path, PathBuf};

#[derive(Clone, Copy)]
pub(crate) struct Profile {
    summary: bool,
    fixed_review: bool,
}
impl Profile {
    pub(crate) fn of(job: &Value) -> Self {
        Self {
            summary: s(job, "context_phase") == "summarize",
            fixed_review: !job["fixed_read_only_review"].is_null()
                || !job["fixed_review_requalification"].is_null()
                || job["initial_review"].is_object(),
        }
    }
    pub(crate) fn isolated(self) -> bool {
        self.summary || self.fixed_review
    }
    pub(crate) fn summary(self) -> bool {
        self.summary
    }
    pub(crate) fn host_tools(self) -> bool {
        !self.isolated()
    }
    pub(crate) fn policy(self, job: &Value) -> cc_job_policy::Policy {
        let mut policy = cc_job_policy::for_sandbox(if self.isolated() {
            "read-only"
        } else {
            s(job, "sandbox")
        });
        if !self.isolated()
            && job.get("original_product_retry").is_some()
            && job["sandbox"] == "workspace-write"
        {
            policy.sandbox = "workspace-write";
        }
        policy
    }
    fn inventory_key(self) -> &'static str {
        if self.fixed_review {
            "fixed_review_disabled_mcp_servers"
        } else {
            "context_disabled_mcp_servers"
        }
    }
    pub(crate) fn disabled_servers(self, job: &Value) -> Vec<&str> {
        let mut servers: Vec<_> = job[self.inventory_key()]
            .as_array()
            .into_iter()
            .flatten()
            .filter_map(Value::as_str)
            .collect();
        if self.fixed_review && !servers.contains(&"node_repl") {
            servers.push("node_repl");
        }
        servers
    }
}

impl Context {
    pub fn capacity_catalog_args(&self, job: &Value, args: &mut Vec<String>) -> Result<()> {
        if !crate::pool_retry::public_selector(s(job, "capacity_account_selector"))
            || crate::pool_retry::native_model(s(job, "model")).is_none()
        {
            return Ok(());
        }
        if !crate::queue::valid_job_id(s(job, "id")) {
            return Err(Error::new("invalid account-route job identity"));
        }
        let catalog = self.rt.capacity_model_catalog(job)?;
        let path = self
            .job_dir(s(job, "id"))
            .join("capacity-model-catalog.json");
        std::fs::create_dir_all(path.parent().unwrap())?;
        self.write_snapshot(&path, pretty(&catalog).as_bytes())?;
        // Root config overrides precede `resume <id> -`, including on the
        // native CLI. Never depend on a fixture accepting options anywhere.
        args.splice(
            2..2,
            [
                "-c".into(),
                format!("model_catalog_json={}", path.to_string_lossy()),
            ],
        );
        Ok(())
    }
    /// The exact builder used by supervise_attempt. Inventory failure returns
    /// before spawning; no browser bridge is exposed to an isolated worker.
    pub fn execution_spawn(&self, job: &Value) -> Result<Spawn> {
        let profile = Profile::of(job);
        let research_policy = self.research_policy(job)?;
        let prompt = self.prompt_for_attempt(job)?;
        if research_policy.is_some() {
            let mut bytes = std::fs::read(&prompt)?;
            crate::finite_research::append_guidance(&mut bytes);
            std::fs::write(&prompt, bytes)?;
        }
        let mut rendered = job.clone();
        if profile.fixed_review {
            let servers = self
                .rt
                .summary_mcp_servers(s(job, "codex"), Path::new(s(job, "cwd")))?;
            rendered[profile.inventory_key()] = json!(servers);
        }
        let mut env = self.job_environment();
        env.insert("CC_JOB_ID".into(), s(job, "id").into());
        env.insert("CODEX_JOB_ID".into(), s(job, "id").into());
        if profile.host_tools() {
            if !s(job, "chrome_pipe").is_empty() {
                env.insert(
                    "CDP_BROWSER_BACKEND_PIPE_PATH".into(),
                    s(job, "chrome_pipe").into(),
                );
                env.insert(
                    "BROWSER_AUTH_EVAL_EXACT_CDP_BACKEND_SOCKET".into(),
                    "true".into(),
                );
            }
        } else {
            env.retain(|key, _| {
                ![
                    "CDP_BROWSER_BACKEND_PIPE_PATH",
                    "BROWSER_AUTH_EVAL_EXACT_CDP_BACKEND_SOCKET",
                    "BROWSER_USE_AVAILABLE_BACKENDS",
                ]
                .iter()
                .any(|name| key.eq_ignore_ascii_case(name))
            });
        }
        let mut argv = codex_args_with_policy(
            &rendered,
            research_policy.unwrap_or_else(|| profile.policy(job)),
        );
        self.capacity_catalog_args(job, &mut argv)?;
        Ok(Spawn {
            argv,
            prompt,
            cwd: PathBuf::from(s(job, "cwd")),
            env,
            flags: NO_WINDOW,
        })
    }
}
