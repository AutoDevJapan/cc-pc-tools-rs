//! Metadata-only recovery of three fixed owned trees. No delete or move API.
//! The internal file name is not a product name.
use super::*;

const VOLUME: u32 = 2229119083;
pub(super) const MATRIX: &str = "ae1a4f84fda55d86d22dcd0d64717148";
pub(super) const GENESIS: &str = "74bcf67bb31b289485bf40ede5988a46";
pub(super) const AUDIT: &str = "02a5ca348b742317fd5b907efad337e8";
// Fixed measured bounds include the root and never apply to another receipt.
fn limits(id: &str) -> Result<(usize, usize, u64)> {
    match id {
        MATRIX => Ok((6468, 367, 131984276)),
        GENESIS => Ok((93276, 3487, 1273627619)),
        AUDIT => Ok((72884, 604, 1934935656)),
        _ => Err("SCOPE_NOT_AUTHORIZED"),
    }
}
fn original_error(id: &str) -> &'static str {
    if id == AUDIT {
        "ENTRY_LIMIT_EXCEEDED"
    } else {
        "LINK_OR_REPARSE_POINT"
    }
}
const RECORD_LIMIT: u64 = 8 * 1024 * 1024;
const REPAIR: &str = "gitdir.todo2140-repair";

pub(super) struct RepoSpec {
    // Relative to the owned root; empty means the root itself is the repository.
    pub(super) dir: &'static str,
    pub(super) origin: &'static str,
    pub(super) head: String,
    pub(super) untracked: &'static [&'static str],
    // (admin, common) for a linked worktree whose `.git` is a file.
    pub(super) link: Option<(PathBuf, PathBuf)>,
}
impl RepoSpec {
    fn path(&self, root: &Path) -> PathBuf {
        if self.dir.is_empty() {
            root.to_owned()
        } else {
            root.join(self.dir)
        }
    }
    fn git_key(&self) -> String {
        if self.dir.is_empty() {
            ".git".into()
        } else {
            format!("{}/.git", self.dir)
        }
    }
}
pub(super) struct Fixed {
    id: &'static str,
    project: &'static str,
    content: &'static str,
    pub(super) created_at: &'static str,
    pub(super) integrity: &'static str,
    file_id: u64,
    repos: Vec<RepoSpec>,
}

