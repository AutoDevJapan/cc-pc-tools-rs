//! Read only the official guard's finite receipt, then bind it to current inputs.
use super::RealBackend;
use crate::common::{invocation, s};
use cc_job_policy::product_retry::digest;
use serde_json::Value;
use std::{fs::File, io::Read};

const MAX_SPEC: u64 = 1024 * 1024;

fn path_digest(path: &str) -> String {
    digest(
        path.replace('\\', "/")
            .trim_end_matches('/')
            .to_lowercase()
            .as_bytes(),
    )
}

impl RealBackend {
    pub(super) fn verify_current_product(&mut self, item: &Value, root: &str) -> bool {
        self.current_product_receipt(item, root).is_some()
    }

    pub(super) fn current_product_receipt(&mut self, item: &Value, root: &str) -> Option<Value> {
        let id = item["id"].as_u64().filter(|id| *id > 0)?;
        let spec = s(&item["spec"]);
        if root.is_empty() || spec.is_empty() || s(&item["project"]).is_empty() {
            return None;
        }
        let guard = self.tools.join("client-records/scrub391.exe");
        let guard = guard.to_str()?;
        let Ok(output) = self.runner.run(&invocation(
            &[guard, "--verify-current-succession", &id.to_string()],
            Some(40),
        )) else {
            return None;
        };
        if output.code != 0 {
            return None;
        }
        let Ok(receipt) = serde_json::from_str::<Value>(&output.stdout) else {
            return None;
        };
        let original_source = receipt["origin_todo_id"] == 2834;
        let current_request = receipt["origin_todo_id"] == 3638
            && receipt["actor_todo_id"] == 3650
            && [1626, 3277, 3549, 3623].contains(&id)
            && receipt["source_kind"] == "native_user"
            && receipt["source_session"] == "01a16c0c-18a9-7570-ab4e-9522ffa8d6d0"
            && receipt["source_event_sha256"]
                == "e0cff4ef85745ffb93ddcc36d1fb9626c4c098967406d8875a672d58c1684465"
            && receipt["source_at"] == "2026-10-24T11:26:20.672Z";
        if receipt["domain"] != "current-product-succession-v1"
            || receipt["verified"] != true
            || !(original_source || current_request)
            || receipt["todo_id"] != item["id"]
            || receipt["project"] != item["project"]
            || receipt["original_registry_recovered"] != false
            || receipt["registered_path_sha256"] != path_digest(root)
            || receipt["spec_path_sha256"] != path_digest(spec)
        {
            return None;
        }
        // No spec body is read before the official verifier succeeds. Bound both
        // the initial file and growth during reading; errors never become empty data.
        let Ok(file) = File::open(spec) else {
            return None;
        };
        if !file
            .metadata()
            .is_ok_and(|m| m.is_file() && m.len() <= MAX_SPEC)
        {
            return None;
        }
        let mut bytes = Vec::new();
        if file.take(MAX_SPEC + 1).read_to_end(&mut bytes).is_err()
            || bytes.len() as u64 > MAX_SPEC
            || receipt["spec_sha256"] != digest(&bytes)
        {
            return None;
        }
        self.guard_workspace_path(root).ok()?;
        Some(receipt)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        backend::Backend,
        common::{Invocation, Output, Result, Runner},
    };
    use serde_json::json;
    use std::sync::{Arc, Mutex};

