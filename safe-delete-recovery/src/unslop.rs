//! Preserve exactly the four retired ToDo1940 trees. No delete or move API.
use super::*;

pub(super) const BUILD: &str = "ef300280dcb8c0e08fcb9be1318565ef";
pub(super) const CORE: &str = "efb997169a0948371bd7d6d9507e7068";
pub(super) const MEASUREMENT: &str = "69e45803950f1fdfb4900ad2c10e2dfd";
pub(super) const INTEGRATION: &str = "8ad3cd1e922596eeec19fac566fddeb7";
const PNPM: &str = "worktree/godd-mcp-alpha/node_modules/";
const REGISTRY_LINKS: [&str; 2] = [
    "measurement/measurement-63628-1791287330489098600/cargo-home/registry",
    "measurement/measurement-80712-1791286803451627800/cargo-home/registry",
];
const RECORD_LIMIT: u64 = 8 * 1024 * 1024;
// The preserved real MCP ticket is 9,255,686 bytes. Its filesystem limits
// stay unchanged; only this pinned tree needs a larger metadata envelope.
pub(super) fn record_limit(c: &Config) -> u64 {
    if c.id == INTEGRATION {
        16 * 1024 * 1024
    } else {
        RECORD_LIMIT
    }
}

#[derive(Clone, Copy)]
pub(super) struct Profile {
    id: &'static str,
    content: &'static str,
    volume: u32,
    file_id: u64,
    entries: usize,
    links: usize,
    bytes: u64,
}

pub(super) fn profile(id: &str) -> Option<Profile> {
    let (id, content, volume, file_id, entries, links, bytes) = match id {
        BUILD => (
            BUILD,
            "rust-build",
            1215965535,
            8162774328342697,
            381,
            0,
            123806370,
        ),
        CORE => (
            CORE,
            "independent-rust-build",
            2229119083,
            3377699730734494,
            1511,
            0,
            898105303,
        ),
        MEASUREMENT => (
            MEASUREMENT,
            "held-input-measurement",
            2229119083,
            25895697868367764,
            4143,
            2,
            1852707508,
        ),
        INTEGRATION => (
            INTEGRATION,
            "todo1931-unslop-integration",
            2229119083,
            2533274803590816,
            14390,
            346,
            1780913205,
        ),
        _ => return None,
    };
    Some(Profile {
        id,
        content,
        volume,
        file_id,
        entries,
        links,
        bytes,
    })
}

