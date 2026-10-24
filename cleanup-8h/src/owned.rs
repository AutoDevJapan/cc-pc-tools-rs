// Finite safe-delete plan -> this process's existing handle deletion -> finish.
// No new roots, supervisor, deletion policy or configurable executable.
use super::*;
use std::{collections::BTreeMap, process::Child};

const SAFE_DELETE: &str = r"D:\CC\Tools\safe-delete\safe-delete.exe";
const MAX_PLAN: u64 = 4 * 1024 * 1024;

#[derive(Clone, Debug)]
struct Expected {
    volume: u32,
    id: u64,
    birth: u64,
    written: u64,
    size: u64,
    attributes: u32,
}

impl Expected {
    fn matches(&self, p: &Pinned, after_children: bool) -> bool {
        let i = &p.info;
        i.serial == self.volume
            && (u64::from(i.index_high) << 32 | u64::from(i.index_low)) == self.id
            && (u64::from(i.created.high) << 32 | u64::from(i.created.low)) == self.birth
            && i.attributes == self.attributes
            && i.attributes & 3 == 0
            && i.links == 1
            && (after_children && p.directory()
                || ((u64::from(i.written.high) << 32 | u64::from(i.written.low)) == self.written
                    && (u64::from(i.size_high) << 32 | u64::from(i.size_low)) == self.size))
    }
}

struct Plan {
    payload: PathBuf,
    bytes: u64,
    entries: BTreeMap<String, Expected>,
}

fn number(v: &Value, key: &str) -> io::Result<u64> {
    v.get(key).and_then(Value::as_u64).ok_or_else(invalid)
}

impl Plan {
    fn parse(wrapper: &Path, v: &Value) -> io::Result<Self> {
        if v.get("action").and_then(Value::as_str) != Some("candidate") {
            return Err(invalid());
        }
        let id = v.get("id").and_then(Value::as_str).ok_or_else(invalid)?;
        if id.len() != 32
            || !id
                .bytes()
                .all(|x| x.is_ascii_digit() || (b'a'..=b'f').contains(&x))
        {
            return Err(invalid());
        }
        let payload = PathBuf::from(v.get("path").and_then(Value::as_str).ok_or_else(invalid)?);
        if payload != wrapper.join("files") || !policy::strict_child(wrapper, &payload) {
            return Err(invalid());
        }
        let items = v
            .get("entries")
            .and_then(Value::as_array)
            .ok_or_else(invalid)?;
        if items.is_empty() || items.len() > 10_001 {
            return Err(invalid());
        }
        let mut entries = BTreeMap::new();
        let mut bytes = 0u64;
        for item in items {
            let relative = item
                .get("relative")
                .and_then(Value::as_str)
                .ok_or_else(invalid)?;
            let joined = payload.join(relative);
            if relative != "."
                && (!policy::strict_child(&payload, &joined)
                    || relative.contains('\\')
                    || relative
                        .split('/')
                        .any(|x| x.is_empty() || x == "." || x == ".." || policy::retained_name(x)))
            {
                return Err(invalid());
            }
            if number(item, "links")? != 1 {
                return Err(invalid());
            }
            let expected = Expected {
                volume: u32::try_from(number(item, "volume")?).map_err(|_| invalid())?,
                id: number(item, "file_id")?,
                birth: number(item, "birth")?,
                written: number(item, "written")?,
                size: number(item, "size")?,
                attributes: u32::try_from(number(item, "attributes")?).map_err(|_| invalid())?,
            };
            if expected.attributes & (REPARSE | 3) != 0 || entries.contains_key(relative) {
                return Err(invalid());
            }
            if expected.attributes & DIRECTORY == 0 {
                bytes = bytes.checked_add(expected.size).ok_or_else(invalid)?;
            }
            entries.insert(relative.to_owned(), expected);
        }
        if !entries.contains_key(".") || bytes != number(v, "bytes")? {
            return Err(invalid());
        }
        let volume = entries["."].volume;
        if entries.values().any(|x| x.volume != volume) || entries["."].attributes & DIRECTORY == 0
        {
            return Err(invalid());
        }
        Ok(Self {
            payload,
            bytes,
            entries,
        })
    }

