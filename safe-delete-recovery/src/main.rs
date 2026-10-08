#![cfg(windows)]

use safe_delete_recovery::powershell_literal as quote;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{
    collections::BTreeMap,
    ffi::OsStr,
    fs::{self, OpenOptions},
    io::{Read, Write},
    os::windows::{ffi::OsStrExt, fs::MetadataExt, process::CommandExt},
    path::{Path, PathBuf},
    process::{Command, Stdio},
    time::{Duration, Instant},
};

type Result<T> = std::result::Result<T, &'static str>;
const ID: &str = "dee85bb67fa66449ba0330f0fbe049f1";
const RESUME_ID: &str = "438ee7d343a6c84558201e2778d2c890";
const LIMIT: usize = 10000;
const REPARSE: u32 = 0x400;

#[derive(Serialize, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
struct Identity {
    version: u32,
    id: String,
    root: String,
    path: String,
    volume: u32,
    file_id: u64,
    created_at: String,
    project: String,
    content: String,
}
#[derive(Serialize, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
struct Receipt {
    #[serde(flatten)]
    identity: Identity,
    integrity: String,
    state: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    destination: Option<String>,
}
#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct Node {
    volume: u32,
    file_id: u64,
    size: u64,
    links: u32,
    attributes: u32,
    written: u64,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Ticket {
    id: String,
    source: String,
    destination: String,
    nodes: BTreeMap<String, Node>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    backlink: Option<BacklinkTicket>,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct BacklinkTicket {
    original: Node,
    replacement: Node,
    admin: Node,
    common: Node,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct ResumeProof {
    ticket_sha256: String,
    files: BTreeMap<String, String>,
}

// Only the explicitly authorized 2050 receipt may accept directory write-time
// drift. Every name and every other Win32 metadata field remains exact.
fn same_inventory(id: &str, old: &BTreeMap<String, Node>, now: &BTreeMap<String, Node>) -> bool {
    old.len() == now.len()
        && old.iter().all(|(name, expected)| {
            let Some(actual) = now.get(name) else {
                return false;
            };
            let mut expected = expected.clone();
            if id == RESUME_ID && expected.attributes & 0x10 != 0 {
                expected.written = actual.written;
            }
            expected == *actual
        })
}

fn file_digests(root: &Path, nodes: &BTreeMap<String, Node>) -> Result<BTreeMap<String, String>> {
    let mut files = BTreeMap::new();
    for (relative, expected) in nodes.iter().filter(|(_, n)| n.attributes & 0x10 == 0) {
        // These keys come from the fresh, validated public clone inventory.
        let path = root.join(relative);
        if node(&path)? != *expected {
            return Err("CONTENT_CHANGED_DURING_PROOF");
        }
        let mut file = fs::File::open(&path).map_err(|_| "CONTENT_PROOF_READ")?;
        let mut hash = Sha256::new();
        let mut buf = [0u8; 65536];
        loop {
            let count = file.read(&mut buf).map_err(|_| "CONTENT_PROOF_READ")?;
            if count == 0 {
                break;
            }
            hash.update(&buf[..count]);
        }
        if node(&path)? != *expected {
            return Err("CONTENT_CHANGED_DURING_PROOF");
        }
        files.insert(relative.clone(), format!("{:x}", hash.finalize()));
    }
    Ok(files)
}
enum RepoKind {
    Clone,
    Linked { admin: PathBuf, common: PathBuf },
    // A fixed, read-only audit acquisition. Never invoke Git or read its contents.
    OpaqueBare,
}
struct Repo {
    name: &'static str,
    origin: &'static str,
    head: String,
    saved_ref: Option<&'static str>,
    kind: RepoKind,
}
struct Config {
    store: PathBuf,
    temp: PathBuf,
    quarantine: PathBuf,
    repos: Vec<Repo>,
    guard: PathBuf,
    id: &'static str,
    project: &'static str,
    content: &'static str,
}

impl Config {
    fn production(id: &str) -> Result<Self> {
        let parent = std::env::current_exe()
            .map_err(|_| "EXE_PATH")?
            .parent()
            .ok_or("EXE_PATH")?
            .to_owned();
        let mut c = Self {
            store: parent.join("receipts-go"),
            temp: std::env::temp_dir(),
            quarantine: PathBuf::from(r"C:\Users\makhmeto\Downloads\削除希望"),
            guard: PathBuf::from("D:/CC/Tools/client-records/scrub391.exe"),
            repos: vec![
                Repo {
                    name: "kcaldav",
                    origin: "https://github.com/katsushi2441/kcaldav.git",
                    head: "e4a3ebd701a86d23fadfde4ae8be27d9bb6ec77c".into(),
                    saved_ref: None,
                    kind: RepoKind::Clone,
                },
                Repo {
                    name: "plane-jp",
                    origin: "https://github.com/katsushi2441/plane-jp.git",
                    head: "1c5078295bc8a33966d0b22ee77cd0194f49e1dc".into(),
                    saved_ref: None,
                    kind: RepoKind::Clone,
                },
            ],
            id: ID,
            project: "todo-2001",
            content: "tool-verification",
        };
        let public = "https://github.com/AutoDevJapan/cc-pc-tools-rs.git";
        let clone = |name, origin, head: &str, saved_ref| Repo {
            name,
            origin,
            head: head.into(),
            saved_ref,
            kind: RepoKind::Clone,
        };
        match id {
            ID => (),
            "76005ef45fe612479fd920447642497e" => {
                c.id = "76005ef45fe612479fd920447642497e";
                c.project = "tools";
                c.content = "todo938";
                c.repos = vec![
                    clone(
                        "repo",
                        public,
                        "d98217fe0922f217fda7b12564bfa826fc772bc0",
                        Some("refs/remotes/origin/fix/todo938-secret-redaction"),
                    ),
                    Repo {
                        name: "customer-audit.git",
                        origin: "",
                        head: String::new(),
                        saved_ref: None,
                        kind: RepoKind::OpaqueBare,
                    },
                ];
            }
            "38e3fbc1767b66eef9127f2a27567c8a" => {
                c.id = "38e3fbc1767b66eef9127f2a27567c8a";
                c.project = "chrome-tab-leak";
                c.content = "implementation-review";
                c.repos = vec![Repo {
                    name: "work",
                    origin: public,
                    head: "7f103193a439f84f40f41f986aa864ef8a60f56f".into(),
                    saved_ref: Some("refs/remotes/origin/fix/chrome-retired-records-2021"),
                    kind: RepoKind::Linked {
                        admin: PathBuf::from(
                            "D:/CC/Worktrees/cc-pc-tools-rs-chatgpt-web/.git/worktrees/work6",
                        ),
                        common: PathBuf::from("D:/CC/Worktrees/cc-pc-tools-rs-chatgpt-web/.git"),
                    },
                }];
            }
            "438ee7d343a6c84558201e2778d2c890" => {
                c.id = "438ee7d343a6c84558201e2778d2c890";
                c.project = "youtube-job-compat";
                c.content = "verified-main-tool-build";
                c.repos = vec![clone(
                    "source",
                    "git@github.com:AutoDevJapan/cc-pc-tools-rs.git",
                    "0b017ae9c35dc499fbe9014f72288064576e7c2b",
                    Some("refs/remotes/origin/main"),
                )];
            }
            "cfee3edefd3704d67c140ca6952dd7ce" => {
                c.id = "cfee3edefd3704d67c140ca6952dd7ce";
                c.project = "todo1935";
                c.content = "m1-finite-validation";
                c.repos = vec![Repo {
                    name: "fixed",
                    // This is a fixed local verification snapshot, not a public clone.
                    origin: "",
                    head: "0f01ed0a36dcc5baea61bf1f689267a362f8784c".into(),
                    saved_ref: None,
                    kind: RepoKind::Linked {
                        admin: PathBuf::from(
                            "D:/CC/Tools/agent-workspaces/todo-1842-9649f0c4667e/work/.git/worktrees/fixed",
                        ),
                        common: PathBuf::from(
                            "D:/CC/Tools/agent-workspaces/todo-1842-9649f0c4667e/work/.git",
                        ),
                    },
                }];
            }
            _ => return Err("SCOPE_NOT_AUTHORIZED"),
        }
        Ok(c)
    }
    fn receipt_path(&self) -> PathBuf {
        self.store.join(format!("{}.json", self.id))
    }
    fn ticket_path(&self) -> PathBuf {
        self.store
            .join(format!("{}.download-quarantine.json", self.id))
    }
    fn proof_path(&self) -> PathBuf {
        self.store
            .join(format!("{}.directory-resume-proof.json", self.id))
    }
    fn content_proof(
        &self,
        path: &Path,
        nodes: &BTreeMap<String, Node>,
        ticket: &[u8],
        create: bool,
    ) -> Result<()> {
        if self.id != RESUME_ID {
            return Err("SCOPE_NOT_AUTHORIZED");
        }
        let existing_proof = if !create {
            if !present(&self.proof_path())? {
                return Err("RESUME_PROOF_MISSING");
            }
            Some(read_record(&self.proof_path(), 8 * 1024 * 1024)?)
        } else {
            None
        };
        let proof = ResumeProof {
            ticket_sha256: format!("{:x}", Sha256::digest(ticket)),
            files: file_digests(path, nodes)?,
        };
        // Re-enumerate after reading all files, including file IDs and links.
        // A second content pass also detects changes which restore size/mtime.
        if scan(path, &self.repos)? != *nodes
            || file_digests(path, nodes)? != proof.files
            || scan(path, &self.repos)? != *nodes
        {
            return Err("CONTENT_CHANGED_DURING_PROOF");
        }
        let bytes = serde_json::to_vec_pretty(&proof).map_err(|_| "PROOF_JSON")?;
        if let Some(saved) = existing_proof {
            if saved != bytes || read_record(&self.proof_path(), 8 * 1024 * 1024)? != saved {
                return Err("RECOVERY_RECORD_CHANGED");
            }
            return Ok(());
        }
        publish_proof(&self.proof_path(), &bytes)
    }
    fn destination(&self) -> PathBuf {
        if self.id == ID {
            return self
                .quarantine
                .join(format!("20261008-todo-2001-tool-verification-{}", &ID[..8]));
        }
        self.quarantine.join(format!(
            "20261009-{}-{}-{}",
            self.project, self.content, self.id
        ))
    }

    fn load(&self, id: &str) -> Result<Receipt> {
        if id != self.id {
            return Err("SCOPE_NOT_AUTHORIZED");
        }
        checked(&self.receipt_path())?;
        let file = node(&self.receipt_path())?;
        if file.links != 1 || file.size > 8192 || file.attributes & 0x10 != 0 {
            return Err("INVALID_RECEIPT_FILE");
        }
        let r: Receipt =
            serde_json::from_slice(&fs::read(self.receipt_path()).map_err(|_| "RECEIPT_READ")?)
                .map_err(|_| "INVALID_RECEIPT_FILE")?;
        let expected = self.temp.join(format!("codex-owned-{}", self.id));
        if r.identity.version != 2
            || r.identity.id != self.id
            || r.identity.project != self.project
            || r.identity.content != self.content
            || !same(Path::new(&r.identity.root), &self.temp)
            || !same(Path::new(&r.identity.path), &expected)
        {
            return Err("RECEIPT_SCOPE_MISMATCH");
        }
        let json = serde_json::to_vec(&r.identity).map_err(|_| "RECEIPT_JSON")?;
        if format!("{:x}", Sha256::digest(json)) != r.integrity {
            return Err("RECEIPT_INTEGRITY_MISMATCH");
        }
        if r.state != "owned" && r.state != "quarantined" {
            return Err("RECEIPT_NOT_OWNED");
        }
        checked(&expected)?;
        Ok(r)
    }
    fn client_guard(&self, path: &Path) -> Result<()> {
        let output = hidden(&self.guard, &[OsStr::new("--guard-path"), path.as_os_str()])?;
        let guard: serde_json::Value =
            serde_json::from_slice(&output).map_err(|_| "CLIENT_GUARD_FAILED")?;
        if guard.get("client_resource").and_then(|x| x.as_bool()) != Some(false) {
            return Err("CLIENT_RESOURCE");
        }
        Ok(())
    }
    fn validate(
        &self,
        r: &Receipt,
        path: &Path,
        before_move: bool,
    ) -> Result<BTreeMap<String, Node>> {
        checked(path)?;
        self.client_guard(path)?;
        let n = node(path)?;
        if n.attributes & 0x10 == 0
            || n.volume != r.identity.volume
            || n.file_id != r.identity.file_id
        {
            return Err("TARGET_IDENTITY_CHANGED");
        }
        let nodes = scan(path, &self.repos)?;
        for repo in &self.repos {
            self.client_guard(&path.join(repo.name))?;
            if let RepoKind::Linked { admin, common } = &repo.kind {
                self.client_guard(admin)?;
                self.client_guard(common)?;
            }
            validate_repo(&path.join(repo.name), repo, before_move)?;
        }
        Ok(nodes)
    }
    fn plan(&self, id: &str) -> Result<serde_json::Value> {
        let r = self.load(id)?;
        if r.state != "owned" {
            return Err("RECEIPT_NOT_OWNED");
        }
        let path = Path::new(&r.identity.path);
        let nodes = self.validate(&r, path, true)?;
        let dest = self.destination();
        checked(&dest)?;
        self.client_guard(&dest)?;
        if path.components().next() != dest.components().next() {
            return Err("QUARANTINE_CROSS_DRIVE");
        }
        let prepared = present(&self.ticket_path())?;
        if present(&dest)? && !prepared {
            return Err("QUARANTINE_ALREADY_PREPARED");
        }
        let bytes: u64 = nodes
            .values()
            .filter(|x| x.attributes & 0x10 == 0)
            .map(|x| x.size)
            .sum();
        let why = if self.id == ID {
            format!(
                "退避状態: 未完了（移動前）。\n元の場所: {}\n大きさ: {bytes} byte\n理由: 所有一時データ内の公開配布物にGit管理領域があり、通常の削除計画と退避が停止した。\n取得元・commit・未変更・外部リンクなしを確認し、Gitを含む全内容を保持して退避する。\n所有ID: {ID}\n",
                r.identity.path
            )
        } else {
            format!(
                "退避状態: 未完了（移動前）。\n元の場所: {}\n大きさ: {bytes} byte\n理由: 所有一時データのGit保護による退避停止。\n仕様に固定した取得物・成果保存・リンクを確認し、Gitを含む全内容を保持する。\n所有ID: {}\n",
                r.identity.path, self.id
            )
        };
        let mut ticket = Ticket {
            id: self.id.into(),
            source: r.identity.path.clone(),
            destination: dest.to_string_lossy().into_owned(),
            nodes: nodes.clone(),
            backlink: self.prepare_backlink(path, &dest.join("files"))?,
        };
        let mut original_bytes = None;
        if self.id == RESUME_ID && prepared {
            let saved = read_record(&self.ticket_path(), 8 * 1024 * 1024)?;
            let old: Ticket = serde_json::from_slice(&saved).map_err(|_| "INVALID_TICKET")?;
            if old.id != ticket.id
                || old.source != ticket.source
                || old.destination != ticket.destination
                || old.backlink.is_some()
                || ticket.backlink.is_some()
                || !same_inventory(self.id, &old.nodes, &nodes)
            {
                return Err("RECOVERY_RECORD_CHANGED");
            }
            // Retain the original ticket verbatim. The content proof binds to
            // those bytes; the original file is never rewritten or discarded.
            ticket.nodes = old.nodes;
            original_bytes = Some(saved);
        }
        let ticket_bytes = match original_bytes {
            Some(saved) => saved,
            None => serde_json::to_vec_pretty(&ticket).map_err(|_| "TICKET_JSON")?,
        };
        // Publish intent before creating the destination so an interrupted plan can resume.
        write_or_verify(&self.ticket_path(), &ticket_bytes, 8 * 1024 * 1024)?;
        fs::create_dir_all(&self.quarantine).map_err(|_| "QUARANTINE_PARENT_CREATE")?;
        checked(&dest)?;
        if !present(&dest)? {
            fs::create_dir(&dest).map_err(|_| "QUARANTINE_DESTINATION_CREATE")?;
        }
        for entry in fs::read_dir(&dest).map_err(|_| "QUARANTINE_METADATA_FAILED")? {
            if entry.map_err(|_| "QUARANTINE_METADATA_FAILED")?.file_name()
                != OsStr::new("理由.txt")
            {
                return Err("QUARANTINE_DESTINATION_NOT_EMPTY");
            }
        }
        write_or_verify(&dest.join("理由.txt"), why.as_bytes(), 8192)?;
        if self.id == RESUME_ID {
            self.content_proof(path, &nodes, &ticket_bytes, true)?;
        }
        Ok(
            serde_json::json!({"id": self.id, "action": "submit_quarantine_to_exec_tool", "bytes": bytes,
            "entries": ticket.nodes.len(), "destination": dest, "reason": "owned_public_download_git",
            "powershell_command": format!("Move-Item -LiteralPath {} -Destination {} -ErrorAction Stop", quote(path), quote(&dest.join("files"))) }),
        )
    }
    fn finish(&self, id: &str) -> Result<serde_json::Value> {
        let mut r = self.load(id)?;
        let ticket_bytes = read_record(&self.ticket_path(), 8 * 1024 * 1024)?;
        let t: Ticket = serde_json::from_slice(&ticket_bytes).map_err(|_| "INVALID_TICKET")?;
        let dest = self.destination();
        if t.id != self.id || t.source != r.identity.path || !same(Path::new(&t.destination), &dest)
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
        let nodes = self.validate(&r, &files, false)?;
        let has_proof = self.id == RESUME_ID;
        if (has_proof && !same_inventory(self.id, &t.nodes, &nodes))
            || (!has_proof && nodes != t.nodes)
        {
            return Err("CONTENT_CHANGED_AFTER_PLAN");
        }
        if has_proof {
            self.content_proof(&files, &nodes, &ticket_bytes, false)?;
        }
        self.repair_backlink(&r, &files, t.backlink.as_ref())?;
        let why = dest.join("理由.txt");
        checked(&why)?;
        if node(&why)?.links != 1 {
            return Err("REASON_HARD_LINK");
        }
        let completed =
            "退避状態: 完了。元の場所の不在と、退避先の識別番号・全内容の一致を確認した。\n";
        if node(&why)?.size > 8192 {
            return Err("INVALID_REASON_FILE");
        }
        let reason = fs::read(&why).map_err(|_| "REASON_READ_FAILED")?;
        if !reason
            .windows(completed.len())
            .any(|s| s == completed.as_bytes())
        {
            let mut f = OpenOptions::new()
                .append(true)
                .open(why)
                .map_err(|_| "REASON_UPDATE_FAILED")?;
            f.write_all(completed.as_bytes())
                .map_err(|_| "REASON_UPDATE_FAILED")?;
            f.sync_all().map_err(|_| "REASON_UPDATE_FAILED")?;
        }
        if r.state == "owned" {
            r.state = "quarantined".into();
            r.destination = Some(t.destination.clone());
            let writing = self.receipt_path().with_extension("recovery-writing");
            write_or_verify(
                &writing,
                &serde_json::to_vec(&r).map_err(|_| "RECEIPT_JSON")?,
                8192,
            )?;
            fs::rename(writing, self.receipt_path()).map_err(|_| "RECEIPT_COMMIT_FAILED")?;
        }
        Ok(
            serde_json::json!({"id":self.id, "state":"quarantined", "source_absent":true,
            "destination":dest, "bytes": nodes.values().filter(|x| x.attributes & 0x10 == 0).map(|x| x.size).sum::<u64>(),
            "entries":nodes.len(), "inventory_verified":true}),
        )
    }
    fn prepare_backlink(&self, root: &Path, destination: &Path) -> Result<Option<BacklinkTicket>> {
        for repo in &self.repos {
            if let RepoKind::Linked { admin, common } = &repo.kind {
                checked_admin(admin)?;
                let backlink = admin.join("gitdir");
                if !same(
                    Path::new(&bounded_text(&backlink)?),
                    &root.join(repo.name).join(".git"),
                ) {
                    return Err("WORKTREE_BACKLINK_CHANGED");
                }
                let replacement = admin.join("gitdir.todo2052-repair");
                let text = format!("{}\n", destination.join(repo.name).join(".git").display());
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
                return Ok(Some(BacklinkTicket {
                    original: node(&backlink)?,
                    replacement: node(&replacement)?,
                    admin: node(admin)?,
                    common: node(common)?,
                }));
            }
        }
        Ok(None)
    }
    fn repair_backlink(
        &self,
        r: &Receipt,
        root: &Path,
        saved: Option<&BacklinkTicket>,
    ) -> Result<()> {
        for repo in &self.repos {
            if let RepoKind::Linked { admin, common } = &repo.kind {
                let backlink = admin.join("gitdir");
                let current = node(&backlink)?;
                let old = Path::new(&r.identity.path).join(repo.name).join(".git");
                let new = root.join(repo.name).join(".git");
                let text = bounded_text(&backlink)?;
                let saved = saved.ok_or("WORKTREE_TICKET_MISSING")?;
                let same_id = |a: &Node, b: &Node| a.volume == b.volume && a.file_id == b.file_id;
                if !same_id(&node(admin)?, &saved.admin) || !same_id(&node(common)?, &saved.common)
                {
                    return Err("WORKTREE_BACKLINK_CHANGED");
                }
                if current == saved.replacement && same(Path::new(&text), &new) {
                    return Ok(());
                }
                if current != saved.original || !same(Path::new(&text), &old) {
                    return Err("WORKTREE_BACKLINK_CHANGED");
                }
                // Only this fixed worktree's backlink changes. Common refs/other worktrees are untouched.
                let replacement = admin.join("gitdir.todo2052-repair");
                if node(&replacement)? != saved.replacement
                    || !same(Path::new(&bounded_text(&replacement)?), &new)
                {
                    return Err("WORKTREE_REPAIR_FAILED");
                }
                fs::rename(&replacement, &backlink).map_err(|_| "WORKTREE_REPAIR_FAILED")?;
                if node(&backlink)? != saved.replacement
                    || !same(Path::new(&bounded_text(&backlink)?), &new)
                {
                    return Err("WORKTREE_REPAIR_FAILED");
                }
                validate_repo(&root.join(repo.name), repo, false)?;
                return Ok(());
            }
        }
        if saved.is_some() {
            return Err("UNEXPECTED_WORKTREE_TICKET");
        }
        Ok(())
    }
}

fn same(a: &Path, b: &Path) -> bool {
    a.components()
        .map(|c| c.as_os_str().to_string_lossy().to_ascii_lowercase())
        .eq(b
            .components()
            .map(|c| c.as_os_str().to_string_lossy().to_ascii_lowercase()))
}
fn protected(name: &str) -> bool {
    [".codex", ".agents", ".ssh", "openmausbot"]
        .iter()
        .any(|x| name.eq_ignore_ascii_case(x))
}
fn checked(path: &Path) -> Result<()> {
    if !path.is_absolute()
        || path
            .components()
            .any(|c| matches!(c, std::path::Component::ParentDir))
    {
        return Err("INVALID_PATH");
    }
    for p in path.ancestors() {
        if let Some(name) = p.file_name() {
            let name = name.to_string_lossy();
            if protected(&name) || name.eq_ignore_ascii_case(".git") {
                return Err("PROTECTED_PATH");
            }
        }
        match fs::symlink_metadata(p) {
            Ok(m) if m.file_attributes() & REPARSE != 0 => return Err("LINK_OR_REPARSE_POINT"),
            Ok(m) if p != path && !m.is_dir() => return Err("ANCESTOR_NOT_DIRECTORY"),
            Ok(_) => (),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => (),
            Err(_) => return Err("PATH_METADATA_FAILED"),
        }
    }
    Ok(())
}
fn present(path: &Path) -> Result<bool> {
    match fs::symlink_metadata(path) {
        Ok(_) => Ok(true),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(false),
        Err(_) => Err("PATH_METADATA_FAILED"),
    }
}
fn write_new(path: &Path, bytes: &[u8]) -> Result<()> {
    checked(path)?;
    let mut f = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(path)
        .map_err(|_| "RECORD_CREATE_FAILED")?;
    f.write_all(bytes)
        .and_then(|()| f.sync_all())
        .map_err(|_| "RECORD_WRITE_FAILED")
}
fn write_or_verify(path: &Path, bytes: &[u8], limit: u64) -> Result<()> {
    checked(path)?;
    if present(path)? {
        let n = node(path)?;
        if n.links != 1 || n.size > limit || n.attributes & 0x10 != 0 {
            return Err("INVALID_RECOVERY_RECORD");
        }
        if fs::read(path).map_err(|_| "RECOVERY_RECORD_READ")? != bytes {
            return Err("RECOVERY_RECORD_CHANGED");
        }
        Ok(())
    } else {
        write_new(path, bytes)
    }
}
fn read_record(path: &Path, limit: u64) -> Result<Vec<u8>> {
    checked(path)?;
    let n = node(path)?;
    if n.links != 1 || n.size > limit || n.attributes & 0x10 != 0 {
        return Err("INVALID_RECOVERY_RECORD");
    }
    fs::read(path).map_err(|_| "RECOVERY_RECORD_READ")
}
fn publish_proof(path: &Path, bytes: &[u8]) -> Result<()> {
    if present(path)? {
        return write_or_verify(path, bytes, 8 * 1024 * 1024);
    }
    // Publish a fully synchronized new record without replacing an existing one.
    // An interruption leaves only a uniquely named unpublished staging record;
    // the next attempt creates a different staging record, never edits that one.
    let nonce = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map_err(|_| "PROOF_CLOCK")?
        .as_nanos();
    let stage = path.with_extension(format!("proof-writing-{}-{nonce}", std::process::id()));
    write_new(&stage, bytes)?;
    let source = extended_path(&stage)?;
    let destination = extended_path(path)?;
    // WRITE_THROUGH without REPLACE_EXISTING: fail rather than overwrite.
    if unsafe { MoveFileExW(source.as_ptr(), destination.as_ptr(), 0x8) } == 0 {
        return Err("PROOF_PUBLISH_FAILED");
    }
    write_or_verify(path, bytes, 8 * 1024 * 1024)
}
fn scan(root: &Path, repos: &[Repo]) -> Result<BTreeMap<String, Node>> {
    let mut nodes = BTreeMap::new();
    let mut pending = vec![root.to_owned()];
    let mut links: BTreeMap<(u32, u64), (u32, u32)> = BTreeMap::new();
    let mut opaque_counts: BTreeMap<(u32, u64), usize> = BTreeMap::new();
    while let Some(path) = pending.pop() {
        let relative = path
            .strip_prefix(root)
            .map_err(|_| "PATH_OUTSIDE_ROOT")?
            .to_string_lossy()
            .replace('\\', "/");
        let name = path.file_name().ok_or("INVALID_PATH")?.to_string_lossy();
        if protected(&name) {
            return Err("PROTECTED_PATH");
        }
        if name.eq_ignore_ascii_case(".git")
            && !repos.iter().any(|r| relative == format!("{}/.git", r.name))
        {
            return Err("UNDECLARED_GIT_METADATA");
        }
        let n = node(&path)?;
        if name.eq_ignore_ascii_case(".git")
            && n.attributes & 0x10 == 0
            && !repos.iter().any(|r| {
                relative == format!("{}/.git", r.name) && matches!(r.kind, RepoKind::Linked { .. })
            })
        {
            return Err("EXTERNAL_GITDIR");
        }
        if nodes.len() >= LIMIT {
            return Err("ENTRY_LIMIT_EXCEEDED");
        }
        if n.attributes & 0x10 != 0 {
            for child in fs::read_dir(&path).map_err(|_| "CONTENT_METADATA_FAILED")? {
                let child = child.map_err(|_| "CONTENT_METADATA_FAILED")?;
                if nodes.len() + pending.len() >= LIMIT {
                    return Err("ENTRY_LIMIT_EXCEEDED");
                }
                pending.push(child.path());
            }
        } else {
            let count = links.entry((n.volume, n.file_id)).or_insert((0, n.links));
            if count.1 != n.links {
                return Err("HARD_LINK");
            }
            count.0 += 1;
        }
        // Do not recreate confidential audit ref/path names in the recovery ticket.
        let key = if repos.iter().any(|r| {
            matches!(r.kind, RepoKind::OpaqueBare)
                && (relative == r.name || relative.starts_with(&format!("{}/", r.name)))
        }) {
            let count = opaque_counts.entry((n.volume, n.file_id)).or_default();
            *count += 1;
            format!("opaque:{}:{}:{}", n.volume, n.file_id, count)
        } else {
            relative
        };
        nodes.insert(key, n);
    }
    if links.values().any(|(inside, total)| inside != total) {
        return Err("HARD_LINK");
    }
    Ok(nodes)
}
fn hidden(exe: &Path, args: &[&OsStr]) -> Result<Vec<u8>> {
    let mut c = Command::new(exe);
    c.args(args)
        .creation_flags(0x08000000)
        .stdout(Stdio::piped())
        .stderr(Stdio::null());
    #[cfg(test)]
    c.stderr(Stdio::inherit());
    // Isolate Git from inherited repository/config/credential environment.
    for (key, _) in std::env::vars_os() {
        if key
            .to_string_lossy()
            .to_ascii_uppercase()
            .starts_with("GIT_")
        {
            c.env_remove(key);
        }
    }
    c.env("GIT_CONFIG_NOSYSTEM", "1")
        .env("GIT_CONFIG_GLOBAL", "NUL")
        .env("GIT_TERMINAL_PROMPT", "0");
    let mut child = c.spawn().map_err(|_| "VALIDATOR_START_FAILED")?;
    let out = child.stdout.take().ok_or("VALIDATOR_OUTPUT")?;
    let reader = std::thread::spawn(move || {
        use std::io::Read;
        let mut b = Vec::new();
        out.take(1024 * 1024 + 1).read_to_end(&mut b).map(|_| b)
    });
    let start = Instant::now();
    let status = loop {
        match child.try_wait() {
            Ok(Some(s)) => break s,
            Ok(None) if start.elapsed() < Duration::from_secs(30) => {
                std::thread::sleep(Duration::from_millis(20))
            }
            _ => {
                let _ = child.kill();
                let _ = child.wait();
                return Err("VALIDATOR_TIMEOUT");
            }
        }
    };
    let output = reader
        .join()
        .map_err(|_| "VALIDATOR_OUTPUT")?
        .map_err(|_| "VALIDATOR_OUTPUT")?;
    if !status.success() || output.len() > 1024 * 1024 {
        #[cfg(test)]
        eprintln!(
            "fixture validator {:?}: {status}, output bytes {}",
            exe.file_name(),
            output.len()
        );
        return Err("VALIDATOR_FAILED");
    }
    Ok(output)
}
fn git(path: &Path, args: &[&str]) -> Result<String> {
    let fixed = [
        "--no-optional-locks",
        "-c",
        "core.fsmonitor=false",
        "-c",
        "core.hooksPath=NUL",
        "-C",
    ];
    let mut argv: Vec<&OsStr> = fixed.iter().map(OsStr::new).collect();
    argv.push(path.as_os_str());
    argv.extend(args.iter().map(OsStr::new));
    String::from_utf8(hidden(Path::new("git.exe"), &argv)?)
        .map(|s| s.trim().into())
        .map_err(|_| "GIT_OUTPUT")
}
fn bounded_text(path: &Path) -> Result<String> {
    let n = node(path)?;
    if n.size > 4096 || n.links != 1 || n.attributes & 0x10 != 0 {
        return Err("INVALID_GIT_POINTER");
    }
    fs::read_to_string(path)
        .map(|s| s.trim().into())
        .map_err(|_| "INVALID_GIT_POINTER")
}
fn checked_admin(path: &Path) -> Result<()> {
    if !path.is_absolute()
        || path
            .components()
            .any(|c| matches!(c, std::path::Component::ParentDir))
    {
        return Err("INVALID_GIT_ADMIN_PATH");
    }
    for p in path.ancestors() {
        if p.file_name()
            .is_some_and(|n| protected(&n.to_string_lossy()))
        {
            return Err("PROTECTED_PATH");
        }
        if node(p)?.attributes & 0x10 == 0 {
            return Err("INVALID_GIT_ADMIN_PATH");
        }
    }
    Ok(())
}
fn validate_repo(path: &Path, repo: &Repo, before_move: bool) -> Result<()> {
    let gitdir = match &repo.kind {
        RepoKind::OpaqueBare => {
            // Inspect only names and metadata of the fixed audit acquisition.
            // Its config, refs, object contents and origin are never read or executed.
            for special in [
                "commondir",
                "gitdir",
                "worktrees",
                "modules",
                "objects/info/alternates",
                "objects/info/http-alternates",
            ] {
                if present(&path.join(special))? {
                    return Err("EXTERNAL_OR_SHARED_GIT_METADATA");
                }
            }
            for required in ["HEAD", "config", "objects", "refs"] {
                node(&path.join(required))?;
            }
            return Ok(());
        }
        RepoKind::Clone => {
            let dir = path.join(".git");
            if node(&dir)?.attributes & 0x10 == 0 {
                return Err("EXTERNAL_GITDIR");
            }
            dir
        }
        RepoKind::Linked { admin, common } => {
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
            // External objects/submodules remain forbidden even for the one fixed worktree.
            for special in [
                "modules",
                "objects/info/alternates",
                "objects/info/http-alternates",
            ] {
                if (before_move || special != "modules") && present(&common.join(special))? {
                    return Err("EXTERNAL_OR_SHARED_GIT_METADATA");
                }
            }
            admin.clone()
        }
    };
    for special in [
        "commondir",
        "gitdir",
        "worktrees",
        "modules",
        "objects/info/alternates",
        "objects/info/http-alternates",
    ] {
        if matches!(repo.kind, RepoKind::Linked { .. }) && matches!(special, "commondir" | "gitdir")
        {
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
    if git(path, &["rev-parse", "HEAD"])? != repo.head {
        return Err("GIT_HEAD_CHANGED");
    }
    let saved_checks = before_move || !matches!(repo.kind, RepoKind::Linked { .. });
    if saved_checks && repo.origin.is_empty() && !git(path, &["remote"])?.is_empty() {
        return Err("GIT_ORIGIN_CHANGED");
    }
    if saved_checks
        && !repo.origin.is_empty()
        && git(
            path,
            &["config", "--local", "--get-all", "remote.origin.url"],
        )?
        .trim_end_matches(".git")
            != repo.origin.trim_end_matches(".git")
    {
        return Err("GIT_ORIGIN_CHANGED");
    }
    let untracked = if repo.saved_ref.is_some() {
        "--untracked-files=all"
    } else {
        "--untracked-files=no"
    };
    if !git(path, &["status", "--porcelain=v1", untracked])?.is_empty() {
        return Err("GIT_TRACKED_CHANGES");
    }
    if let Some(saved) = repo.saved_ref.filter(|_| saved_checks)
        && git(path, &["rev-parse", saved])? != repo.head
    {
        return Err("GIT_UNPUSHED_COMMITS");
    }
    if !matches!(repo.kind, RepoKind::Linked { .. })
        && !git(
            path,
            &["rev-list", "--branches", "--not", "--remotes=origin"],
        )?
        .is_empty()
    {
        return Err("GIT_UNPUSHED_COMMITS");
    }
    if !matches!(repo.kind, RepoKind::Linked { .. })
        && !git(path, &["for-each-ref", "--format=%(refname)", "refs/stash"])?.is_empty()
    {
        return Err("GIT_STASH");
    }
    Ok(())
}

#[repr(C)]
#[derive(Default)]
struct WinInfo {
    attr: u32,
    created: [u32; 2],
    accessed: [u32; 2],
    written: [u32; 2],
    volume: u32,
    size_high: u32,
    size_low: u32,
    links: u32,
    index_high: u32,
    index_low: u32,
}
#[link(name = "kernel32")]
unsafe extern "system" {
    fn MoveFileExW(source: *const u16, destination: *const u16, flags: u32) -> i32;
    fn CreateFileW(
        name: *const u16,
        access: u32,
        share: u32,
        security: *const u8,
        disposition: u32,
        flags: u32,
        template: isize,
    ) -> isize;
    fn GetFileInformationByHandle(handle: isize, info: *mut WinInfo) -> i32;
    fn CloseHandle(handle: isize) -> i32;
}
fn extended_path(path: &Path) -> Result<Vec<u16>> {
    if !path.is_absolute() {
        return Err("INVALID_PATH");
    }
    let raw: Vec<u16> = path
        .as_os_str()
        .encode_wide()
        .map(|c| if c == 47 { 92 } else { c })
        .collect();
    let mut name = if raw.starts_with(&[92, 92, 63, 92]) {
        raw
    } else if raw.starts_with(&[92, 92]) {
        OsStr::new(r"\\?\UNC\")
            .encode_wide()
            .chain(raw[2..].iter().copied())
            .collect()
    } else {
        OsStr::new(r"\\?\").encode_wide().chain(raw).collect()
    };
    name.push(0);
    Ok(name)
}
fn node(path: &Path) -> Result<Node> {
    let name = extended_path(path)?;
    let mut info = WinInfo::default();
    // Metadata access only, with OPEN_REPARSE_POINT; never follows a junction.
    let ok = unsafe {
        let h = CreateFileW(name.as_ptr(), 0x80, 7, std::ptr::null(), 3, 0x02200000, 0);
        if h == -1 {
            return Err("IDENTITY_READ_FAILED");
        }
        let ok = GetFileInformationByHandle(h, &mut info);
        CloseHandle(h);
        ok
    };
    if ok == 0 {
        return Err("IDENTITY_READ_FAILED");
    }
    if info.attr & REPARSE != 0 {
        return Err("LINK_OR_REPARSE_POINT");
    }
    Ok(Node {
        volume: info.volume,
        file_id: ((info.index_high as u64) << 32) | info.index_low as u64,
        size: ((info.size_high as u64) << 32) | info.size_low as u64,
        links: info.links,
        attributes: info.attr,
        written: ((info.written[1] as u64) << 32) | info.written[0] as u64,
    })
}
fn run() -> Result<serde_json::Value> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.len() != 3 || args[1] != "--id" {
        return Err("INVALID_ARGUMENTS");
    }
    let c = Config::production(&args[2])?;
    match args[0].as_str() {
        "plan" => c.plan(&args[2]),
        "finish" => c.finish(&args[2]),
        _ => Err("INVALID_ACTION"),
    }
}
fn main() {
    match run() {
        Ok(v) => println!("{v}"),
        Err(code) => {
            println!("{}", serde_json::json!({"ok":false,"code":code}));
            std::process::exit(2);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::ops::{Deref, DerefMut};
    use std::sync::atomic::{AtomicU64, Ordering};
    static SERIAL: AtomicU64 = AtomicU64::new(0);
    struct Fixture {
        config: Config,
        base: PathBuf,
        identity: Node,
    }
    impl Deref for Fixture {
        type Target = Config;
        fn deref(&self) -> &Config {
            &self.config
        }
    }
    impl DerefMut for Fixture {
        fn deref_mut(&mut self) -> &mut Config {
            &mut self.config
        }
    }
    // Only test-created cases: this is never compiled into the production tool.
    // This module is Windows-only: false clears the DOS read-only bit, not Unix permissions.
    #[allow(clippy::permissions_set_readonly_false)]
    fn remove_fixture(base: &Path, identity: &Node) -> Result<()> {
        let owner =
            PathBuf::from(std::env::var_os("SAFE_DELETE_TEST_ROOT").ok_or("TEST_ROOT_MISSING")?);
        checked(&owner)?;
        checked(base)?;
        if base.parent() != Some(owner.as_path())
            || !base
                .file_name()
                .ok_or("INVALID_TEST_PATH")?
                .to_string_lossy()
                .starts_with("case-")
        {
            return Err("TEST_PATH_OUTSIDE_OWNER");
        }
        let current = node(base)?;
        if current.volume != identity.volume || current.file_id != identity.file_id {
            return Err("TEST_IDENTITY_CHANGED");
        }
        let mut pending = vec![base.to_owned()];
        while let Some(p) = pending.pop() {
            let m = fs::symlink_metadata(&p).map_err(|_| "TEST_METADATA_FAILED")?;
            if m.file_attributes() & REPARSE != 0 {
                continue;
            }
            if m.is_dir() {
                for entry in fs::read_dir(&p).map_err(|_| "TEST_METADATA_FAILED")? {
                    pending.push(entry.map_err(|_| "TEST_METADATA_FAILED")?.path());
                }
            } else if m.permissions().readonly() {
                let mut permissions = m.permissions();
                permissions.set_readonly(false);
                fs::set_permissions(&p, permissions).map_err(|_| "TEST_ATTRIBUTE_FAILED")?;
            }
        }
        // Windows remove_dir_all unlinks reparse points rather than traversing them.
        fs::remove_dir_all(base).map_err(|_| "TEST_FIXTURE_CLEANUP_FAILED")
    }
    impl Drop for Fixture {
        fn drop(&mut self) {
            if let Err(code) = remove_fixture(&self.base, &self.identity) {
                if std::thread::panicking() {
                    eprintln!("test fixture retained: {code}");
                } else {
                    panic!("test fixture cleanup failed: {code}");
                }
            }
        }
    }
    #[test]
    #[ignore = "explicit recovery for cases generated by earlier versions in this owned test root"]
    fn retire_prior_generated_fixtures() {
        let owner = PathBuf::from(std::env::var_os("SAFE_DELETE_TEST_ROOT").unwrap());
        // The exact owned root is created by this task. No other root is accepted for legacy retirement.
        assert!(same(
            &owner,
            Path::new(
                r"C:\Users\makhmeto\AppData\Local\Temp\codex-owned-3d76fd4c38f5355b9378be03e062914a"
            )
        ));
        checked(&owner).unwrap();
        for entry in fs::read_dir(&owner).unwrap() {
            let path = entry.unwrap().path();
            if path
                .file_name()
                .unwrap()
                .to_string_lossy()
                .starts_with("case-")
            {
                let n = node(&path).unwrap();
                remove_fixture(&path, &n).unwrap();
            }
        }
    }
    #[test]
    fn real_go_receipt_checksum_is_compatible() {
        let identity = Identity {
            version: 2,
            id: ID.into(),
            root: r"C:\Users\makhmeto\AppData\Local\Temp".into(),
            path: format!(r"C:\Users\makhmeto\AppData\Local\Temp\codex-owned-{ID}"),
            volume: 2229119083,
            file_id: 15481123719934670,
            created_at: "2026-10-08T15:34:13.325996+09:00".into(),
            project: "todo-2001".into(),
            content: "tool-verification".into(),
        };
        assert_eq!(
            format!(
                "{:x}",
                Sha256::digest(serde_json::to_vec(&identity).unwrap())
            ),
            "3451328d31fcb9f36297c8d085cb3743de54fafec0b248ed14e23c1b18f68ce7"
        );
    }
    fn fixture() -> Fixture {
        fixture_for(ID)
    }
    fn fixture_for(id: &str) -> Fixture {
        let owner = std::env::var_os("SAFE_DELETE_TEST_ROOT")
            .expect("tests require an owned safe-delete folder");
        let base = PathBuf::from(owner).join(format!(
            "case-{}-{}",
            std::process::id(),
            SERIAL.fetch_add(1, Ordering::SeqCst)
        ));
        fs::create_dir(&base).unwrap();
        let mut c = Config::production(id).unwrap();
        if let Some(guard) = std::env::var_os("SAFE_DELETE_TEST_GUARD") {
            c.guard = guard.into();
        }
        c.temp = base.clone();
        c.store = base.join("receipts");
        c.quarantine = base.join("quarantine");
        let mut fixture = Fixture {
            config: c,
            identity: node(&base).unwrap(),
            base: base.clone(),
        };
        let c = &mut fixture.config;
        fs::create_dir(&c.store).unwrap();
        let target = base.join(format!("codex-owned-{}", c.id));
        fs::create_dir(&target).unwrap();
        for repo in &mut c.repos {
            let path = target.join(repo.name);
            if matches!(repo.kind, RepoKind::OpaqueBare) {
                fs::create_dir(&path).unwrap();
                git(&path, &["init", "--bare"]).unwrap();
                // Invalid config proves the opaque acquisition is never executed/read by Git.
                fs::write(
                    path.join("config"),
                    b"opaque fixture; not a valid git config\n",
                )
                .unwrap();
                fs::write(path.join("refs/fixture-private-name"), b"opaque fixture\n").unwrap();
                continue;
            }
            let seed = if matches!(repo.kind, RepoKind::Linked { .. }) {
                base.join("common")
            } else {
                path.clone()
            };
            fs::create_dir(&seed).unwrap();
            seed_repo(&seed, repo);
            if matches!(repo.kind, RepoKind::Linked { .. }) {
                git(
                    &seed,
                    &[
                        "worktree",
                        "add",
                        "-b",
                        "fixture-linked",
                        path.to_str().unwrap(),
                    ],
                )
                .unwrap();
                repo.kind = RepoKind::Linked {
                    admin: seed.join(".git/worktrees").join(repo.name),
                    common: seed.join(".git"),
                };
            }
        }
        let n = node(&target).unwrap();
        let identity = Identity {
            version: 2,
            id: c.id.into(),
            root: base.to_string_lossy().into_owned(),
            path: target.to_string_lossy().into_owned(),
            volume: n.volume,
            file_id: n.file_id,
            created_at: "2026-10-08T15:34:13.325996+09:00".into(),
            project: c.project.into(),
            content: c.content.into(),
        };
        let integrity = format!(
            "{:x}",
            Sha256::digest(serde_json::to_vec(&identity).unwrap())
        );
        let r = Receipt {
            identity,
            integrity,
            state: "owned".into(),
            destination: None,
        };
        write_new(&c.receipt_path(), &serde_json::to_vec(&r).unwrap()).unwrap();
        fixture
    }
    fn seed_repo(path: &Path, repo: &mut Repo) {
        git(path, &["init", "-b", "main"]).unwrap();
        fs::write(path.join("source.txt"), b"public fixture\n").unwrap();
        git(path, &["add", "source.txt"]).unwrap();
        git(
            path,
            &[
                "-c",
                "user.name=Fixture",
                "-c",
                "user.email=fixture@example.invalid",
                "commit",
                "-m",
                "fixture",
            ],
        )
        .unwrap();
        repo.head = git(path, &["rev-parse", "HEAD"]).unwrap();
        if !repo.origin.is_empty() {
            git(path, &["remote", "add", "origin", repo.origin]).unwrap();
            git(
                path,
                &[
                    "update-ref",
                    repo.saved_ref.unwrap_or("refs/remotes/origin/main"),
                    &repo.head,
                ],
            )
            .unwrap();
        }
    }
    fn source(c: &Config) -> PathBuf {
        c.temp.join(format!("codex-owned-{}", c.id))
    }
    #[test]
    fn receipt_digest_matches_go_and_tampering_is_rejected() {
        let c = fixture();
        let mut r = c.load(ID).unwrap();
        r.identity.content = "different".into();
        fs::write(c.receipt_path(), serde_json::to_vec(&r).unwrap()).unwrap();
        assert!(matches!(c.load(ID), Err("RECEIPT_SCOPE_MISMATCH")));
    }
    #[test]
    fn valid_plan_preserves_git_and_finish_checks_whole_inventory() {
        let c = fixture();
        let source = source(&c);
        fs::write(source.join("trial-session"), b"disposable fixture").unwrap();
        let plan = c.plan(ID).unwrap();
        assert!(
            plan["powershell_command"]
                .as_str()
                .unwrap()
                .starts_with("Move-Item -LiteralPath")
        );
        assert!(matches!(c.finish(ID), Err("TARGET_STILL_EXISTS")));
        // Test-only rename models the separately submitted ordinary Move-Item.
        fs::rename(&source, c.destination().join("files")).unwrap();
        let result = c.finish(ID).unwrap();
        assert_eq!(result["state"], "quarantined");
        assert_eq!(c.load(ID).unwrap().state, "quarantined");
        assert!(c.destination().join("files/kcaldav/.git").is_dir());
        assert!(c.destination().join("files/trial-session").is_file());
        assert!(!source.exists());
        c.finish(ID).unwrap();
    }
    #[test]
    fn dirty_tracked_file_is_rejected_without_preparing_destination() {
        let c = fixture();
        fs::write(source(&c).join("kcaldav/source.txt"), b"unsaved work").unwrap();
        assert!(matches!(c.plan(ID), Err("GIT_TRACKED_CHANGES")));
        assert!(!c.destination().exists());
        assert!(source(&c).is_dir());
    }
    #[test]
    fn other_receipts_and_nested_git_and_protected_bot_are_rejected() {
        let c = fixture();
        assert!(matches!(
            c.plan("00000000000000000000000000000000"),
            Err("SCOPE_NOT_AUTHORIZED")
        ));
        fs::create_dir(source(&c).join("OpenMausBot")).unwrap();
        assert!(matches!(c.plan(ID), Err("PROTECTED_PATH")));
        let c = fixture();
        fs::create_dir_all(source(&c).join("extra/.git")).unwrap();
        assert!(matches!(c.plan(ID), Err("UNDECLARED_GIT_METADATA")));
    }
    #[test]
    fn external_git_alternates_and_unpushed_history_are_rejected() {
        let c = fixture();
        fs::write(
            source(&c).join("kcaldav/.git/objects/info/alternates"),
            b"C:/elsewhere",
        )
        .unwrap();
        assert!(matches!(c.plan(ID), Err("EXTERNAL_OR_SHARED_GIT_METADATA")));
        let c = fixture();
        let repo = source(&c).join("kcaldav");
        git(
            &repo,
            &[
                "update-ref",
                "refs/remotes/origin/main",
                "0000000000000000000000000000000000000000",
            ],
        )
        .unwrap();
        assert!(matches!(c.plan(ID), Err("GIT_UNPUSHED_COMMITS")));
    }
    #[test]
    fn external_hardlink_is_rejected() {
        let c = fixture();
        fs::hard_link(
            source(&c).join("kcaldav/source.txt"),
            c.temp.join("external.txt"),
        )
        .unwrap();
        assert!(matches!(c.plan(ID), Err("HARD_LINK")));
        assert!(!c.destination().exists());
    }
    #[test]
    fn replaced_source_identity_and_changed_moved_inventory_are_rejected() {
        let c = fixture();
        let src = source(&c);
        fs::rename(&src, c.temp.join("original")).unwrap();
        fs::create_dir(&src).unwrap();
        assert!(matches!(c.plan(ID), Err("TARGET_IDENTITY_CHANGED")));
        let c = fixture();
        c.plan(ID).unwrap();
        fs::rename(source(&c), c.destination().join("files")).unwrap();
        fs::write(c.destination().join("files/new-data"), b"unexpected write").unwrap();
        assert!(matches!(c.finish(ID), Err("CONTENT_CHANGED_AFTER_PLAN")));
        assert_eq!(c.load(ID).unwrap().state, "owned");
    }
    #[test]
    fn trailing_separator_and_interrupted_plan_and_receipt_commit_resume() {
        let mut c = fixture();
        c.temp = PathBuf::from(format!("{}\\", c.temp.display()));
        c.load(ID).unwrap();
        let first = c.plan(ID).unwrap();
        let second = c.plan(ID).unwrap();
        assert_eq!(first, second);
        let src = source(&c);
        fs::rename(src, c.destination().join("files")).unwrap();
        let mut r = c.load(ID).unwrap();
        r.state = "quarantined".into();
        r.destination = Some(c.destination().to_string_lossy().into_owned());
        let writing = c.receipt_path().with_extension("recovery-writing");
        write_new(&writing, &serde_json::to_vec(&r).unwrap()).unwrap();
        c.finish(ID).unwrap();
        c.finish(ID).unwrap();
        let reason = fs::read_to_string(c.destination().join("理由.txt")).unwrap();
        assert_eq!(reason.matches("退避状態: 完了。").count(), 1);
    }
    #[test]
    fn destination_collision_and_gitdir_pointer_are_rejected() {
        let c = fixture();
        fs::create_dir_all(c.destination()).unwrap();
        assert!(matches!(c.plan(ID), Err("QUARANTINE_ALREADY_PREPARED")));
        let c = fixture();
        let p = source(&c).join("kcaldav/.git");
        fs::rename(&p, source(&c).join("git-preserved")).unwrap();
        fs::write(p, b"gitdir: C:/external\n").unwrap();
        assert!(matches!(c.plan(ID), Err("EXTERNAL_GITDIR")));
    }
    #[test]
    fn junction_and_missing_client_guard_are_rejected() {
        let c = fixture();
        let link = source(&c).join("junction");
        let cmd = format!(
            "New-Item -ItemType Junction -Path {} -Target {} | Out-Null",
            quote(&link),
            quote(&c.store)
        );
        hidden(
            Path::new("pwsh.exe"),
            &[
                OsStr::new("-NoProfile"),
                OsStr::new("-NonInteractive"),
                OsStr::new("-Command"),
                OsStr::new(&cmd),
            ],
        )
        .unwrap();
        assert!(matches!(c.plan(ID), Err("LINK_OR_REPARSE_POINT")));
        let mut c = fixture();
        c.guard = c.temp.join("missing-guard.exe");
        assert!(matches!(c.plan(ID), Err("VALIDATOR_START_FAILED")));
        assert!(!c.destination().exists());
    }
    #[test]
    fn fixed_profiles_reject_unknown_id_and_cross_task_receipt() {
        assert!(matches!(
            Config::production("00000000000000000000000000000000"),
            Err("SCOPE_NOT_AUTHORIZED")
        ));
        let c = fixture_for("438ee7d343a6c84558201e2778d2c890");
        let mut r = c.load(c.id).unwrap();
        r.identity.project = "another-task".into();
        r.integrity = format!(
            "{:x}",
            Sha256::digest(serde_json::to_vec(&r.identity).unwrap())
        );
        fs::write(c.receipt_path(), serde_json::to_vec(&r).unwrap()).unwrap();
        assert!(matches!(c.plan(c.id), Err("RECEIPT_SCOPE_MISMATCH")));
    }
    #[test]
    fn opaque_audit_is_preserved_without_parsing_config_or_recording_ref_names() {
        let c = fixture_for("76005ef45fe612479fd920447642497e");
        c.plan(c.id).unwrap();
        let ticket = fs::read_to_string(c.ticket_path()).unwrap();
        assert!(!ticket.contains("fixture-private-name"));
        assert!(!ticket.contains("customer-audit.git"));
        fs::rename(source(&c), c.destination().join("files")).unwrap();
        c.finish(c.id).unwrap();
        assert_eq!(
            fs::read(c.destination().join("files/customer-audit.git/config")).unwrap(),
            b"opaque fixture; not a valid git config\n"
        );
    }
    #[test]
    fn build_internal_hardlinks_are_preserved_and_extra_git_is_rejected() {
        let c = fixture_for("438ee7d343a6c84558201e2778d2c890");
        let target = source(&c).join("target");
        fs::create_dir(&target).unwrap();
        fs::write(target.join("output"), b"fixture").unwrap();
        fs::hard_link(target.join("output"), target.join("output-linked")).unwrap();
        c.plan(c.id).unwrap();
        fs::rename(source(&c), c.destination().join("files")).unwrap();
        c.finish(c.id).unwrap();
        let c = fixture_for("438ee7d343a6c84558201e2778d2c890");
        fs::create_dir_all(source(&c).join("unexpected/.git")).unwrap();
        assert!(matches!(c.plan(c.id), Err("UNDECLARED_GIT_METADATA")));
    }
    fn drift_directory(path: &Path) {
        use std::os::windows::fs::OpenOptionsExt;
        let stamp = fs::metadata(path).unwrap().modified().unwrap() + Duration::from_secs(30);
        OpenOptions::new()
            .write(true)
            .custom_flags(0x02000000)
            .open(path)
            .unwrap()
            .set_times(fs::FileTimes::new().set_modified(stamp))
            .unwrap();
    }
    #[test]
    fn legacy_2050_directory_drift_preserves_ticket_and_resumes_through_finish() {
        let c = fixture_for(RESUME_ID);
        let first = c.plan(c.id).unwrap();
        let original = fs::read(c.ticket_path()).unwrap();
        // Model an old deployed plan, which has no content proof.
        fs::remove_file(c.proof_path()).unwrap();
        drift_directory(&source(&c).join("source/.git"));
        assert_eq!(first, c.plan(c.id).unwrap());
        assert_eq!(fs::read(c.ticket_path()).unwrap(), original);
        assert_eq!(first, c.plan(c.id).unwrap());
        fs::rename(source(&c), c.destination().join("files")).unwrap();
        drift_directory(&c.destination().join("files/source/.git"));
        c.finish(c.id).unwrap();
        c.finish(c.id).unwrap();
        assert_eq!(fs::read(c.ticket_path()).unwrap(), original);
        assert_eq!(c.load(c.id).unwrap().state, "quarantined");
    }
    #[test]
    fn directory_tolerance_is_fixed_to_2050_and_only_written_field() {
        let c = fixture_for(RESUME_ID);
        let old = c
            .validate(&c.load(c.id).unwrap(), &source(&c), true)
            .unwrap();
        let mut changed = old.clone();
        changed.get_mut("source/.git").unwrap().written += 1;
        assert!(same_inventory(RESUME_ID, &old, &changed));
        assert!(!same_inventory(ID, &old, &changed));
        for field in 0..6 {
            let mut changed = old.clone();
            let n = changed.get_mut("source/.git").unwrap();
            match field {
                0 => n.volume += 1,
                1 => n.file_id += 1,
                2 => n.size += 1,
                3 => n.links += 1,
                4 => n.attributes ^= 2,
                _ => n.attributes &= !0x10,
            }
            assert!(!same_inventory(RESUME_ID, &old, &changed));
        }
        let mut changed = old.clone();
        changed.get_mut("source/.git/HEAD").unwrap().written += 1;
        assert!(!same_inventory(RESUME_ID, &old, &changed));
        changed = old.clone();
        changed.remove("source/.git/HEAD");
        assert!(!same_inventory(RESUME_ID, &old, &changed));
        let c = fixture();
        c.plan(c.id).unwrap();
        drift_directory(&source(&c).join("kcaldav/.git"));
        assert!(matches!(c.plan(c.id), Err("RECOVERY_RECORD_CHANGED")));
    }
    #[test]
    fn resumed_plan_rejects_new_nodes_changed_files_and_external_links() {
        for mutation in 0..4 {
            let c = fixture_for(RESUME_ID);
            c.plan(c.id).unwrap();
            let target = source(&c).join("source/.git");
            drift_directory(&target);
            match mutation {
                0 => fs::write(target.join("unexpected"), b"new").unwrap(),
                1 => {
                    let p = target.join("HEAD");
                    let stamp =
                        fs::metadata(&p).unwrap().modified().unwrap() + Duration::from_secs(30);
                    fs::File::options()
                        .write(true)
                        .open(&p)
                        .unwrap()
                        .set_times(fs::FileTimes::new().set_modified(stamp))
                        .unwrap();
                }
                2 => fs::hard_link(target.join("HEAD"), c.base.join("external-link")).unwrap(),
                _ => {
                    fs::remove_file(target.join("description")).unwrap();
                }
            }
            let expected = if mutation == 2 {
                "HARD_LINK"
            } else {
                "RECOVERY_RECORD_CHANGED"
            };
            assert!(matches!(c.plan(c.id), Err(code) if code == expected));
            assert_eq!(c.load(c.id).unwrap().state, "owned");
            assert!(!c.destination().join("files").exists());
        }
    }
    #[test]
    fn content_proof_rejects_same_metadata_content_change_before_and_after_move() {
        for moved in [false, true] {
            let c = fixture_for(RESUME_ID);
            let output = source(&c).join("output");
            fs::write(&output, b"aaaa").unwrap();
            c.plan(c.id).unwrap();
            let output = if moved {
                fs::rename(source(&c), c.destination().join("files")).unwrap();
                c.destination().join("files/output")
            } else {
                output
            };
            let old = node(&output).unwrap();
            let modified = fs::metadata(&output).unwrap().modified().unwrap();
            fs::write(&output, b"bbbb").unwrap();
            fs::File::options()
                .write(true)
                .open(&output)
                .unwrap()
                .set_times(fs::FileTimes::new().set_modified(modified))
                .unwrap();
            assert_eq!(node(&output).unwrap(), old);
            let result = if moved { c.finish(c.id) } else { c.plan(c.id) };
            assert!(
                matches!(result, Err("RECOVERY_RECORD_CHANGED")),
                "unexpected result: {result:?}"
            );
            assert_eq!(c.load(c.id).unwrap().state, "owned");
        }
    }
    #[test]
    fn invalid_resume_proof_never_reissues_move_or_finishes() {
        for moved in [false, true] {
            let c = fixture_for(RESUME_ID);
            c.plan(c.id).unwrap();
            fs::write(c.proof_path(), b"invalid proof").unwrap();
            if moved {
                fs::rename(source(&c), c.destination().join("files")).unwrap();
            }
            let result = if moved { c.finish(c.id) } else { c.plan(c.id) };
            assert!(matches!(result, Err("RECOVERY_RECORD_CHANGED")));
            assert_eq!(c.load(c.id).unwrap().state, "owned");
        }
    }
    #[test]
    fn missing_proof_and_changed_ticket_cannot_finish_a_2050_move() {
        let c = fixture_for(RESUME_ID);
        c.plan(c.id).unwrap();
        fs::remove_file(c.proof_path()).unwrap();
        fs::rename(source(&c), c.destination().join("files")).unwrap();
        assert!(matches!(c.finish(c.id), Err("RESUME_PROOF_MISSING")));
        assert_eq!(c.load(c.id).unwrap().state, "owned");
        let c = fixture_for(RESUME_ID);
        c.plan(c.id).unwrap();
        let mut ticket: Ticket =
            serde_json::from_slice(&fs::read(c.ticket_path()).unwrap()).unwrap();
        ticket.nodes.get_mut("source/.git").unwrap().written += 1;
        fs::write(c.ticket_path(), serde_json::to_vec_pretty(&ticket).unwrap()).unwrap();
        fs::rename(source(&c), c.destination().join("files")).unwrap();
        assert!(matches!(c.finish(c.id), Err("RECOVERY_RECORD_CHANGED")));
        assert_eq!(c.load(c.id).unwrap().state, "owned");
    }
    #[test]
    fn proof_publish_resumes_past_unpublished_partial_and_never_overwrites() {
        let c = fixture_for(RESUME_ID);
        let partial = c
            .proof_path()
            .with_extension("proof-writing-prior-interrupted");
        fs::write(&partial, b"partial").unwrap();
        publish_proof(&c.proof_path(), b"complete").unwrap();
        assert_eq!(fs::read(&partial).unwrap(), b"partial");
        assert_eq!(fs::read(c.proof_path()).unwrap(), b"complete");
        publish_proof(&c.proof_path(), b"complete").unwrap();
        assert!(matches!(
            publish_proof(&c.proof_path(), b"different"),
            Err("RECOVERY_RECORD_CHANGED")
        ));
        assert_eq!(fs::read(c.proof_path()).unwrap(), b"complete");
    }
    #[test]
    fn linked_worktree_move_repairs_only_its_backlink_and_resumes_after_repair() {
        let c = fixture_for("38e3fbc1767b66eef9127f2a27567c8a");
        let (admin, common) = match &c.repos[0].kind {
            RepoKind::Linked { admin, common } => (admin, common),
            _ => unreachable!(),
        };
        let refs = git(common.parent().unwrap(), &["show-ref"]).unwrap();
        let first = c.plan(c.id).unwrap();
        assert_eq!(first, c.plan(c.id).unwrap());
        let ticket: Ticket = serde_json::from_slice(&fs::read(c.ticket_path()).unwrap()).unwrap();
        git(
            common.parent().unwrap(),
            &["update-ref", "-d", c.repos[0].saved_ref.unwrap()],
        )
        .unwrap();
        let refs_after_prune = git(common.parent().unwrap(), &["show-ref"]).unwrap();
        assert_ne!(refs_after_prune, refs);
        fs::rename(source(&c), c.destination().join("files")).unwrap();
        // Model interruption immediately after the atomic backlink commit, before receipt commit.
        c.repair_backlink(
            &c.load(c.id).unwrap(),
            &c.destination().join("files"),
            ticket.backlink.as_ref(),
        )
        .unwrap();
        c.finish(c.id).unwrap();
        c.finish(c.id).unwrap();
        assert!(same(
            Path::new(&bounded_text(&admin.join("gitdir")).unwrap()),
            &c.destination().join("files/work/.git")
        ));
        assert_eq!(
            git(common.parent().unwrap(), &["show-ref"]).unwrap(),
            refs_after_prune
        );
        assert!(!present(&admin.join("gitdir.todo2052-repair")).unwrap());
        assert!(
            git(
                &c.destination().join("files/work"),
                &["status", "--porcelain=v1"]
            )
            .unwrap()
            .is_empty()
        );
    }
    #[test]
    fn local_verification_worktree_preserves_repro_and_internal_hardlinks() {
        let c = fixture_for("cfee3edefd3704d67c140ca6952dd7ce");
        let root = source(&c);
        fs::create_dir(root.join("repro")).unwrap();
        fs::write(root.join("repro/m1.rs"), b"preserved regression input").unwrap();
        fs::write(
            root.join("fixed/dependency-snapshot.txt"),
            b"copied dependency",
        )
        .unwrap();
        fs::create_dir(root.join("target")).unwrap();
        let artifact = root.join("target/test.exe");
        fs::write(&artifact, b"test artifact").unwrap();
        fs::hard_link(&artifact, root.join("target/test-copy.exe")).unwrap();
        let common = match &c.repos[0].kind {
            RepoKind::Linked { common, .. } => common,
            _ => unreachable!(),
        };
        let refs = git(common.parent().unwrap(), &["show-ref"]).unwrap();
        c.plan(c.id).unwrap();
        fs::rename(&root, c.destination().join("files")).unwrap();
        let result = c.finish(c.id).unwrap();
        assert_eq!(result["state"], "quarantined");
        let moved = c.destination().join("files");
        assert_eq!(
            fs::read(moved.join("repro/m1.rs")).unwrap(),
            b"preserved regression input"
        );
        assert_eq!(
            fs::read(moved.join("fixed/dependency-snapshot.txt")).unwrap(),
            b"copied dependency"
        );
        assert_eq!(node(&moved.join("target/test.exe")).unwrap().links, 2);
        assert_eq!(
            git(&moved.join("fixed"), &["rev-parse", "HEAD"]).unwrap(),
            c.repos[0].head
        );
        assert_eq!(git(common.parent().unwrap(), &["show-ref"]).unwrap(), refs);
        c.finish(c.id).unwrap();
    }
    #[test]
    fn local_verification_worktree_rejects_remotes_and_tracked_edits() {
        let c = fixture_for("cfee3edefd3704d67c140ca6952dd7ce");
        git(
            &source(&c).join("fixed"),
            &[
                "remote",
                "add",
                "origin",
                "https://example.invalid/unexpected",
            ],
        )
        .unwrap();
        assert!(matches!(c.plan(c.id), Err("GIT_ORIGIN_CHANGED")));
        assert!(!c.destination().exists());
        let c = fixture_for("cfee3edefd3704d67c140ca6952dd7ce");
        fs::write(source(&c).join("fixed/source.txt"), b"unsaved change").unwrap();
        assert!(matches!(c.plan(c.id), Err("GIT_TRACKED_CHANGES")));
        assert!(!c.destination().exists());
    }
    #[test]
    fn linked_worktree_backlink_tampering_keeps_receipt_owned() {
        let c = fixture_for("38e3fbc1767b66eef9127f2a27567c8a");
        c.plan(c.id).unwrap();
        let admin = match &c.repos[0].kind {
            RepoKind::Linked { admin, .. } => admin,
            _ => unreachable!(),
        };
        fs::write(admin.join("gitdir"), b"C:/someone-else/.git\n").unwrap();
        fs::rename(source(&c), c.destination().join("files")).unwrap();
        assert!(matches!(c.finish(c.id), Err("WORKTREE_BACKLINK_CHANGED")));
        assert_eq!(c.load(c.id).unwrap().state, "owned");
    }
    #[test]
    fn long_path_inventory_survives_the_longer_quarantine_prefix() {
        let c = fixture();
        let relative = PathBuf::from("kcaldav")
            .join("a".repeat(100))
            .join("b".repeat(100));
        let path = source(&c).join(&relative);
        fs::create_dir_all(&path).unwrap();
        fs::write(path.join("proof.txt"), b"long path fixture").unwrap();
        assert!(path.as_os_str().encode_wide().count() > 260);
        c.plan(ID).unwrap();
        fs::rename(source(&c), c.destination().join("files")).unwrap();
        c.finish(ID).unwrap();
        assert!(
            c.destination()
                .join("files")
                .join(relative)
                .join("proof.txt")
                .is_file()
        );
    }
    #[test]
    fn extended_metadata_paths_handle_drive_unc_and_existing_prefix() {
        let decode = |s| String::from_utf16(&extended_path(Path::new(s)).unwrap()).unwrap();
        assert_eq!(decode("C:/trial/name"), "\\\\?\\C:\\trial\\name\0");
        assert_eq!(
            decode(r"\\server\share\trial"),
            "\\\\?\\UNC\\server\\share\\trial\0"
        );
        assert_eq!(decode(r"\\?\C:\trial"), "\\\\?\\C:\\trial\0");
        assert!(matches!(
            extended_path(Path::new("relative")),
            Err("INVALID_PATH")
        ));
    }
}