pub(super) fn configure(c: &mut Config, id: &str) -> Result<()> {
    let p = profile(id).ok_or("SCOPE_NOT_AUTHORIZED")?;
    c.id = p.id;
    c.project = if id == INTEGRATION {
        "GoDD-MCP-Alpha"
    } else {
        "GoDD-Unslop"
    };
    c.content = p.content;
    c.store = PathBuf::from("D:/CC/Tools/safe-delete/receipts-go");
    c.temp = PathBuf::from(if id == BUILD {
        "D:/CC/Apps/GoDD-Unslop"
    } else {
        "C:/Users/makhmeto/AppData/Local/Temp"
    });
    if id == BUILD {
        c.quarantine = PathBuf::from("D:/削除希望");
    }
    c.pinned_identity = Some((p.volume, p.file_id));
    c.repos.clear();
    if id == INTEGRATION {
        c.repos.push(Repo {
            name: "worktree",
            origin: "git@github.com:AutoDevJapan/GoDD-MCP-Alpha.git",
            head: "0621aac55a3035ab473cb2f7b49887a5a0925d6c".into(),
            saved_ref: Some("refs/remotes/origin/codex/todo1931-unslop-20261006"),
            saved_merge: None,
            allowed_untracked: &[],
            kind: RepoKind::Linked {
                admin: PathBuf::from(
                    "C:/Users/makhmeto/Desktop/dev/ADJ/GoDD-MCP-Alpha/.git/worktrees/worktree",
                ),
                common: PathBuf::from("C:/Users/makhmeto/Desktop/dev/ADJ/GoDD-MCP-Alpha/.git"),
            },
        });
    }
    Ok(())
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct Entry {
    node: Node,
    security_sha256: String,
    reparse: Option<Vec<u8>>,
    aliases_inside: u32,
}
type Inventory = BTreeMap<String, Entry>;

fn shared_package(c: &Config, key: &str, n: &Node) -> bool {
    c.id == INTEGRATION
        && n.attributes & (REPARSE | 0x10) == 0
        && key.starts_with(&format!("{PNPM}.pnpm/"))
}
fn same_node(c: &Config, key: &str, old: &Node, now: &Node) -> bool {
    let mut comparable = old.clone();
    // Other tasks may add/remove names in the external PNPM store. Those
    // names are not owned or moved here. Every other metadata field is exact.
    if shared_package(c, key, old) && shared_package(c, key, now) {
        comparable.links = now.links;
    }
    comparable == *now
}
fn same_nodes(c: &Config, old: &Inventory, now: &Inventory) -> bool {
    old.len() == now.len()
        && old.iter().all(|(key, expected)| {
            now.get(key).is_some_and(|actual| {
                same_node(c, key, &expected.node, &actual.node)
                    && expected.security_sha256 == actual.security_sha256
                    && expected.reparse == actual.reparse
                    && expected.aliases_inside == actual.aliases_inside
            })
        })
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct MoveTicket {
    id: String,
    source: String,
    destination: String,
    integrity: String,
    nodes: Inventory,
    backlink: Option<BacklinkTicket>,
    backlink_security_sha256: Option<String>,
}

// Decode only the substitute-name field. Preserve the entire original reparse
// buffer in the ticket, including flags and print-name bytes. Never open targets.
fn absolute_target(raw: &[u8], expected_tag: u32) -> Result<String> {
    if raw.len() < 20 {
        return Err("INVALID_LINK_DATA");
    }
    let u16_at = |n| u16::from_le_bytes([raw[n], raw[n + 1]]) as usize;
    if u32::from_le_bytes(raw[..4].try_into().unwrap()) != expected_tag
        || u16_at(4) + 8 != raw.len()
        || (expected_tag == 0xa000000c && raw[16..20] != [0, 0, 0, 0])
    {
        return Err("UNEXPECTED_REPARSE_TYPE");
    }
    let base = if expected_tag == 0xa0000003 { 16 } else { 20 };
    let (offset, length) = (u16_at(8), u16_at(10));
    if offset % 2 != 0 || length % 2 != 0 || length == 0 || base + offset + length > raw.len() {
        return Err("INVALID_LINK_DATA");
    }
    let wide: Vec<_> = raw[base + offset..base + offset + length]
        .as_chunks::<2>()
        .0
        .iter()
        .map(|b| u16::from_le_bytes(*b))
        .collect();
    let text = String::from_utf16(&wide).map_err(|_| "INVALID_LINK_DATA")?;
    let target = text.strip_prefix(r"\??\").ok_or("INVALID_LINK_DATA")?;
    if target.contains('\0') {
        return Err("INVALID_LINK_DATA");
    }
    Ok(target.to_owned())
}

fn registry_target(c: &Config) -> PathBuf {
    #[cfg(test)]
    {
        c.temp.join("registry-target")
    }
    #[cfg(not(test))]
    {
        let _ = c;
        PathBuf::from(r"C:\Users\makhmeto\.cargo\registry")
    }
}

fn validate_link(c: &Config, source: &Path, key: &str, raw: &[u8]) -> Result<Option<String>> {
    if c.id == MEASUREMENT && REGISTRY_LINKS.contains(&key) {
        let target = absolute_target(raw, 0xa000000c)?;
        if !same(Path::new(&target), &registry_target(c)) {
            return Err("REGISTRY_LINK_CHANGED");
        }
        return Ok(None);
    }
    if c.id != INTEGRATION || !key.starts_with(PNPM) {
        return Err("UNDECLARED_REPARSE_POINT");
    }
    let target = absolute_target(raw, 0xa0000003)?;
    let suffix = target
        .strip_prefix(&format!(
            "{}\\",
            source.to_string_lossy().replace('/', "\\")
        ))
        .ok_or("EXTERNAL_LINK")?
        .replace('\\', "/");
    if !suffix.starts_with(PNPM)
        || suffix.split('/').any(|s| {
            s.is_empty()
                || s == "."
                || s == ".."
                || s.ends_with(['.', ' '])
                || s.contains([':', '*', '?'])
        })
    {
        return Err("EXTERNAL_LINK");
    }
    Ok(Some(suffix))
}

fn inventory(c: &Config, r: &Receipt, root: &Path) -> Result<Inventory> {
    let p = profile(c.id).ok_or("SCOPE_NOT_AUTHORIZED")?;
    checked(root)?;
    c.client_guard(root)?;
    let n = node(root)?;
    if n.attributes & 0x10 == 0 || (n.volume, n.file_id) != (r.identity.volume, r.identity.file_id)
    {
        return Err("TARGET_IDENTITY_CHANGED");
    }
    let mut pending = vec![root.to_owned()];
    let mut entries = Inventory::new();
    let mut targets = BTreeMap::new();
    let mut aliases: BTreeMap<(u32, u64), (u32, u32, bool)> = BTreeMap::new();
    let mut bytes = 0u64;
    let mut links = 0usize;
    while let Some(path) = pending.pop() {
        let key = path
            .strip_prefix(root)
            .map_err(|_| "PATH_OUTSIDE_ROOT")?
            .to_string_lossy()
            .replace('\\', "/");
        let name = path.file_name().ok_or("INVALID_PATH")?.to_string_lossy();
        if protected(&name) {
            return Err("PROTECTED_PATH");
        }
        if name.eq_ignore_ascii_case(".git")
            && !(c.id == INTEGRATION
                && matches!(key.as_str(), "worktree/.git" | "worktree/officialsite/.git"))
        {
            return Err("UNDECLARED_GIT_METADATA");
        }
        if entries.len() >= p.entries {
            return Err("ENTRY_LIMIT_EXCEEDED");
        }
        let n = node_no_follow(&path)?;
        let security = scoopa::security_hash(&path, &n)?;
        let reparse = if n.attributes & REPARSE != 0 {
            if n.links != 1 || n.attributes & 0x10 == 0 {
                return Err("UNEXPECTED_REPARSE_TYPE");
            }
            let raw = scoopa::raw_reparse(&path)?;
            if let Some(target) = validate_link(c, Path::new(&r.identity.path), &key, &raw)? {
                targets.insert(key.clone(), target);
            }
            links += 1;
            if links > p.links {
                return Err("LINK_LIMIT_EXCEEDED");
            }
            Some(raw)
        } else {
            if n.attributes & 0x10 != 0 {
                for child in fs::read_dir(&path).map_err(|_| "CONTENT_METADATA_FAILED")? {
                    if pending.len() + entries.len() >= p.entries {
                        return Err("ENTRY_LIMIT_EXCEEDED");
                    }
                    pending.push(child.map_err(|_| "CONTENT_METADATA_FAILED")?.path());
                }
            } else {
                bytes = bytes.checked_add(n.size).ok_or("BYTE_LIMIT_EXCEEDED")?;
                if bytes > p.bytes {
                    return Err("BYTE_LIMIT_EXCEEDED");
                }
                let a = aliases
                    .entry((n.volume, n.file_id))
                    .or_insert((0, n.links, true));
                if a.1 != n.links && !(a.2 && shared_package(c, &key, &n)) {
                    return Err("HARD_LINK_CHANGED");
                }
                a.1 = a.1.min(n.links);
                a.0 += 1;
                // Only PNPM store files may retain pre-existing external aliases.
                a.2 &= c.id == INTEGRATION && key.starts_with(&format!("{PNPM}.pnpm/"));
            }
            None
        };
        if !same_node(c, &key, &n, &node_no_follow(&path)?) {
            return Err("INVENTORY_CHANGED");
        }
        entries.insert(
            key,
            Entry {
                node: n,
                security_sha256: security,
                reparse,
                aliases_inside: 0,
            },
        );
    }
    if aliases
        .values()
        .any(|(inside, total, pnpm)| inside > total || (inside != total && !pnpm))
    {
        return Err("EXTERNAL_HARD_LINK");
    }
    for target in targets.values() {
        if !entries
            .get(target)
            .is_some_and(|e| e.node.attributes & 0x10 != 0 && e.reparse.is_none())
        {
            return Err("JUNCTION_TARGET_NOT_PRESENT");
        }
        for ancestor in Path::new(target).ancestors().skip(1) {
            let key = ancestor.to_string_lossy().replace('\\', "/");
            if entries.get(&key).is_none_or(|e| e.reparse.is_some()) {
                return Err("JUNCTION_TARGET_IS_LINK");
            }
        }
    }
    if c.id == MEASUREMENT
        && !REGISTRY_LINKS
            .iter()
            .all(|k| entries.get(*k).is_some_and(|e| e.reparse.is_some()))
    {
        return Err("REGISTRY_LINK_MISSING");
    }
    for e in entries
        .values_mut()
        .filter(|e| e.node.attributes & (REPARSE | 0x10) == 0)
    {
        e.aliases_inside = aliases[&(e.node.volume, e.node.file_id)].0;
    }
    for repo in &c.repos {
        c.client_guard(&root.join(repo.name))?;
        if let RepoKind::Linked { admin, common } = &repo.kind {
            c.client_guard(admin)?;
            c.client_guard(common)?;
        }
        let before = same(root, Path::new(&r.identity.path));
        validate_repo_options(&root.join(repo.name), repo, before, true)?;
        validate_submodule(c, root, before)?;
    }
    Ok(entries)
}

fn snapshot(c: &Config, r: &Receipt, root: &Path) -> Result<Inventory> {
    let first = inventory(c, r, root)?;
    if !same_nodes(c, &first, &inventory(c, r, root)?) {
        return Err("INVENTORY_CHANGED");
    }
    Ok(first)
}
fn lexical_pointer(base: &Path, text: &str) -> Result<PathBuf> {
    let joined = base.join(text);
    let mut result = PathBuf::new();
    for component in joined.components() {
        match component {
            std::path::Component::ParentDir => {
                if !result.pop() {
                    return Err("INVALID_GIT_POINTER");
                }
            }
            std::path::Component::CurDir => (),
            other => result.push(other.as_os_str()),
        }
    }
    Ok(result)
}

fn validate_submodule(c: &Config, root: &Path, before: bool) -> Result<()> {
    let site = root.join("worktree/officialsite");
    #[cfg(test)]
    if !present(&site.join(".git"))? {
        return Ok(());
    }
    let RepoKind::Linked { admin, .. } = &c.repos[0].kind else {
        return Err("EXTERNAL_GITDIR");
    };
    let module = admin.join("modules/officialsite");
    c.client_guard(&module)?;
    checked_admin(&module)?;
    let pointer = bounded_text(&site.join(".git"))?;
    let raw = pointer
        .strip_prefix("gitdir: ")
        .ok_or("INVALID_GIT_POINTER")?;
    // Archive the relative pointer verbatim. Validate its old target before Git.
    if before && !same(&lexical_pointer(&site, raw)?, &module) {
        return Err("EXTERNAL_GITDIR");
    }
    for special in [
        "objects/info/alternates",
        "objects/info/http-alternates",
        "modules",
        "commondir",
        "worktrees",
    ] {
        if present(&module.join(special))? {
            return Err("SUBMODULE_EXTERNAL_METADATA");
        }
    }
    for entry in fs::read_dir(admin.join("modules")).map_err(|_| "SUBMODULE_METADATA_FAILED")? {
        if entry.map_err(|_| "SUBMODULE_METADATA_FAILED")?.file_name() != OsStr::new("officialsite")
        {
            return Err("UNDECLARED_SUBMODULE");
        }
    }
    if !before {
        return Ok(());
    }
    c.client_guard(&site)?;
    if !same(
        Path::new(&git(&site, &["rev-parse", "--git-dir"])?),
        &module,
    ) || !same(
        Path::new(&git(&site, &["rev-parse", "--show-toplevel"])?),
        &site,
    ) {
        return Err("EXTERNAL_GITDIR");
    }
    let head = git(&root.join("worktree"), &["rev-parse", "HEAD:officialsite"])?;
    #[cfg(not(test))]
    if head != "3ba5f7e44bb2e8fadc03f649775e5350ff4f3511" {
        return Err("SUBMODULE_HEAD_CHANGED");
    }
    if git(&site, &["rev-parse", "HEAD"])? != head
        || !git(
            &site,
            &["status", "--porcelain=v1", "--untracked-files=all"],
        )?
        .is_empty()
    {
        return Err("SUBMODULE_UNSAVED_CHANGES");
    }
    #[cfg(not(test))]
    if git(&site, &["remote", "get-url", "origin"])?
        != "git@github.com:AutoDevJapan/OfficialSite.git"
    {
        return Err("GIT_ORIGIN_CHANGED");
    }
    Ok(())
}

fn backlink_security(
    c: &Config,
    expected: Option<&BacklinkTicket>,
    preparing: bool,
) -> Result<Option<String>> {
    let Some(saved) = expected else {
        return Ok(None);
    };
    let RepoKind::Linked { admin, .. } = &c.repos[0].kind else {
        return Err("EXTERNAL_GITDIR");
    };
    let old = admin.join("gitdir");
    let current = node(&old)?;
    let old_hash = scoopa::security_hash(&old, &current)?;
    // Until the replacement has been committed, verify both ACLs at every
    // boundary. A repeated finish sees the already committed replacement and
    // validates it directly without requiring the retired staging name.
    if preparing || current == saved.original {
        let replacement = admin.join("gitdir.todo2052-repair");
        if scoopa::security_hash(&replacement, &saved.replacement)? != old_hash {
            return Err("BACKLINK_SECURITY_MISMATCH");
        }
    }
    Ok(Some(old_hash))
}

fn ticket_path(c: &Config) -> PathBuf {
    c.ticket_path().with_extension("unslop.json")
}
fn proof_path(c: &Config) -> PathBuf {
    ticket_path(c).with_extension("sha256")
}
fn total(nodes: &Inventory) -> u64 {
    nodes
        .values()
        .filter(|e| e.node.attributes & (0x10 | REPARSE) == 0)
        .map(|e| e.node.size)
        .sum()
}
fn why(c: &Config, r: &Receipt, nodes: &Inventory) -> String {
    format!(
        "退避状態: 未完了（移動前）。\n元の場所: {}\n大きさ: {} byte\n項目数: {}（根を含む）\n理由: 通常の所有確認がハードリンク・依存リンクで停止した。ToDo1940の4件だけを固定し、全内容・リンク自身・ACL・Git管理情報を保持して同じドライブへ退避する。\nPNPMの絶対参照と公式サイトの相対Git参照は元の文字列を保つため、退避後はそのまま再利用できない。参照先とサイトの実体も全てfiles内に保持する。Cargo registryの参照先と外部の共有ファイルには触れない。\n所有ID: {}\n",
        r.identity.path,
        total(nodes),
        nodes.len(),
        c.id
    )
}
pub(super) fn inspect(c: &Config, id: &str) -> Result<serde_json::Value> {
    let r = c.load(id)?;
    let nodes = snapshot(c, &r, Path::new(&r.identity.path))?;
    Ok(
        serde_json::json!({"id":id,"entries":nodes.len(),"bytes":total(&nodes),
        "links":nodes.values().filter(|e|e.reparse.is_some()).count(),"read_only":true}),
    )
}
pub(super) fn plan(c: &Config, id: &str) -> Result<serde_json::Value> {
    let r = c.load(id)?;
    if r.state != "owned" {
        return Err("RECEIPT_NOT_OWNED");
    }
    let path = Path::new(&r.identity.path);
    let dest = c.destination();
    checked(&dest)?;
    c.client_guard(&dest)?;
    if path.components().next() != dest.components().next() {
        return Err("QUARANTINE_CROSS_DRIVE");
    }
    if c.id == INTEGRATION
        && dest
            .join("files/worktree")
            .as_os_str()
            .encode_wide()
            .count()
            > 240
    {
        return Err("GIT_DESTINATION_ROOT_TOO_LONG");
    }
    let nodes = snapshot(c, &r, path)?;
    let prepared = present(&ticket_path(c))?;
    if present(&dest)? && !prepared {
        return Err("QUARANTINE_ALREADY_PREPARED");
    }
    let backlink = c.prepare_backlink(path, &dest.join("files"))?;
    let backlink_security_sha256 = backlink_security(c, backlink.as_ref(), true)?;
    let ticket = MoveTicket {
        id: id.into(),
        source: r.identity.path.clone(),
        destination: dest.to_string_lossy().into_owned(),
        integrity: r.integrity.clone(),
        backlink,
        backlink_security_sha256,
        nodes,
    };
    let encoded = serde_json::to_vec_pretty(&ticket).map_err(|_| "TICKET_JSON")?;
    if encoded.len() as u64 > record_limit(c) {
        return Err("TICKET_SIZE_LIMIT");
    }
    write_or_verify(&ticket_path(c), &encoded, record_limit(c))?;
    let digest = format!("{:x}", Sha256::digest(&encoded));
    if present(&proof_path(c))? {
        write_or_verify(&proof_path(c), digest.as_bytes(), 128)?;
    } else {
        publish_proof(&proof_path(c), digest.as_bytes())?;
    }
    fs::create_dir_all(&c.quarantine).map_err(|_| "QUARANTINE_PARENT_CREATE")?;
    checked(&dest)?;
    if !present(&dest)? {
        fs::create_dir(&dest).map_err(|_| "QUARANTINE_DESTINATION_CREATE")?;
    }
    for entry in fs::read_dir(&dest).map_err(|_| "QUARANTINE_METADATA_FAILED")? {
        if entry.map_err(|_| "QUARANTINE_METADATA_FAILED")?.file_name() != OsStr::new("理由.txt")
        {
            return Err("QUARANTINE_DESTINATION_NOT_EMPTY");
        }
    }
    write_or_verify(
        &dest.join("理由.txt"),
        why(c, &r, &ticket.nodes).as_bytes(),
        8192,
    )?;
    Ok(
        serde_json::json!({"id":id,"action":"submit_quarantine_to_exec_tool","bytes":total(&ticket.nodes),
        "entries":ticket.nodes.len(),"destination":dest,"inventory_sha256":digest,
        "powershell_command":format!("Move-Item -LiteralPath {} -Destination {} -ErrorAction Stop",quote(path),quote(&dest.join("files")))}),
    )
}
pub(super) fn finish(c: &Config, id: &str) -> Result<serde_json::Value> {
    let mut r = c.load(id)?;
    let encoded = read_record(&ticket_path(c), record_limit(c))?;
    let t: MoveTicket = serde_json::from_slice(&encoded).map_err(|_| "INVALID_TICKET")?;
    let dest = c.destination();
    if t.id != id
        || t.source != r.identity.path
        || t.destination != dest.to_string_lossy()
        || t.integrity != r.integrity
    {
        return Err("TICKET_SCOPE_MISMATCH");
    }
    let saved_proof = read_record(&proof_path(c), 128)?;
    if saved_proof != format!("{:x}", Sha256::digest(&encoded)).as_bytes() {
        return Err("INVENTORY_PROOF_CHANGED");
    }
    if r.state == "quarantined" && r.destination.as_deref() != Some(t.destination.as_str()) {
        return Err("RECEIPT_DESTINATION_MISMATCH");
    }
    if present(Path::new(&r.identity.path))? {
        return Err("TARGET_STILL_EXISTS");
    }
    let files = dest.join("files");
    let now = snapshot(c, &r, &files)?;
    if !same_nodes(c, &t.nodes, &now) {
        return Err("CONTENT_CHANGED_AFTER_PLAN");
    }
    if backlink_security(c, t.backlink.as_ref(), false)? != t.backlink_security_sha256 {
        return Err("BACKLINK_SECURITY_CHANGED");
    }
    c.repair_backlink(&r, &files, t.backlink.as_ref())?;
    if backlink_security(c, t.backlink.as_ref(), false)? != t.backlink_security_sha256 {
        return Err("BACKLINK_SECURITY_CHANGED");
    }
    let reason = dest.join("理由.txt");
    let expected = why(c, &r, &t.nodes);
    let completed = "退避状態: 完了。元の場所の不在、全項目の識別番号・名前・容量・時刻・属性・ACL・リンク情報の一致を確認した。ファイル内容は読まず、同じ実体の保持を確認した。\n";
    let saved = read_record(&reason, 8192)?;
    if saved != expected.as_bytes() && saved != format!("{expected}{completed}").as_bytes() {
        return Err("REASON_CHANGED");
    }
    if saved == expected.as_bytes() {
        let mut f = OpenOptions::new()
            .append(true)
            .open(&reason)
            .map_err(|_| "REASON_UPDATE_FAILED")?;
        f.write_all(completed.as_bytes())
            .and_then(|()| f.sync_all())
            .map_err(|_| "REASON_UPDATE_FAILED")?;
    }
    if r.state == "owned" {
        r.state = "quarantined".into();
        r.destination = Some(t.destination);
        let writing = c.receipt_path().with_extension("recovery-writing");
        write_or_verify(
            &writing,
            &serde_json::to_vec(&r).map_err(|_| "RECEIPT_JSON")?,
            8192,
        )?;
        fs::rename(writing, c.receipt_path()).map_err(|_| "RECEIPT_COMMIT_FAILED")?;
    }
    Ok(
        serde_json::json!({"id":id,"state":"quarantined","source_absent":true,"destination":dest,
        "bytes":total(&now),"entries":now.len(),"inventory_verified":true}),
    )
}
