//! Metadata-only recovery of the one authorized PNPM tree. No delete or move API.
use super::*;

pub(super) const ID: &str = "80b350ade8bb05cd61ab94f7485d09cc";
#[cfg(not(test))]
const INTEGRITY: &str = "ec6d702beea2c00011eed142fa8f9e14f1e684d45b09eaf1dea9629c26ab2413";
// Includes the root; this is the measured tree, not a general larger limit.
const ENTRY_LIMIT: usize = 17799;
const LINK_LIMIT: usize = 105;
const BYTE_LIMIT: u64 = 411015031;
const RECORD_LIMIT: u64 = 8 * 1024 * 1024;

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct Link {
    target: String,
    reparse: Vec<u8>,
}
#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct Entry {
    node: Node,
    link: Option<Link>,
}
type Inventory = BTreeMap<String, Entry>;
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct MoveTicket {
    id: String,
    source: String,
    destination: String,
    integrity: String,
    nodes: Inventory,
}

#[link(name = "kernel32")]
unsafe extern "system" {
    fn DeviceIoControl(
        handle: isize,
        code: u32,
        input: *const u8,
        input_size: u32,
        output: *mut u8,
        output_size: u32,
        returned: *mut u32,
        overlapped: *mut u8,
    ) -> i32;
}

fn read_relative_link(path: &Path) -> Result<Link> {
    let name = extended_path(path)?;
    let mut buf = vec![0u8; 16384];
    let mut returned = 0u32;
    // OPEN_REPARSE_POINT, metadata only. FSCTL_GET_REPARSE_POINT reads the link
    // itself. No path canonicalization or opening of the target occurs.
    let ok = unsafe {
        let handle = CreateFileW(name.as_ptr(), 0, 7, std::ptr::null(), 3, 0x02200000, 0);
        if handle == -1 {
            return Err("LINK_METADATA_FAILED");
        }
        let ok = DeviceIoControl(
            handle,
            0x900a8,
            std::ptr::null(),
            0,
            buf.as_mut_ptr(),
            buf.len() as u32,
            &mut returned,
            std::ptr::null_mut(),
        );
        CloseHandle(handle);
        ok
    };
    if ok == 0 || returned < 20 || returned as usize > buf.len() {
        return Err("LINK_METADATA_FAILED");
    }
    buf.truncate(returned as usize);
    let u16_at = |offset| u16::from_le_bytes([buf[offset], buf[offset + 1]]) as usize;
    let tag = u32::from_le_bytes(buf[0..4].try_into().unwrap());
    let flags = u32::from_le_bytes(buf[16..20].try_into().unwrap());
    if tag != 0xa000000c || flags != 1 || u16_at(4) + 8 != buf.len() {
        return Err("NOT_RELATIVE_SYMLINK");
    }
    let offset = u16_at(8);
    let length = u16_at(10);
    if offset % 2 != 0 || length % 2 != 0 || length == 0 || 20 + offset + length > buf.len() {
        return Err("INVALID_LINK_DATA");
    }
    let wide: Vec<u16> = buf[20 + offset..20 + offset + length]
        .chunks_exact(2)
        .map(|b| u16::from_le_bytes([b[0], b[1]]))
        .collect();
    let target = String::from_utf16(&wide).map_err(|_| "INVALID_LINK_DATA")?;
    if target.contains(['\0', ':', '/']) || Path::new(&target).has_root() {
        return Err("EXTERNAL_LINK");
    }
    Ok(Link {
        target,
        reparse: buf,
    })
}

// Resolve solely against the enumerated names. Reject targets with symlink
// ancestors rather than relying on lexical normalization through another link.
fn target_key(key: &str, target: &str) -> Result<String> {
    if target.contains(['\0', ':']) || Path::new(target).has_root() {
        return Err("EXTERNAL_LINK");
    }
    let mut parts: Vec<&str> = key.split('/').collect();
    parts.pop();
    let mut normal = false;
    for part in target.split(['/', '\\']) {
        match part {
            "" | "." => (),
            ".." => {
                // PNPM uses a leading parent sequence followed by normal names.
                // Do not normalize away an intermediate link or directory.
                if normal {
                    return Err("LINK_NON_PNPM_TARGET");
                }
                parts.pop().ok_or("EXTERNAL_LINK")?;
            }
            name => {
                normal = true;
                if name.ends_with(['.', ' ']) || name.contains(['*', '?']) {
                    return Err("INVALID_LINK_DATA");
                }
                parts.push(name);
            }
        }
    }
    Ok(parts.join("/"))
}