fn spec(
    dir: &'static str,
    origin: &'static str,
    head: &str,
    untracked: &'static [&'static str],
    link: Option<(&str, &str)>,
) -> RepoSpec {
    RepoSpec {
        dir,
        origin,
        head: head.into(),
        untracked,
        link: link.map(|(a, c)| (PathBuf::from(a), PathBuf::from(c))),
    }
}
pub(super) fn fixed(id: &str) -> Option<Fixed> {
    let wt = "C:/Users/makhmeto/Desktop/dev";
    let linked = |name: &str, base: &str| {
        let common = format!("{wt}/{base}/.git");
        (format!("{common}/worktrees/{name}"), common)
    };
    Some(match id {
        MATRIX => Fixed {
            id: MATRIX,
            project: "seller-contact-2129-godd",
            content: "matrix-clone",
            created_at: "2026-10-09T17:55:48.7221982+09:00",
            integrity: "aad2cdd0b6ae0a40c20263e96feba13db21d59283235cec37771ebe18f11c9ca",
            file_id: 5629499549883108,
            repos: vec![spec(
                "",
                "https://github.com/AutoDevJapan/GoDD-Matrix.git",
                "83b8ccf6ce11e378ba7c03a904c6fcf13cc903df",
                &[],
                None,
            )],
        },
        GENESIS => Fixed {
            id: GENESIS,
            project: "seller-contact-2129-godd",
            content: "genesis-clone",
            created_at: "2026-10-09T17:55:48.708827+09:00",
            integrity: "1906fddc4a07ee011b21d45733fa308f9deebcb57a43b42b7de9b4e8367f824d",
            file_id: 16888498618309316,
            repos: vec![spec(
                "",
                "https://github.com/AutoDevJapan/GoDD-Genesis.git",
                "09cf3468c4b5805b066111493d4c903dde37226e",
                &["target-seller-contact-2132/", ".cursor/state/"],
                None,
            )],
        },
        AUDIT => {
            let (qa, qa_common) = linked("QaJaRuEn", "ADJ/QaJaRuEn");
            let (oss, oss_common) = linked("oss-mcp", "ADJ/oss-mcp");
            let (alt, alt_common) = linked("alt-line", "YEG/alt-line");
            Fixed {
                id: AUDIT,
                project: "ADJ",
                content: "seller-contact-audit-20261009",
                created_at: "2026-10-09T17:50:05.2590863+09:00",
                integrity: "db98fae73bc6eb573c25d8ea203dd87c47d01163e4ae7792e142754851d73350",
                file_id: 5629499549869580,
                repos: vec![
                    spec(
                        "QaJaRuEn",
                        "git@github.com:AutoDevJapan/QaJaRuEn.git",
                        "2a0f66d6d81a0bacb262a4953c817702656ecac4",
                        &[".cursor/state/"],
                        Some((qa.as_str(), qa_common.as_str())),
                    ),
                    spec(
                        "oss-mcp",
                        "git@github.com:AutoDevJapan/oss-mcp.git",
                        "b9235de6785374faa5808cb7174dad3b3f1ffd15",
                        &[".cursor/state/"],
                        Some((oss.as_str(), oss_common.as_str())),
                    ),
                    spec(
                        "alt-line",
                        "git@github.com:AutoDevJapan/alt-line.git",
                        "2382cc46ddcb0ec4100e735ac17f4f864353dffa",
                        &[],
                        Some((alt.as_str(), alt_common.as_str())),
                    ),
                ],
            }
        }
        _ => return None,
    })
}
pub(super) fn configure(c: &mut Config, f: Fixed) {
    c.id = f.id;
    c.project = f.project;
    c.content = f.content;
    c.store = PathBuf::from("D:/CC/Tools/safe-delete/receipts-go");
    c.temp = PathBuf::from(r"C:\Users\makhmeto\AppData\Local\Temp");
    c.pinned_identity = Some((VOLUME, f.file_id));
    c.repos.clear();
    c.seller = f.repos;
}