    struct Guard {
        text: String,
        code: i32,
        root_allowed: bool,
        calls: Arc<Mutex<Vec<Invocation>>>,
    }
    impl Runner for Guard {
        fn run(&mut self, c: &Invocation) -> Result<Output> {
            self.calls.lock().unwrap().push(c.clone());
            let verify = c.args.iter().any(|a| a == "--verify-current-succession");
            assert!(verify || c.args.iter().any(|a| a == "--guard-path"));
            assert!(c.program.ends_with("scrub391.exe"));
            if self.code == -1 {
                return Err(crate::common::Error("TimeoutExpired"));
            }
            Ok(Output {
                code: if verify { self.code } else { 0 },
                stdout: if verify {
                    self.text.clone()
                } else {
                    json!({"client_resource": !self.root_allowed}).to_string()
                },
                stderr: String::new(),
            })
        }
    }
    #[test]
    fn current_owner_request_binds_its_own_source_without_relabeling_the_old_origin() {
        for gate in [
            "pass", "actor", "kind", "session", "event", "at", "unknown", "spec", "root",
        ] {
            let temp = tempfile::tempdir().unwrap();
            let spec = temp.path().join("original.md");
            std::fs::write(&spec, b"original unchanged work").unwrap();
            let mut item = json!({"id":3277,"project":"original","spec":spec});
            let root = "D:/original";
            let mut receipt = json!({"domain":"current-product-succession-v1","verified":true,
                "origin_todo_id":3638,"actor_todo_id":3650,"todo_id":3277,"project":"original",
                "registered_path_sha256":path_digest(root),"spec_path_sha256":path_digest(spec.to_str().unwrap()),
                "spec_sha256":digest(b"original unchanged work"),"original_registry_recovered":false,
                "source_kind":"native_user","source_session":"01a16c0c-18a9-7570-ab4e-9522ffa8d6d0",
                "source_event_sha256":"e0cff4ef85745ffb93ddcc36d1fb9626c4c098967406d8875a672d58c1684465",
                "source_at":"2026-10-24T11:26:20.672Z"});
            match gate {
                "actor" => receipt["actor_todo_id"] = json!(999),
                "kind" => receipt["source_kind"] = json!("assistant_quote"),
                "session" => receipt["source_session"] = json!("another-session"),
                "event" => receipt["source_event_sha256"] = json!("other"),
                "at" => receipt["source_at"] = json!("2026-10-13T00:00:00Z"),
                "unknown" => {
                    item["id"] = json!(2467);
                    receipt["todo_id"] = json!(2467);
                }
                "spec" => std::fs::write(&spec, b"changed after qualification").unwrap(),
                "root" => receipt["registered_path_sha256"] = json!(path_digest("D:/other")),
                _ => (),
            }
            let mut backend = RealBackend::new(temp.path().join("scheduler"));
            backend.runner = Box::new(Guard {
                text: receipt.to_string(),
                code: 0,
                root_allowed: true,
                calls: Arc::new(Mutex::new(vec![])),
            });
            assert_eq!(
                backend.current_product_allowed(&item, root),
                gate == "pass",
                "{gate}"
            );
        }
    }
    #[test]
    fn real_backend_current_receipt_binds_every_field_and_bounded_spec_bytes() {
        for gate in [
            "pass",
            "normalized",
            "unsupported",
            "runner_error",
            "json",
            "domain",
            "verified",
            "origin",
            "id",
            "project",
            "root",
            "spec_path",
            "body",
            "recovered",
            "missing_field",
            "missing_spec",
            "large_spec",
            "directory",
            "protected_root",
            "old_item",
            "changed_item",
        ] {
            let temp = tempfile::tempdir().unwrap();
            let spec = temp.path().join("spec.md");
            std::fs::write(&spec, b"exact saved spec\r\n").unwrap();
            let mut item = json!({"id":2839,"project":"GoDD-AO","spec":spec});
            let root = if gate == "normalized" {
                "D:\\WORK\\AO\\"
            } else {
                "d:/work/ao"
            };
            let mut receipt = json!({"domain":"current-product-succession-v1","verified":true,
                "origin_todo_id":2834,"todo_id":2839,"project":"GoDD-AO",
                "registered_path_sha256":"1782e36ad9ee1d06207809d581efd0e0056745d2c6577a8e4c576bbad3ac78b1",
                "spec_path_sha256":path_digest(spec.to_str().unwrap()),
                "spec_sha256":digest(b"exact saved spec\r\n"),"original_registry_recovered":false});
            match gate {
                "domain" => receipt["domain"] = json!("other"),
                "verified" => receipt["verified"] = json!("true"),
                "origin" => receipt["origin_todo_id"] = json!(1161),
                "id" => receipt["todo_id"] = json!(999),
                "project" => receipt["project"] = json!("other"),
                "root" => receipt["registered_path_sha256"] = json!(digest(b"d:/other")),
                "spec_path" => receipt["spec_path_sha256"] = json!(digest(b"d:/other.md")),
                "body" => std::fs::write(&spec, b"changed").unwrap(),
                "recovered" => receipt["original_registry_recovered"] = json!(true),
                "missing_field" => {
                    receipt
                        .as_object_mut()
                        .unwrap()
                        .remove("original_registry_recovered");
                }
                "missing_spec" => std::fs::remove_file(&spec).unwrap(),
                "large_spec" => std::fs::write(&spec, vec![b'x'; MAX_SPEC as usize + 1]).unwrap(),
                "directory" => {
                    std::fs::remove_file(&spec).unwrap();
                    std::fs::create_dir(&spec).unwrap();
                }
                "old_item" => item["id"] = json!(1161),
                "changed_item" => item["project"] = json!("other"),
                _ => {}
            }
            let calls = Arc::new(Mutex::new(vec![]));
            let mut backend = RealBackend::new(temp.path().join("scheduler"));
            backend.runner = Box::new(Guard {
                text: if gate == "json" {
                    "not json".into()
                } else {
                    receipt.to_string()
                },
                code: match gate {
                    "unsupported" => 1,
                    "runner_error" => -1,
                    _ => 0,
                },
                root_allowed: gate != "protected_root",
                calls: calls.clone(),
            });
            assert_eq!(
                backend.current_product_allowed(&item, root),
                matches!(gate, "pass" | "normalized"),
                "{gate}"
            );
            let calls = calls.lock().unwrap();
            assert_eq!(
                calls[0].args[calls[0].args.len() - 2..],
                [
                    "--verify-current-succession".to_string(),
                    item["id"].to_string()
                ]
            );
            assert_eq!(calls[0].timeout, Some(std::time::Duration::from_secs(40)));
            assert_eq!(
                calls.len(),
                if matches!(gate, "pass" | "normalized" | "protected_root") {
                    2
                } else {
                    1
                },
                "{gate}"
            );
        }
    }
}