fn scan_links(root: &Path) -> Result<Inventory> {
    checked(root)?;
    let mut nodes = Inventory::new();
    let mut pending = vec![(root.to_owned(), String::new())];
    let mut hardlinks: BTreeMap<(u32, u64), (u32, u32)> = BTreeMap::new();
    let mut bytes = 0u64;
    let mut link_count = 0usize;
    while let Some((path, key)) = pending.pop() {
        let name = path
            .file_name()
            .ok_or("INVALID_PATH")?
            .to_str()
            .ok_or("INVALID_PATH")?;
        if protected(name) || name.eq_ignore_ascii_case(".git") {
            return Err("PROTECTED_PATH");
        }
        if nodes.len() >= ENTRY_LIMIT {
            return Err("ENTRY_LIMIT_EXCEEDED");
        }
        let n = node_no_follow(&path)?;
        let link = if n.attributes & REPARSE != 0 {
            if !key.starts_with("frontend-runtime/node_modules/") {
                return Err("LINK_OUTSIDE_PNPM");
            }
            link_count += 1;
            if link_count > LINK_LIMIT {
                return Err("LINK_LIMIT_EXCEEDED");
            }
            let link = read_relative_link(&path)?;
            if node_no_follow(&path)? != n {
                return Err("LINK_CHANGED_DURING_SCAN");
            }
            Some(link)
        } else if n.attributes & 0x10 != 0 {
            for child in fs::read_dir(&path).map_err(|_| "CONTENT_METADATA_FAILED")? {
                let child = child.map_err(|_| "CONTENT_METADATA_FAILED")?;
                let name = child.file_name();
                let name = name.to_str().ok_or("INVALID_PATH")?;
                if nodes.len() + pending.len() >= ENTRY_LIMIT {
                    return Err("ENTRY_LIMIT_EXCEEDED");
                }
                let child_key = if key.is_empty() {
                    name.into()
                } else {
                    format!("{key}/{name}")
                };
                pending.push((child.path(), child_key));
            }
            None
        } else {
            bytes = bytes.checked_add(n.size).ok_or("SIZE_LIMIT_EXCEEDED")?;
            if bytes > BYTE_LIMIT {
                return Err("SIZE_LIMIT_EXCEEDED");
            }
            let count = hardlinks
                .entry((n.volume, n.file_id))
                .or_insert((0, n.links));
            if count.1 != n.links {
                return Err("HARD_LINK");
            }
            count.0 += 1;
            None
        };
        nodes.insert(key, Entry { node: n, link });
    }
    if hardlinks.values().any(|(inside, total)| inside != total) {
        return Err("HARD_LINK");
    }
    for (key, entry) in &nodes {
        if entry.node.volume != nodes[""].node.volume {
            return Err("EXTERNAL_VOLUME");
        }
        if let Some(link) = &entry.link {
            let target = target_key(key, &link.target)?;
            if target.is_empty() || key.starts_with(&format!("{target}/")) {
                return Err("LINK_TARGET_ANCESTOR");
            }
            let mut prefix = String::new();
            let parts: Vec<&str> = target.split('/').collect();
            for (i, part) in parts.iter().enumerate() {
                if i != 0 {
                    prefix.push('/');
                }
                prefix.push_str(part);
                let found = nodes.get(&prefix).ok_or("LINK_TARGET_MISSING")?;
                if found.link.is_some() {
                    return Err("LINK_TARGET_IS_LINK");
                }
                if i + 1 != parts.len() && found.node.attributes & 0x10 == 0 {
                    return Err("LINK_TARGET_NOT_DIRECTORY");
                }
            }
            if nodes[&target].node.attributes & 0x10 != entry.node.attributes & 0x10 {
                return Err("LINK_TARGET_TYPE_MISMATCH");
            }
        }
    }
    Ok(nodes)
}

