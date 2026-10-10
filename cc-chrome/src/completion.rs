//! Explicit per-tab browser work completion, independent of worker lifetime.
use crate::*;

#[derive(Serialize, Deserialize)]
struct Request {
    record: Record,
    close: bool,
    reason: String,
}
fn request_path(dir: &Path, path: &Path) -> PathBuf {
    dir.join("chrome-tab-requests")
        .join(path.file_name().unwrap())
}
pub(crate) fn requested(
    dir: &Path,
    path: &Path,
    record: &Record,
) -> io::Result<Option<(bool, String)>> {
    match fs::read(request_path(dir, path)) {
        Ok(bytes) => {
            let request: Request = serde_json::from_slice(&bytes)?;
            if request.record != *record || request.reason.trim().is_empty() {
                return Err(io::Error::other("tab completion identity mismatch"));
            }
            Ok(Some((request.close, request.reason)))
        }
        Err(e) if e.kind() == io::ErrorKind::NotFound => Ok(None),
        Err(e) => Err(e),
    }
}
pub(crate) fn remove_request(dir: &Path, path: &Path) -> io::Result<()> {
    match fs::remove_file(request_path(dir, path)) {
        Err(e) if e.kind() != io::ErrorKind::NotFound => Err(e),
        _ => Ok(()),
    }
}
fn mark(jobs: &Path, job: &str, id: &str, close: bool, reason: &str) -> io::Result<()> {
    if !safe_job(job) || uuid::Uuid::parse_str(id).is_err() || reason.trim().is_empty() {
        return Err(io::Error::other(
            "expected job, record UUID and completion evidence",
        ));
    }
    let dir = jobs.join(job);
    let paths: Vec<_> = ["chrome-tabs", "chrome-emergency-tabs"]
        .iter()
        .map(|kind| dir.join(kind).join(format!("{id}.json")))
        .filter(|path| path.exists())
        .collect();
    if paths.len() != 1 {
        return Err(io::Error::other("expected one owned tab record"));
    }
    let path = &paths[0];
    let record: Record = serde_json::from_slice(&fs::read(path)?)?;
    if record.job != job
        || record.tab == 0
        || record.instance.is_empty()
        || record.generation.is_empty()
    {
        return Err(io::Error::other("invalid tab ownership"));
    }
    if close && requested(&dir, path, &record)?.is_some_and(|(close, _)| !close) {
        return Err(io::Error::other("owner-retained tab cannot be closed"));
    }
    lifecycle::atomic_json(
        &request_path(&dir, path),
        &Request {
            record,
            close,
            reason: reason.into(),
        },
    )
}
/// Persist before closing: the existing minute sweep retries even while the owner runs.
pub fn complete(jobs: &Path, job: &str, id: &str, close: bool, reason: &str) -> io::Result<Sweep> {
    let lock = fs::OpenOptions::new()
        .create(true)
        .truncate(false)
        .read(true)
        .write(true)
        .open(jobs.join("chrome-cleanup.lock"))?;
    // Explicit completion must be journaled, not silently dropped on lock contention.
    lock.lock()?;
    mark(jobs, job, id, close, reason)?;
    if !close {
        return Ok(Sweep {
            kept: 1,
            ..Default::default()
        });
    }
    #[cfg(windows)]
    {
        sweep_selected(
            jobs,
            Some(job),
            Some(id),
            epoch(),
            false,
            &mut native::NativeCloser,
        )
    }
    #[cfg(not(windows))]
    {
        Err(io::Error::other("Chrome extension requires Windows"))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    struct Fake {
        calls: Vec<u64>,
        fail: bool,
        gone: bool,
    }
    impl Closer for Fake {
        fn close(&mut self, r: &Record) -> io::Result<bool> {
            self.calls.push(r.tab);
            if self.fail {
                Err(io::Error::other("offline"))
            } else {
                Ok(!self.gone)
            }
        }
    }
    fn setup() -> (tempfile::TempDir, String, String) {
        let root = tempfile::tempdir().unwrap();
        let dir = root.path().join("live");
        lifecycle::atomic_json(&dir.join("job.json"), &json!({"id":"live","status":"running","last_event_at":chrono::Utc::now().to_rfc3339()})).unwrap();
        let mut ids = vec![];
        for tab in [11, 22] {
            let r = Record {
                job: "live".into(),
                pipe: "pipe".into(),
                instance: "extension".into(),
                generation: "boot".into(),
                session: "session".into(),
                turn: "turn".into(),
                tab,
                created: epoch(),
            };
            ids.push(
                persist(&dir, &r)
                    .unwrap()
                    .file_stem()
                    .unwrap()
                    .to_str()
                    .unwrap()
                    .to_owned(),
            );
        }
        (root, ids.remove(0), ids.remove(0))
    }
    #[test]
    fn phase_end_closes_only_selected_tab_of_running_worker_and_retries() {
        let (root, id, _) = setup();
        mark(root.path(), "live", &id, true, "browser work completed").unwrap();
        let mut fake = Fake {
            calls: vec![],
            fail: true,
            gone: false,
        };
        let first = sweep_selected(
            root.path(),
            Some("live"),
            Some(&id),
            epoch(),
            false,
            &mut fake,
        )
        .unwrap();
        assert_eq!((first.failed, first.closed), (1, 0));
        fake.fail = false;
        let retry = sweep_with(root.path(), None, epoch(), false, &mut fake).unwrap();
        assert_eq!((retry.closed, retry.kept, retry.failed), (1, 1, 0));
        assert_eq!(fake.calls, [11, 11]);
        let history: Value = serde_json::from_slice(
            &fs::read(
                root.path()
                    .join(format!("live/chrome-tab-history/{id}.json")),
            )
            .unwrap(),
        )
        .unwrap();
        assert_eq!(history["completion_reason"], "browser work completed");
    }
    #[test]
    fn retained_delivery_survives_terminal_job_and_explicit_close() {
        let (root, id, _) = setup();
        mark(
            root.path(),
            "live",
            &id,
            false,
            "owner asked to keep delivery",
        )
        .unwrap();
        assert!(mark(root.path(), "live", &id, true, "done").is_err());
        lifecycle::atomic_json(
            &root.path().join("live/job.json"),
            &json!({"id":"live","status":"succeeded"}),
        )
        .unwrap();
        let mut fake = Fake {
            calls: vec![],
            fail: false,
            gone: false,
        };
        let r = sweep_with(root.path(), None, epoch(), false, &mut fake).unwrap();
        assert_eq!((r.closed, r.kept), (1, 1));
        assert_eq!(fake.calls, [22]);
    }
    #[test]
    fn running_worker_absence_retires_only_gone_records_without_close() {
        struct Probe;
        impl Closer for Probe {
            fn absent(&mut self, r: &Record) -> io::Result<bool> {
                Ok(r.tab == 11)
            }
            fn close(&mut self, _: &Record) -> io::Result<bool> {
                panic!("live tab must not be closed")
            }
        }
        let (root, _, _) = setup();
        let r = sweep_with(root.path(), None, epoch(), false, &mut Probe).unwrap();
        assert_eq!((r.gone, r.closed, r.kept), (1, 0, 1));
    }
    #[test]
    fn identity_change_unowned_and_already_gone_are_distinct() {
        let (root, id, _) = setup();
        assert!(mark(root.path(), "live", "../other", true, "done").is_err());
        assert!(
            mark(
                root.path(),
                "live",
                &uuid::Uuid::new_v4().to_string(),
                true,
                "done"
            )
            .is_err()
        );
        mark(root.path(), "live", &id, true, "done").unwrap();
        let path = root.path().join(format!("live/chrome-tabs/{id}.json"));
        let mut record: Record = serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
        record.generation = "different".into();
        lifecycle::atomic_json(&path, &record).unwrap();
        let mut fake = Fake {
            calls: vec![],
            fail: false,
            gone: true,
        };
        let r = sweep_with(root.path(), None, epoch(), false, &mut fake).unwrap();
        assert_eq!((r.failed, r.kept), (1, 1));
        assert!(fake.calls.is_empty());
        record.generation = "boot".into();
        lifecycle::atomic_json(&path, &record).unwrap();
        mark(root.path(), "live", &id, true, "done").unwrap();
        let r = sweep_with(root.path(), None, epoch(), false, &mut fake).unwrap();
        assert_eq!((r.gone, r.closed, r.kept), (1, 0, 1));
    }
}
