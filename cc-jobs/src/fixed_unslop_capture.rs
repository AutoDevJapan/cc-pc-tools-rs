//! Closed native-capture dispatch. Stored metadata is never launch authority.
//! The production issuer remains unavailable until the fixed reader, its owner
//! process and the purpose-qualified root/provider are connected together.
use crate::{Error, Result, Value, json};
use std::io::Write;

const COMMAND: &str = "capture-unslop";
const KIND: &str = "unslop_owned_capture_v1";

fn failure() -> Error {
    Error { code: 1, message: "capture_owner_unavailable".into() }
}

fn valid_key(value: &str) -> bool {
    value.len() == 32 && value.bytes().all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}

fn invalid_request() -> Error {
    Error { code: 2, message: "invalid_capture_request".into() }
}

pub(crate) fn handles(args: &[String]) -> bool {
    args.first().is_some_and(|arg| arg == COMMAND)
}

/// This fixed command takes only an idempotency key. It never accepts a path,
/// executable, argument list, PID or a claimed qualification. No job is created
/// before a normal native owner can be acquired.
pub(crate) fn dispatch(args: &[String], out: &mut dyn Write) -> Result<i32> {
    if args.len() == 2 && args[0] == COMMAND && args[1] == "--help" {
        out.write_all(b"usage: codex_jobs.exe capture-unslop --key <32 lowercase hex>\n\nThe current build does not provide a native capture owner.\n")?;
        return Ok(0);
    }
    if args.len() != 3 || args[0] != COMMAND || args[1] != "--key" || !valid_key(&args[2]) {
        return Err(invalid_request());
    }
    let result = json!({
        "kind": KIND,
        "state": "unavailable",
        "reason": "capture_owner_unavailable",
        "qualified": false,
        "data_access_granted": false,
        "update_performed": false,
        "started": false,
        "cleanup_pending": false
    });
    serde_json::to_writer(&mut *out, &result)?;
    out.write_all(b"\n")?;
    Ok(1)
}

/// A native marker can never fall through to Codex execution, prompt loading,
/// model selection or capacity retry. Wrongly typed markers are also rejected.
pub(crate) fn reject_model_route(job: &Value) -> Result<()> {
    if job.get("native_capture").is_some() || job.get("kind").and_then(Value::as_str) == Some(KIND) {
        return Err(failure());
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    const KEY: &str = "11111111111111111111111111111111";
    #[test]
    fn unconnected_command_returns_fixed_failure_without_a_job() {
        let mut out = Vec::new();
        let code=dispatch(&[COMMAND.into(),"--key".into(),KEY.into()],&mut out).unwrap();
        let value: Value=serde_json::from_slice(&out).unwrap();
        assert_eq!(code,1);
        assert_eq!(value["reason"],"capture_owner_unavailable");
        assert_eq!(value["started"],false);
        assert_eq!(value["qualified"],false);
        assert!(value.get("job_id").is_none());
        assert!(!String::from_utf8(out).unwrap().contains(KEY));
    }

    #[test]
    fn command_rejects_extra_authority_and_unknown_input() {
        for args in [
            vec![COMMAND,"--key",KEY,"--root","C:/private"],
            vec![COMMAND,"--path","C:/private"],
            vec![COMMAND,"--key","ABCDEF"],
            vec![COMMAND,"--key",KEY,"--key",KEY],
        ] {
            let mut out=Vec::new();
            let err=dispatch(&args.into_iter().map(str::to_owned).collect::<Vec<_>>(),&mut out).unwrap_err();
            assert_eq!(err.code,2);assert_eq!(err.message,"invalid_capture_request");assert!(out.is_empty());
        }
    }

    #[test]
    fn native_marker_cannot_use_model_route() {
        for job in [json!({"native_capture":null}),json!({"native_capture":true}),json!({"kind":KIND})] {
            assert_eq!(reject_model_route(&job).unwrap_err().message,"capture_owner_unavailable");
        }
        assert!(reject_model_route(&json!({"model":"preserved","kind":"ordinary"})).is_ok());
    }

}