fn load(c: &Config, id: &str) -> Result<Receipt> {
    let r = c.load(id)?;
    if c.id != ID || !c.repos.is_empty() {
        return Err("SCOPE_NOT_AUTHORIZED");
    }
    if r.state == "owned" && r.destination.is_some() {
        return Err("RECEIPT_DESTINATION_MISMATCH");
    }
    // Fixture construction is compile-time test-only; production has no override.
    #[cfg(not(test))]
    if r.integrity != INTEGRITY || r.identity.created_at != "2026-10-09T08:25:27.1778086+09:00" {
        return Err("RECEIPT_SCOPE_MISMATCH");
    }
    Ok(r)
}

fn validate(c: &Config, r: &Receipt, root: &Path) -> Result<Inventory> {
    checked(root)?;
    c.client_guard(root)?;
    let n = node(root)?;
    if n.attributes & 0x10 == 0 || n.volume != r.identity.volume || n.file_id != r.identity.file_id
    {
        return Err("TARGET_IDENTITY_CHANGED");
    }
    let first = scan_links(root)?;
    if scan_links(root)? != first {
        return Err("CONTENT_CHANGED_DURING_SCAN");
    }
    Ok(first)
}

fn totals(nodes: &Inventory) -> (u64, usize) {
    (
        nodes
            .values()
            .filter(|e| e.link.is_none() && e.node.attributes & 0x10 == 0)
            .map(|e| e.node.size)
            .sum(),
        nodes.values().filter(|e| e.link.is_some()).count(),
    )
}
fn proof_path(c: &Config) -> PathBuf {
    c.ticket_path().with_extension("scoopa-proof.json")
}

pub(super) fn plan(c: &Config, id: &str) -> Result<serde_json::Value> {
    let r = load(c, id)?;
    if r.state != "owned" || r.destination.is_some() {
        return Err("RECEIPT_NOT_OWNED");
    }
    let source = Path::new(&r.identity.path);
    let nodes = validate(c, &r, source)?;
    let dest = c.destination();
    checked(&dest)?;
    c.client_guard(&dest)?;
    if source.components().next() != dest.components().next() {
        return Err("QUARANTINE_CROSS_DRIVE");
    }
    if present(&dest)? && !present(&c.ticket_path())? {
        return Err("QUARANTINE_ALREADY_PREPARED");
    }
    let (bytes, links) = totals(&nodes);
    let ticket = MoveTicket {
        id: ID.into(),
        source: r.identity.path.clone(),
        destination: dest.to_string_lossy().into_owned(),
        integrity: r.integrity.clone(),
        nodes,
    };
    let serialized = serde_json::to_vec_pretty(&ticket).map_err(|_| "TICKET_JSON")?;
    if serialized.len() as u64 > RECORD_LIMIT {
        return Err("INVALID_RECOVERY_RECORD");
    }
    // Publish intent before destination creation; resume compares, never replaces it.
    publish_proof(&c.ticket_path(), &serialized)?;
    let digest = format!("{:x}", Sha256::digest(&serialized));
    publish_proof(&proof_path(c), digest.as_bytes())?;
    fs::create_dir_all(&c.quarantine).map_err(|_| "QUARANTINE_PARENT_CREATE")?;
    checked(&dest)?;
    if !present(&dest)? {
        fs::create_dir(&dest).map_err(|_| "QUARANTINE_DESTINATION_CREATE")?;
    }
    if node(&dest)?.attributes & 0x10 == 0 {
        return Err("QUARANTINE_NOT_DIRECTORY");
    }
    for entry in fs::read_dir(&dest).map_err(|_| "QUARANTINE_METADATA_FAILED")? {
        if entry.map_err(|_| "QUARANTINE_METADATA_FAILED")?.file_name() != OsStr::new("理由.txt")
        {
            return Err("QUARANTINE_DESTINATION_NOT_EMPTY");
        }
    }
    let why = format!(
        "退避状態: 未完了（移動前）。\n元の場所: {}\n大きさ: {bytes} byte\n項目数: {}（根を含む）、相対リンク: {links}件\n理由: 通常の退避が依存リンクと項目数の上限で停止した。所有IDを固定した復旧手順でリンク自身だけを検査し、全データを保持して同じドライブへ退避する。\n所有ID: {ID}\n",
        r.identity.path,
        ticket.nodes.len()
    );
    write_or_verify(&dest.join("理由.txt"), why.as_bytes(), 8192)?;
    // A fresh scan immediately before handing the ordinary move to the caller.
    if validate(c, &r, source)? != ticket.nodes {
        return Err("CONTENT_CHANGED_AFTER_PLAN");
    }
    Ok(
        serde_json::json!({"id":ID,"action":"submit_quarantine_to_exec_tool","bytes":bytes,
        "entries":ticket.nodes.len(),"links":links,"destination":dest,"inventory_sha256":digest,
        "powershell_command":format!("Move-Item -LiteralPath {} -Destination {} -ErrorAction Stop", quote(source), quote(&dest.join("files")))}),
    )
}