    fn expected(&self, path: &Path) -> io::Result<&Expected> {
        let relative = path.strip_prefix(&self.payload).map_err(|_| invalid())?;
        let key = if relative.as_os_str().is_empty() {
            ".".to_owned()
        } else {
            relative.to_str().ok_or_else(invalid)?.replace('\\', "/")
        };
        self.entries.get(&key).ok_or_else(invalid)
    }

    fn validate(
        &self,
        path: &Path,
        budget: &Budget,
        stats: &mut Stats,
        depth: usize,
    ) -> io::Result<usize> {
        budget.check(stats)?;
        if depth > 64 || path != self.payload && !policy::strict_child(&self.payload, path) {
            return Err(invalid());
        }
        let p = Pinned::open(path, false)?;
        if !self.expected(path)?.matches(&p, false) {
            return Err(invalid());
        }
        let mut visited = 1;
        if p.directory() {
            for entry in fs::read_dir(path)? {
                visited += self.validate(&entry?.path(), budget, stats, depth + 1)?;
            }
        }
        Ok(visited)
    }

    fn remove(
        &self,
        path: &Path,
        budget: &Budget,
        stats: &mut Stats,
        depth: usize,
    ) -> io::Result<()> {
        budget.check(stats)?;
        if depth > 64 || path != self.payload && !policy::strict_child(&self.payload, path) {
            return Err(invalid());
        }
        let expected = self.expected(path)?;
        let p = Pinned::open(path, false)?;
        if !expected.matches(&p, false) {
            return Err(invalid());
        }
        let directory = p.directory();
        if directory {
            for entry in fs::read_dir(path)? {
                self.remove(&entry?.path(), budget, stats, depth + 1)?;
            }
        }
        drop(p);
        let deleting = Pinned::open(path, true)?;
        if !expected.matches(&deleting, directory) {
            return Err(invalid());
        }
        let bytes = deleting.remove()?;
        stats.logical_bytes = stats.logical_bytes.saturating_add(bytes);
        if directory {
            stats.removed_directories += 1;
        } else {
            stats.removed_files += 1;
        }
        Ok(())
    }
}

struct Lease {
    child: Child,
    capture: OwnedCapture,
    until: Instant,
    expected_id: Option<String>,
}
impl Lease {
    fn open(path: &Path, budget: &Budget) -> io::Result<(Self, Value)> {
        let capture = Path::new(RUNTIME).join(format!(
            "owned-lease-{}-{}.tmp",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap_or_default()
                .as_nanos()
        ));
        let file = OpenOptions::new()
            .create_new(true)
            .write(true)
            .open(&capture)?;
        let capture = OwnedCapture(capture);
        let child = Command::new(SAFE_DELETE)
            .arg("cleanup-lease")
            .arg("--path")
            .arg(path)
            .creation_flags(NO_WINDOW)
            .stdin(Stdio::piped())
            .stdout(Stdio::from(file))
            .stderr(Stdio::null())
            .spawn()?;
        Self::receive(child, capture, budget)
    }

