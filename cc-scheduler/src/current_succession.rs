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
        let Some(id) = item["id"].as_u64().filter(|id| *id > 0) else {
            return false;
        };
        let spec = s(&item["spec"]);
        if root.is_empty() || spec.is_empty() || s(&item["project"]).is_empty() {
            return false;
        }
        let guard = self.tools.join("client-records/scrub391.exe");
        let Some(guard) = guard.to_str() else {
            return false;
        };
        let Ok(output) = self.runner.run(&invocation(
            &[guard, "--verify-current-succession", &id.to_string()],
            Some(40),
        )) else {
            return false;
        };
        if output.code != 0 {
            return false;
        }
        let Ok(receipt) = serde_json::from_str::<Value>(&output.stdout) else {
            return false;
        };
        if receipt["domain"] != "current-product-succession-v1"
            || receipt["verified"] != true
            || receipt["origin_todo_id"] != 2834
            || receipt["todo_id"] != item["id"]
            || receipt["project"] != item["project"]
            || receipt["original_registry_recovered"] != false
            || receipt["registered_path_sha256"] != path_digest(root)
            || receipt["spec_path_sha256"] != path_digest(spec)
        {
            return false;
        }
        // No spec body is read before the official verifier succeeds. Bound both
        // the initial file and growth during reading; errors never become empty data.
        let Ok(file) = File::open(spec) else {
            return false;
        };
        if !file
            .metadata()
            .is_ok_and(|m| m.is_file() && m.len() <= MAX_SPEC)
        {
            return false;
        }
        let mut bytes = Vec::new();
        if file.take(MAX_SPEC + 1).read_to_end(&mut bytes).is_err()
            || bytes.len() as u64 > MAX_SPEC
            || receipt["spec_sha256"] != digest(&bytes)
        {
            return false;
        }
        self.guard_workspace_path(root).is_ok()
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
    fn real_backend_current_receipt_binds_every_field_and_bounded_spec_bytes() {
        for gate in [
            "pass",
            "normalized",
            "unsupported",
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
                "registered_path_sha256":digest(b"d:/work/ao"),
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
                code: if gate == "unsupported" { 1 } else { 0 },
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