pub(super) fn finish(c: &Config, id: &str) -> Result<serde_json::Value> {
    let mut r = load(c, id)?;
    let serialized = read_record(&c.ticket_path(), RECORD_LIMIT)?;
    let digest = format!("{:x}", Sha256::digest(&serialized));
    if read_record(&proof_path(c), 128)? != digest.as_bytes() {
        return Err("RECOVERY_RECORD_CHANGED");
    }
    let ticket: MoveTicket = serde_json::from_slice(&serialized).map_err(|_| "INVALID_TICKET")?;
    let dest = c.destination();
    if ticket.id != ID
        || ticket.source != r.identity.path
        || ticket.integrity != r.integrity
        || !same(Path::new(&ticket.destination), &dest)
    {
        return Err("TICKET_SCOPE_MISMATCH");
    }
    if r.state == "quarantined" && r.destination.as_deref() != Some(ticket.destination.as_str()) {
        return Err("RECEIPT_DESTINATION_MISMATCH");
    }
    if present(Path::new(&r.identity.path))? {
        return Err("TARGET_STILL_EXISTS");
    }
    let nodes = validate(c, &r, &dest.join("files"))?;
    if nodes != ticket.nodes {
        return Err("CONTENT_CHANGED_AFTER_PLAN");
    }
    let reason_path = dest.join("理由.txt");
    let reason = read_record(&reason_path, 8192)?;
    let completed = "退避状態: 完了。元の場所の不在、同一の根の識別番号、通常ファイルと相対リンク自身の全メタデータ・ターゲットの一致を確認した。\n";
    if !reason
        .windows(completed.len())
        .any(|s| s == completed.as_bytes())
    {
        let mut f = OpenOptions::new()
            .append(true)
            .open(reason_path)
            .map_err(|_| "REASON_UPDATE_FAILED")?;
        f.write_all(completed.as_bytes())
            .and_then(|()| f.sync_all())
            .map_err(|_| "REASON_UPDATE_FAILED")?;
    }
    if r.state == "owned" {
        r.state = "quarantined".into();
        r.destination = Some(ticket.destination.clone());
        let writing = c.receipt_path().with_extension("recovery-writing");
        write_or_verify(
            &writing,
            &serde_json::to_vec(&r).map_err(|_| "RECEIPT_JSON")?,
            8192,
        )?;
        fs::rename(writing, c.receipt_path()).map_err(|_| "RECEIPT_COMMIT_FAILED")?;
    }
    let (bytes, links) = totals(&nodes);
    Ok(
        serde_json::json!({"id":ID,"state":"quarantined","source_absent":true,"destination":dest,
        "bytes":bytes,"entries":nodes.len(),"links":links,"inventory_verified":true,
        "inventory_sha256":digest,"root_volume":r.identity.volume,"root_file_id":r.identity.file_id}),
    )
}
