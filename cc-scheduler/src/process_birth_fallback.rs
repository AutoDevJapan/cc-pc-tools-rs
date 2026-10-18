//! CIM is read-only and exposes creation times even when a process handle is denied.
//! Imprecise results are bounds, never an invented exact process identity.
use super::BIRTH_FALLBACK_MARGIN as MARGIN;
use crate::common::{invocation, Error, Result, Runner};
use serde_json::Value;
use std::collections::{BTreeMap, BTreeSet};

pub(super) fn decode(text: &str, wanted: &BTreeSet<u32>) -> Result<BTreeMap<u32, (u64, u64)>> {
    if text.len() > 32 * 1024 {
        return Err(Error("BirthMetadataInvalid"));
    }
    let value: Value = serde_json::from_str(text.trim_start_matches('\u{feff}'))?;
    let rows = value.as_array().ok_or(Error("BirthMetadataInvalid"))?;
    let mut found = BTreeMap::new();
    for row in rows {
        let object = row
            .as_object()
            .filter(|v| v.len() == 2)
            .ok_or(Error("BirthMetadataInvalid"))?;
        let pid = object
            .get("pid")
            .and_then(Value::as_u64)
            .and_then(|p| u32::try_from(p).ok())
            .filter(|p| wanted.contains(p))
            .ok_or(Error("BirthMetadataInvalid"))?;
        let created = object
            .get("created")
            .and_then(Value::as_str)
            .filter(|s| !s.is_empty() && s.len() <= 20 && s.bytes().all(|b| b.is_ascii_digit()))
            .and_then(|s| s.parse::<u64>().ok())
            .filter(|v| *v > MARGIN && *v < u64::MAX - MARGIN)
            .ok_or(Error("BirthMetadataInvalid"))?;
        // CIM permits providers not to evaluate the fractional field. A full
        // second also conservatively covers its rounding or truncation.
        if found
            .insert(pid, (created - MARGIN, created + MARGIN))
            .is_some()
        {
            return Err(Error("BirthMetadataInvalid"));
        }
    }
    Ok(found)
}

pub(super) fn query(runner: &mut dyn Runner, pids: &[u32]) -> Result<BTreeMap<u32, (u64, u64)>> {
    let wanted: BTreeSet<_> = pids.iter().copied().collect();
    if wanted.is_empty() {
        return Ok(BTreeMap::new());
    }
    if wanted.len() > 64 || wanted.contains(&0) {
        return Err(Error("BirthMetadataInvalid"));
    }
    let filter = wanted
        .iter()
        .map(|p| format!("ProcessId={p}"))
        .collect::<Vec<_>>()
        .join(" OR ");
    let script = format!(
        "$ErrorActionPreference='Stop';$birthRecords=@(Get-CimInstance Win32_Process -Filter '{filter}' -Property ProcessId,CreationDate -ErrorAction Stop|ForEach-Object{{if($null -eq $_.CreationDate){{throw 'birth unavailable'}};[pscustomobject]@{{pid=[uint32]$_.ProcessId;created=$_.CreationDate.ToFileTimeUtc().ToString([Globalization.CultureInfo]::InvariantCulture)}}}});ConvertTo-Json -InputObject $birthRecords -Compress"
    );
    let powershell = std::path::PathBuf::from(
        std::env::var_os("SystemRoot").unwrap_or_else(|| "C:/Windows".into()),
    )
    .join("System32/WindowsPowerShell/v1.0/powershell.exe");
    let executable = powershell.to_str().ok_or(Error("BirthMetadataInvalid"))?;
    let output = runner.run(&invocation(
        &[
            executable,
            "-NoProfile",
            "-NonInteractive",
            "-Command",
            &script,
        ],
        Some(3),
    ))?;
    if output.code != 0 {
        return Err(Error("BirthMetadataUnavailable"));
    }
    decode(&output.stdout, &wanted)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::common::{Invocation, Output};
    #[derive(Default)]
    struct Recording {
        calls: Vec<Invocation>,
        failed: bool,
    }
    impl Runner for Recording {
        fn run(&mut self, command: &Invocation) -> Result<Output> {
            self.calls.push(command.clone());
            Ok(Output {
                code: if self.failed { 1 } else { 0 },
                stdout: r#"[{"pid":42,"created":"134367176974490920"}]"#.into(),
                stderr: String::new(),
            })
        }
    }
    #[test]
    fn query_is_batched_read_only_and_deadline_bounded() {
        let mut r = Recording::default();
        assert!(query(&mut r, &[42, 42]).unwrap().contains_key(&42));
        assert_eq!(r.calls.len(), 1);
        assert_eq!(r.calls[0].timeout, Some(std::time::Duration::from_secs(3)));
        let script = r.calls[0].args.last().unwrap();
        assert!(script.contains("-Property ProcessId,CreationDate"));
        assert_eq!(script.matches("ProcessId=42").count(), 1);
        assert!(!script.contains("CommandLine"));
        assert!(!script.contains("GetEnvironment"));
        assert!(query(&mut r, &[0]).is_err());
        assert!(query(&mut r, &(1..=65).collect::<Vec<_>>()).is_err());
        assert_eq!(r.calls.len(), 1);
        r.failed = true;
        assert!(query(&mut r, &[42]).is_err());
    }
    #[test]
    fn imprecise_birth_does_not_claim_an_exact_identity() {
        let wanted = BTreeSet::from([42]);
        let r = decode(r#"[{"pid":42,"created":"134367176974490920"}]"#, &wanted).unwrap();
        assert_eq!(r[&42], (134367176964490920, 134367176984490920));
        for bad in [
            r#"{"pid":42,"created":"123"}"#,
            r#"[{"pid":43,"created":"123"}]"#,
            r#"[{"pid":42,"created":"123"},{"pid":42,"created":"124"}]"#,
            r#"[{"pid":42,"created":123}]"#,
            r#"[{"pid":42,"created":"x123"}]"#,
            r#"[{"pid":42,"created":"123","name":"private"}]"#,
        ] {
            assert!(decode(bad, &wanted).is_err());
        }
        assert!(decode("[]", &wanted).unwrap().is_empty());
    }
}