#[derive(Clone, Debug, Serialize, PartialEq, Eq)]
struct Entry {
    node: Node,
    // Raw FSCTL_GET_REPARSE_POINT bytes of a dependency link, as hex.
    reparse: Option<String>,
}
type Inventory = BTreeMap<String, Entry>;
struct Counts {
    entries: usize,
    bytes: u64,
    links: usize,
    outside_dependency_aliases: u64,
}
#[derive(Serialize, Deserialize, PartialEq, Eq)]
struct Id {
    volume: u32,
    file_id: u64,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Backlink {
    dir: String,
    original: Node,
    replacement: Node,
    admin: Id,
    common: Id,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct MoveTicket {
    id: String,
    source: String,
    destination: String,
    integrity: String,
    entries: usize,
    inventory_sha256: String,
    backlinks: Vec<Backlink>,
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

// OPEN_REPARSE_POINT metadata read of the link itself. The target is never
// opened, canonicalized or compared; only symlink and junction data are kept.
pub(super) fn reparse_data(path: &Path) -> Result<Vec<u8>> {
    let name = extended_path(path)?;
    let mut buf = vec![0u8; 16384];
    let mut returned = 0u32;
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
    if ok == 0 || returned < 8 || returned as usize > buf.len() {
        return Err("LINK_METADATA_FAILED");
    }
    buf.truncate(returned as usize);
    let tag = u32::from_le_bytes(buf[0..4].try_into().unwrap());
    if tag != 0xa000000c && tag != 0xa0000003 {
        return Err("UNSUPPORTED_REPARSE");
    }
    Ok(buf)
}

fn scan(root: &Path, c: &Config) -> Result<(Inventory, Counts)> {
    let (entry_limit, link_limit, byte_limit) = limits(c.id)?;
    let mut nodes = Inventory::new();
    let mut pending = vec![(root.to_owned(), String::new())];
    let mut hardlinks: BTreeMap<(u32, u64), (u32, u32, bool)> = BTreeMap::new();
    let (mut bytes, mut links) = (0u64, 0usize);
    while let Some((path, key)) = pending.pop() {
        let name = path
            .file_name()
            .and_then(OsStr::to_str)
            .ok_or("INVALID_PATH")?;
        if protected(name) {
            return Err("PROTECTED_PATH");
        }
        let n = node_no_follow(&path)?;
        let dir = n.attributes & 0x10 != 0;
        if name.eq_ignore_ascii_case(".git") {
            match c.seller.iter().find(|r| r.git_key() == key) {
                Some(r) if n.attributes & REPARSE == 0 && dir == r.link.is_none() => (),
                Some(_) => return Err("EXTERNAL_GITDIR"),
                None => return Err("UNDECLARED_GIT_METADATA"),
            }
        }
        if nodes.len() >= entry_limit {
            return Err("ENTRY_LIMIT_EXCEEDED");
        }
        let mut reparse = None;
        if n.attributes & REPARSE != 0 {
            // Dependency links only, never entered.
            let parent = key.rsplit_once('/').map_or("", |(p, _)| p);
            if n.links != 1
                || !parent
                    .split('/')
                    .any(|p| p.eq_ignore_ascii_case("node_modules"))
            {
                return Err("LINK_OUTSIDE_DEPENDENCIES");
            }
            links += 1;
            if links > link_limit {
                return Err("LINK_LIMIT_EXCEEDED");
            }
            let raw = reparse_data(&path)?;
            if node_no_follow(&path)? != n {
                return Err("LINK_CHANGED_DURING_SCAN");
            }
            reparse = Some(raw.iter().map(|b| format!("{b:02x}")).collect());
        } else if dir {
            for child in fs::read_dir(&path).map_err(|_| "CONTENT_METADATA_FAILED")? {
                let child = child.map_err(|_| "CONTENT_METADATA_FAILED")?;
                let child_name = child.file_name();
                let child_name = child_name.to_str().ok_or("INVALID_PATH")?;
                if nodes.len() + pending.len() >= entry_limit {
                    return Err("ENTRY_LIMIT_EXCEEDED");
                }
                let child_key = if key.is_empty() {
                    child_name.into()
                } else {
                    format!("{key}/{child_name}")
                };
                pending.push((child.path(), child_key));
            }
        } else {
            bytes = bytes.checked_add(n.size).ok_or("SIZE_LIMIT_EXCEEDED")?;
            if bytes > byte_limit {
                return Err("SIZE_LIMIT_EXCEEDED");
            }
            let seen = hardlinks
                .entry((n.volume, n.file_id))
                .or_insert((0, n.links, true));
            if seen.1 != n.links {
                return Err("HARD_LINK");
            }
            seen.0 += 1;
            seen.2 &= key.split('/').any(|part| part == "node_modules");
        }
        nodes.insert(key, Entry { node: n, reparse });
    }
    // A same-volume whole-root rename changes no file data or alias count.
    // Only fixed dependency names may share regular files outside the root.
    // Outside alias names are neither opened nor enumerated.
    if hardlinks
        .values()
        .any(|(inside, total, dependency)| inside > total || (inside != total && !dependency))
    {
        return Err("HARD_LINK");
    }
    let volume = nodes[""].node.volume;
    if nodes.values().any(|e| e.node.volume != volume) {
        return Err("EXTERNAL_VOLUME");
    }
    let outside_dependency_aliases = hardlinks
        .values()
        .map(|(inside, total, _)| u64::from(total - inside))
        .sum();
    let entries = nodes.len();
    Ok((
        nodes,
        Counts {
            entries,
            bytes,
            links,
            outside_dependency_aliases,
        },
    ))
}
fn digest(nodes: &Inventory) -> Result<String> {
    let mut hash = Sha256::new();
    serde_json::to_writer(&mut hash, nodes).map_err(|_| "INVENTORY_JSON")?;
    Ok(format!("{:x}", hash.finalize()))
}

fn validate_repo(path: &Path, r: &RepoSpec, before_move: bool) -> Result<()> {
    let gitdir = match &r.link {
        None => {
            let dir = path.join(".git");
            if node(&dir)?.attributes & 0x10 == 0 {
                return Err("EXTERNAL_GITDIR");
            }
            dir
        }
        Some((admin, common)) => {
            checked_admin(admin)?;
            checked_admin(common)?;
            let pointer = bounded_text(&path.join(".git"))?;
            if admin.parent() != Some(common.join("worktrees").as_path())
                || !pointer
                    .strip_prefix("gitdir: ")
                    .is_some_and(|p| same(Path::new(p), admin))
                || bounded_text(&admin.join("commondir"))? != "../.."
                || !same(
                    Path::new(&git(path, &["rev-parse", "--git-common-dir"])?),
                    common,
                )
            {
                return Err("EXTERNAL_GITDIR");
            }
            for special in [
                "modules",
                "objects/info/alternates",
                "objects/info/http-alternates",
                "info/grafts",
            ] {
                if (before_move || special != "modules") && present(&common.join(special))? {
                    return Err("EXTERNAL_OR_SHARED_GIT_METADATA");
                }
            }
            admin.clone()
        }
    };
    let own = ["commondir", "gitdir"];
    for special in [
        "commondir",
        "gitdir",
        "worktrees",
        "modules",
        "objects/info/alternates",
        "objects/info/http-alternates",
        "info/grafts",
    ] {
        if r.link.is_some() && own.contains(&special) {
            continue;
        }
        if present(&gitdir.join(special))? {
            return Err("EXTERNAL_OR_SHARED_GIT_METADATA");
        }
    }
    if !same(
        Path::new(&git(path, &["rev-parse", "--show-toplevel"])?),
        path,
    ) {
        return Err("GIT_TOP_MISMATCH");
    }
    if git(path, &["rev-parse", "HEAD"])? != r.head {
        return Err("GIT_HEAD_CHANGED");
    }
    // A worktree shares its config with other owners, so origin is a pre-move check there.
    if (before_move || r.link.is_none())
        && git(
            path,
            &["config", "--local", "--get-all", "remote.origin.url"],
        )?
        .trim_end_matches(".git")
            != r.origin.trim_end_matches(".git")
    {
        return Err("GIT_ORIGIN_CHANGED");
    }
    let status = git(
        path,
        &["status", "--porcelain=v1", "--untracked-files=normal"],
    )?;
    if !status
        .lines()
        .all(|line| r.untracked.iter().any(|name| line == format!("?? {name}")))
    {
        return Err("GIT_TRACKED_CHANGES");
    }
    if r.link.is_none() {
        // The PR head is verified upstream, so HEAD counts as pushed even when the
        // branch was pruned; every other local ref must already be on origin.
        let refs = git(path, &["for-each-ref", "--format=%(refname)"])?;
        if refs.lines().any(|l| l == "refs/stash") {
            return Err("GIT_STASH");
        }
        if !refs.lines().all(|l| {
            ["refs/heads/", "refs/remotes/", "refs/tags/"]
                .iter()
                .any(|p| l.starts_with(p))
        }) {
            return Err("GIT_UNEXPECTED_REFS");
        }
        let unpushed = git(
            path,
            &[
                "rev-list",
                "--branches",
                "--tags",
                "--not",
                "--remotes=origin",
                "HEAD",
            ],
        )?;
        if !unpushed.is_empty() {
            return Err("GIT_UNPUSHED_COMMITS");
        }
    }
    Ok(())
}

fn load(c: &Config, id: &str) -> Result<Receipt> {
    let f = fixed(id).ok_or("SCOPE_NOT_AUTHORIZED")?;
    if c.id != f.id || c.seller.is_empty() {
        return Err("SCOPE_NOT_AUTHORIZED");
    }
    let r = c.load(id)?;
    if r.state == "owned" && r.destination.is_some() {
        return Err("RECEIPT_DESTINATION_MISMATCH");
    }
    // Test fixtures cannot reproduce the real receipt; production has no override.
    #[cfg(not(test))]
    if r.integrity != f.integrity || r.identity.created_at != f.created_at {
        return Err("RECEIPT_SCOPE_MISMATCH");
    }
    Ok(r)
}
fn validate(
    c: &Config,
    r: &Receipt,
    root: &Path,
    before_move: bool,
) -> Result<(Inventory, Counts)> {
    checked(root)?;
    c.client_guard(root)?;
    for repo in &c.seller {
        if !repo.dir.is_empty() {
            c.client_guard(&repo.path(root))?;
        }
        if let Some((admin, common)) = &repo.link {
            c.client_guard(admin)?;
            c.client_guard(common)?;
        }
    }
    let n = node(root)?;
    if n.attributes & 0x10 == 0 || n.volume != r.identity.volume || n.file_id != r.identity.file_id
    {
        return Err("TARGET_IDENTITY_CHANGED");
    }
    for repo in &c.seller {
        validate_repo(&repo.path(root), repo, before_move)?;
    }
    // Scan after Git so the compared inventory is the one the move will carry.
    let (first, counts) = scan(root, c)?;
    if scan(root, c)?.0 != first {
        return Err("CONTENT_CHANGED_DURING_SCAN");
    }
    Ok((first, counts))
}
fn proof_path(c: &Config) -> PathBuf {
    c.ticket_path().with_extension("seller2140-proof.json")
}
fn id_of(path: &Path) -> Result<Id> {
    let n = node(path)?;
    Ok(Id {
        volume: n.volume,
        file_id: n.file_id,
    })
}

pub(super) fn inspect(c: &Config, id: &str) -> Result<serde_json::Value> {
    let r = load(c, id)?;
    if r.state != "owned" {
        return Err("RECEIPT_NOT_OWNED");
    }
    let (_, n) = validate(c, &r, Path::new(&r.identity.path), true)?;
    Ok(
        serde_json::json!({"id":c.id,"action":"read_only_inspection","entries":n.entries,
        "bytes":n.bytes,"links":n.links,"repositories":c.seller.len(),
        "outside_dependency_aliases":n.outside_dependency_aliases,"outside_aliases_opened":0,
        "ticket_created":false,"move_command_issued":false}),
    )
}

// Create the replacement backlink beside the fixed external admin entry. It never
// overwrites an existing file and touches no other worktree, ref or object.
fn prepare(c: &Config, root: &Path, files: &Path) -> Result<Vec<Backlink>> {
    let mut out = Vec::new();
    for r in &c.seller {
        let Some((admin, common)) = &r.link else {
            continue;
        };
        let backlink = admin.join("gitdir");
        if !same(
            Path::new(&bounded_text(&backlink)?),
            &r.path(root).join(".git"),
        ) {
            return Err("WORKTREE_BACKLINK_CHANGED");
        }
        let replacement = admin.join(REPAIR);
        let text = format!("{}\n", r.path(files).join(".git").display());
        if present(&replacement)? {
            if bounded_text(&replacement)? != text.trim() {
                return Err("WORKTREE_REPAIR_FILE_CHANGED");
            }
        } else {
            let mut f = OpenOptions::new()
                .write(true)
                .create_new(true)
                .open(&replacement)
                .map_err(|_| "WORKTREE_REPAIR_PREPARE_FAILED")?;
            f.write_all(text.as_bytes())
                .and_then(|()| f.sync_all())
                .map_err(|_| "WORKTREE_REPAIR_PREPARE_FAILED")?;
        }
        out.push(Backlink {
            dir: r.dir.into(),
            original: node(&backlink)?,
            replacement: node(&replacement)?,
            admin: id_of(admin)?,
            common: id_of(common)?,
        });
    }
    Ok(out)
}
// Each backlink is already original, already replaced, or replaced now, so a
// partial run resumes. Anything else is refused.
fn repair(c: &Config, old_root: &Path, files: &Path, saved: &[Backlink]) -> Result<()> {
    let linked: Vec<&RepoSpec> = c.seller.iter().filter(|r| r.link.is_some()).collect();
    if linked.len() != saved.len() {
        return Err("WORKTREE_TICKET_MISMATCH");
    }
    for (r, s) in linked.into_iter().zip(saved) {
        let (admin, common) = r.link.as_ref().ok_or("WORKTREE_TICKET_MISMATCH")?;
        if s.dir != r.dir {
            return Err("WORKTREE_TICKET_MISMATCH");
        }
        if id_of(admin)? != s.admin || id_of(common)? != s.common {
            return Err("WORKTREE_BACKLINK_CHANGED");
        }
        let backlink = admin.join("gitdir");
        let new = r.path(files).join(".git");
        let current = node(&backlink)?;
        let text = bounded_text(&backlink)?;
        if current == s.replacement && same(Path::new(&text), &new) {
            continue;
        }
        if current != s.original || !same(Path::new(&text), &r.path(old_root).join(".git")) {
            return Err("WORKTREE_BACKLINK_CHANGED");
        }
        let replacement = admin.join(REPAIR);
        if node(&replacement)? != s.replacement
            || !same(Path::new(&bounded_text(&replacement)?), &new)
        {
            return Err("WORKTREE_REPAIR_FAILED");
        }
        let (from, to) = (extended_path(&replacement)?, extended_path(&backlink)?);
        // REPLACE_EXISTING | WRITE_THROUGH: one atomic exchange of this backlink only.
        if unsafe { MoveFileExW(from.as_ptr(), to.as_ptr(), 0x9) } == 0 {
            return Err("WORKTREE_REPAIR_FAILED");
        }
        if node(&backlink)? != s.replacement || !same(Path::new(&bounded_text(&backlink)?), &new) {
            return Err("WORKTREE_REPAIR_FAILED");
        }
        validate_repo(&r.path(files), r, false)?;
    }
    Ok(())
}

pub(super) fn plan(c: &Config, id: &str) -> Result<serde_json::Value> {
    let r = load(c, id)?;
    if r.state != "owned" {
        return Err("RECEIPT_NOT_OWNED");
    }
    let source = Path::new(&r.identity.path);
    let dest = c.destination();
    let files = dest.join("files");
    // Git resolves its root before core.longpaths applies.
    if c.seller
        .iter()
        .any(|repo| repo.path(&files).as_os_str().encode_wide().count() > 240)
    {
        return Err("GIT_DESTINATION_ROOT_TOO_LONG");
    }
    let (nodes, n) = validate(c, &r, source, true)?;
    checked(&dest)?;
    c.client_guard(&dest)?;
    if source.components().next() != dest.components().next() {
        return Err("QUARANTINE_CROSS_DRIVE");
    }
    if present(&dest)? && !present(&c.ticket_path())? {
        return Err("QUARANTINE_ALREADY_PREPARED");
    }
    let inventory_sha256 = digest(&nodes)?;
    let ticket = MoveTicket {
        id: c.id.into(),
        source: r.identity.path.clone(),
        destination: dest.to_string_lossy().into_owned(),
        integrity: r.integrity.clone(),
        entries: n.entries,
        inventory_sha256: inventory_sha256.clone(),
        backlinks: prepare(c, source, &files)?,
    };
    let serialized = serde_json::to_vec_pretty(&ticket).map_err(|_| "TICKET_JSON")?;
    if serialized.len() as u64 > RECORD_LIMIT {
        return Err("INVALID_RECOVERY_RECORD");
    }
    // Publish intent before creating the destination; resume compares, never replaces.
    publish_proof(&c.ticket_path(), &serialized)?;
    let ticket_sha256 = format!("{:x}", Sha256::digest(&serialized));
    publish_proof(&proof_path(c), ticket_sha256.as_bytes())?;
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
        "退避状態: 未完了（移動前）。\n元の場所: {}\n大きさ: {} byte\n項目数: {}（根を含む）、依存リンク: {}件\n理由: 通常のplanとquarantineは{}で停止した。所有IDを固定した復旧手順で、リンク先に入らず、Git管理情報と全データを保持して同じドライブへ退避する。\n所有ID: {}\n",
        r.identity.path,
        n.bytes,
        n.entries,
        n.links,
        original_error(c.id),
        c.id
    );
    write_or_verify(&dest.join("理由.txt"), why.as_bytes(), 8192)?;
    Ok(
        serde_json::json!({"id":c.id,"action":"submit_quarantine_to_exec_tool","bytes":n.bytes,
        "entries":n.entries,"links":n.links,"destination":dest,"inventory_sha256":inventory_sha256,
        "worktree_backlinks":ticket.backlinks.len(),
        "powershell_command":format!("Move-Item -LiteralPath {} -Destination {} -ErrorAction Stop", quote(source), quote(&files))}),
    )
}

pub(super) fn finish(c: &Config, id: &str) -> Result<serde_json::Value> {
    let mut r = load(c, id)?;
    let serialized = read_record(&c.ticket_path(), RECORD_LIMIT)?;
    let ticket_sha256 = format!("{:x}", Sha256::digest(&serialized));
    if read_record(&proof_path(c), 128)? != ticket_sha256.as_bytes() {
        return Err("RECOVERY_RECORD_CHANGED");
    }
    let t: MoveTicket = serde_json::from_slice(&serialized).map_err(|_| "INVALID_TICKET")?;
    let dest = c.destination();
    if t.id != c.id
        || t.source != r.identity.path
        || t.integrity != r.integrity
        || !same(Path::new(&t.destination), &dest)
    {
        return Err("TICKET_SCOPE_MISMATCH");
    }
    if r.state == "quarantined" && r.destination.as_deref() != Some(t.destination.as_str()) {
        return Err("RECEIPT_DESTINATION_MISMATCH");
    }
    if present(Path::new(&r.identity.path))? {
        return Err("TARGET_STILL_EXISTS");
    }
    let files = dest.join("files");
    let (nodes, n) = validate(c, &r, &files, false)?;
    if n.entries != t.entries || digest(&nodes)? != t.inventory_sha256 {
        return Err("CONTENT_CHANGED_AFTER_PLAN");
    }
    // Success is only reported once every fixed worktree backlink points here.
    repair(c, Path::new(&r.identity.path), &files, &t.backlinks)?;
    let reason_path = dest.join("理由.txt");
    let reason = read_record(&reason_path, 8192)?;
    let completed = "退避状態: 完了。元の場所の不在、同一の根の識別番号、全項目のメタデータと依存リンクの生データの一致、Git管理情報の確認を済ませた。\n";
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
        r.destination = Some(t.destination.clone());
        let writing = c.receipt_path().with_extension("recovery-writing");
        write_or_verify(
            &writing,
            &serde_json::to_vec(&r).map_err(|_| "RECEIPT_JSON")?,
            8192,
        )?;
        fs::rename(writing, c.receipt_path()).map_err(|_| "RECEIPT_COMMIT_FAILED")?;
    }
    Ok(
        serde_json::json!({"id":c.id,"state":"quarantined","source_absent":true,"destination":dest,
        "bytes":n.bytes,"entries":n.entries,"links":n.links,"inventory_verified":true,
        "worktree_backlinks_repaired":t.backlinks.len(),
        "outside_dependency_aliases":n.outside_dependency_aliases,"outside_aliases_opened":0,
        "inventory_sha256":t.inventory_sha256,"root_volume":r.identity.volume,"root_file_id":r.identity.file_id}),
    )
}