    fn receive(child: Child, capture: OwnedCapture, budget: &Budget) -> io::Result<(Self, Value)> {
        let mut lease = Self {
            child,
            capture,
            until: budget.until.min(Instant::now() + Duration::from_secs(110)),
            expected_id: None,
        };
        loop {
            let bytes = lease.bytes()?;
            if let Some(end) = bytes.iter().position(|&x| x == b'\n') {
                let v: Value = serde_json::from_slice(&bytes[..end]).map_err(|_| invalid())?;
                lease.expected_id = v.get("id").and_then(Value::as_str).map(str::to_owned);
                return Ok((lease, v));
            }
            if Instant::now() >= lease.until || lease.child.try_wait()?.is_some() {
                return Err(io::Error::new(
                    io::ErrorKind::TimedOut,
                    "owned lease unavailable",
                ));
            }
            thread::sleep(Duration::from_millis(20));
        }
    }
    fn bytes(&self) -> io::Result<Vec<u8>> {
        let mut bytes = Vec::new();
        fs::File::open(&self.capture.0)?
            .take(MAX_PLAN + 1)
            .read_to_end(&mut bytes)?;
        if bytes.len() as u64 > MAX_PLAN {
            return Err(invalid());
        }
        Ok(bytes)
    }
    fn finish(&mut self) -> io::Result<()> {
        self.child
            .stdin
            .take()
            .ok_or_else(invalid)?
            .write_all(b"finish\n")?;
        loop {
            if let Some(status) = self.child.try_wait()? {
                if !status.success() {
                    return Err(invalid());
                }
                let bytes = self.bytes()?;
                let line = bytes
                    .split(|&x| x == b'\n')
                    .rfind(|x| !x.is_empty())
                    .ok_or_else(invalid)?;
                let v: Value = serde_json::from_slice(line).map_err(|_| invalid())?;
                return if v.get("state").and_then(Value::as_str) == Some("quarantine_removed")
                    && v.get("id").and_then(Value::as_str) == self.expected_id.as_deref()
                    && self.expected_id.is_some()
                {
                    Ok(())
                } else {
                    Err(invalid())
                };
            }
            if Instant::now() >= self.until {
                return Err(io::Error::new(
                    io::ErrorKind::TimedOut,
                    "owned finish deadline",
                ));
            }
            thread::sleep(Duration::from_millis(20));
        }
    }
}
impl Drop for Lease {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

fn retain(stats: &mut Stats, reason: &str, bytes: Option<u64>) {
    stats.owned_retained += 1;
    if let Some(bytes) = bytes {
        stats.owned_retained_bytes = stats.owned_retained_bytes.saturating_add(bytes);
    } else {
        stats.owned_retained_unmeasured += 1;
    }
    // Never copy paths, private output or arbitrary tool text into the log.
    let reason =
        if reason.len() <= 64 && reason.bytes().all(|x| x.is_ascii_uppercase() || x == b'_') {
            reason
        } else {
            "QUALIFICATION_FAILED"
        };
    *stats.owned_reasons.entry(reason.to_owned()).or_default() += 1;
}

pub(super) fn clean_candidate(
    path: &Path,
    apply: bool,
    budget: &Budget,
    stats: &mut Stats,
) -> io::Result<bool> {
    let (mut lease, value) = match Lease::open(path, budget) {
        Ok(v) => v,
        Err(_) => {
            retain(stats, "QUALIFICATION_UNAVAILABLE", None);
            return Ok(true);
        }
    };
    match value.get("action").and_then(Value::as_str) {
        Some("completed") => return Ok(true),
        Some("retain") => {
            let reason = value
                .get("code")
                .and_then(Value::as_str)
                .unwrap_or("QUALIFICATION_FAILED");
            // An old quarantine wrapper without an index is retained as well.
            // It must not fall through to the generic cleanup after proof loss.
            if reason == "NOT_REGISTERED"
                && !path.join("理由.txt").exists()
                && !path.join("files").exists()
            {
                return Ok(false);
            }
            retain(stats, reason, None);
            return Ok(true);
        }
        Some("candidate") => (),
        _ => {
            retain(stats, "INVALID_PLAN", None);
            return Ok(true);
        }
    }
    let plan = match Plan::parse(path, &value) {
        Ok(p) => p,
        Err(_) => {
            retain(stats, "INVALID_PLAN", None);
            return Ok(true);
        }
    };
    stats.owned_candidates += 1;
    stats.owned_candidate_bytes = stats.owned_candidate_bytes.saturating_add(plan.bytes);
    let _pins = pin_ancestors(path)?;
    match plan.validate(&plan.payload, budget, stats, 0) {
        Ok(count) if count == plan.entries.len() => (),
        _ => {
            retain(stats, "PHYSICAL_CONTENTS_CHANGED", Some(plan.bytes));
            return Ok(true);
        }
    }
    if !apply {
        return Ok(true);
    }
    if plan.remove(&plan.payload, budget, stats, 0).is_err() {
        retain(stats, "DELETE_INCOMPLETE", Some(plan.bytes));
        stats.failures += 1;
        return Ok(true);
    }
    if lease.finish().is_err() {
        stats.owned_finish_failed += 1;
        stats.failures += 1;
        return Ok(true);
    }
    stats.owned_completed += 1;
    Ok(true)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture() -> PathBuf {
        let path = std::env::temp_dir().join(format!(
            "cleanup3757-{}-{}",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        fs::create_dir(&path).unwrap();
        path
    }
    fn plan(wrapper: &Path) -> Value {
        fn visit(root: &Path, path: &Path, out: &mut Vec<Value>, bytes: &mut u64) {
            let p = Pinned::open(path, false).unwrap();
            let i = &p.info;
            let relative = path
                .strip_prefix(root)
                .unwrap()
                .to_str()
                .unwrap()
                .replace('\\', "/");
            let size = u64::from(i.size_high) << 32 | u64::from(i.size_low);
            if !p.directory() {
                *bytes += size;
            }
            out.push(json!({"relative":if relative.is_empty(){"."}else{&relative},"volume":i.serial,"file_id":u64::from(i.index_high)<<32|u64::from(i.index_low),"birth":u64::from(i.created.high)<<32|u64::from(i.created.low),"written":u64::from(i.written.high)<<32|u64::from(i.written.low),"size":size,"attributes":i.attributes,"links":i.links}));
            if p.directory() {
                for x in fs::read_dir(path).unwrap() {
                    visit(root, &x.unwrap().path(), out, bytes);
                }
            }
        }
        let payload = wrapper.join("files");
        let mut entries = Vec::new();
        let mut bytes = 0;
        visit(&payload, &payload, &mut entries, &mut bytes);
        json!({"action":"candidate","id":"a".repeat(32),"path":payload,"bytes":bytes,"entries":entries})
    }
    fn budget() -> Budget {
        Budget {
            until: Instant::now() + Duration::from_secs(10),
            max_entries: 1000,
        }
    }

    #[test]
    fn owned_plan_deletes_only_matching_payload_and_keeps_quarantine_evidence() {
        let root = fixture();
        let payload = root.join("files");
        fs::create_dir(&payload).unwrap();
        fs::write(payload.join("cache.bin"), b"fixture").unwrap();
        fs::write(root.join("理由.txt"), b"keep evidence").unwrap();
        let value = plan(&root);
        let p = Plan::parse(&root, &value).unwrap();
        let mut stats = Stats::default();
        assert_eq!(
            p.validate(&payload, &budget(), &mut stats, 0).unwrap(),
            p.entries.len()
        );
        p.remove(&payload, &budget(), &mut stats, 0).unwrap();
        assert!(!payload.exists());
        assert!(root.join("理由.txt").exists());
        assert_eq!(stats.logical_bytes, 7);
        fs::remove_file(root.join("理由.txt")).unwrap();
        fs::remove_dir(root).unwrap();
    }
    #[test]
    fn owned_plan_refuses_changed_identity_metadata_added_file_and_source_names() {
        let root = fixture();
        let payload = root.join("files");
        fs::create_dir(&payload).unwrap();
        let file = payload.join("cache.bin");
        fs::write(&file, b"fixture").unwrap();
        let value = plan(&root);
        let p = Plan::parse(&root, &value).unwrap();
        fs::write(&file, b"changed size").unwrap();
        assert!(
            p.validate(&payload, &budget(), &mut Stats::default(), 0)
                .is_err()
        );
        let mut source = value.clone();
        source["entries"][1]["relative"] = json!("main.rs");
        assert!(Plan::parse(&root, &source).is_err());
        let value = plan(&root);
        let p = Plan::parse(&root, &value).unwrap();
        fs::write(payload.join("extra.bin"), b"extra").unwrap();
        assert!(
            p.validate(&payload, &budget(), &mut Stats::default(), 0)
                .is_err()
        );
        fs::remove_file(payload.join("extra.bin")).unwrap();
        fs::remove_file(file).unwrap();
        fs::remove_dir(payload).unwrap();
        fs::remove_dir(root).unwrap();
    }
    #[test]
    fn owned_plan_refuses_bad_boundaries_duplicate_entries_and_wrong_bytes() {
        let root = fixture();
        fs::create_dir(root.join("files")).unwrap();
        let value = plan(&root);
        for relative in [
            "../outside",
            "D:/outside",
            "child/../outside",
            "child\\outside",
            "child//outside",
            "src/file.bin",
        ] {
            let mut v = value.clone();
            v["entries"][0]["relative"] = json!(relative);
            assert!(Plan::parse(&root, &v).is_err(), "{relative}");
        }
        let mut v = value.clone();
        v["bytes"] = json!(99);
        assert!(Plan::parse(&root, &v).is_err());
        let mut v = value.clone();
        v["entries"]
            .as_array_mut()
            .unwrap()
            .push(value["entries"][0].clone());
        assert!(Plan::parse(&root, &v).is_err());
        fs::remove_dir(root.join("files")).unwrap();
        fs::remove_dir(root).unwrap();
    }
    #[test]
    fn owned_plan_keeps_busy_file_and_rejects_hardlinks() {
        let root = fixture();
        let payload = root.join("files");
        fs::create_dir(&payload).unwrap();
        let file = payload.join("cache.bin");
        fs::write(&file, b"fixture").unwrap();
        let value = plan(&root);
        let p = Plan::parse(&root, &value).unwrap();
        let busy = OpenOptions::new()
            .read(true)
            .share_mode(1)
            .open(&file)
            .unwrap();
        assert!(
            p.remove(&payload, &budget(), &mut Stats::default(), 0)
                .is_err()
        );
        assert!(file.exists());
        drop(busy);
        let outside = root.join("outside.bin");
        fs::hard_link(&file, &outside).unwrap();
        let value = plan(&root);
        assert!(Plan::parse(&root, &value).is_err());
        fs::remove_file(outside).unwrap();
        fs::remove_file(file).unwrap();
        fs::remove_dir(payload).unwrap();
        fs::remove_dir(root).unwrap();
    }
    #[test]
    fn guard_requires_clear_access_and_complete_response() {
        let clear = json!({"ok":true,"access_allowed":true,"client_resource":false,"classification_unverified":false,"protection_status":"clear"});
        assert!(guard_clear(&clear));
        for key in [
            "ok",
            "access_allowed",
            "client_resource",
            "classification_unverified",
            "protection_status",
        ] {
            let mut v = clear.clone();
            v.as_object_mut().unwrap().remove(key);
            assert!(!guard_clear(&v));
        }
        let mut denied = clear;
        denied["access_allowed"] = json!(false);
        assert!(!guard_clear(&denied));
    }

    #[test]
    fn owned_real_go_bridge_finishes_original_receipt_in_separate_process() {
        let root = fixture();
        let exe = root.join("safe-delete.test.exe");
        let output = Command::new("go")
            .arg("test")
            .arg("-c")
            .arg("-o")
            .arg(&exe)
            .current_dir(
                Path::new(env!("CARGO_MANIFEST_DIR"))
                    .parent()
                    .unwrap()
                    .join("safe-delete"),
            )
            .creation_flags(NO_WINDOW)
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        let data = root.join("synthetic");
        fs::create_dir(&data).unwrap();
        let capture = root.join("producer.jsonl");
        let file = OpenOptions::new()
            .create_new(true)
            .write(true)
            .open(&capture)
            .unwrap();
        let child = Command::new(&exe)
            .arg("-test.run=^TestRetirementLeaseProcess$")
            .env("CC_RETIREMENT_FIXTURE_ROOT", &data)
            .creation_flags(NO_WINDOW)
            .stdin(Stdio::piped())
            .stdout(Stdio::from(file))
            .stderr(Stdio::null())
            .spawn()
            .unwrap();
        let budget = Budget {
            until: Instant::now() + Duration::from_secs(45),
            max_entries: 1000,
        };
        let (mut lease, value) = Lease::receive(child, OwnedCapture(capture), &budget).unwrap();
        let payload = PathBuf::from(value["path"].as_str().unwrap());
        let wrapper = payload.parent().unwrap();
        assert!(wrapper.starts_with(&data));
        let plan = Plan::parse(wrapper, &value).unwrap();
        let mut stats = Stats::default();
        assert_eq!(
            plan.validate(&payload, &budget, &mut stats, 0).unwrap(),
            plan.entries.len()
        );
        plan.remove(&payload, &budget, &mut stats, 0).unwrap();
        lease.finish().unwrap();
        assert!(!payload.exists());
        assert!(wrapper.join("理由.txt").exists());
        let id = value["id"].as_str().unwrap();
        let receipt: Value = serde_json::from_slice(
            &fs::read(data.join("receipts").join(format!("{id}.json"))).unwrap(),
        )
        .unwrap();
        assert_eq!(receipt["state"], "quarantine_removed");
        assert_eq!(stats.logical_bytes, value["bytes"].as_u64().unwrap());
        drop(lease);
        fs::remove_dir_all(root).unwrap();
    }
}
