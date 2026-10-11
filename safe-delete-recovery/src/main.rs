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
    process::{Command, ExitStatus, Stdio},
    time::{Duration, Instant},
};

type Result<T> = std::result::Result<T, &'static str>;
const ID: &str = "dee85bb67fa66449ba0330f0fbe049f1";
const CARGO_ID: &str = "c058c98f1288073adbd10236900199d1";
const ADAPTIVE_ID: &str = "b6ef1394c70c4f11ee5ac370ad82886e";
const QUEUE_ID: &str = "9f836113c154d11da358e0cef15db869";
const REBOOT_ID: &str = "464577ad28cdf40461bb04a301f7e8e7";
const BOARD_ID: &str = "67feb7811b6d0a94c98465c784b64f45";
const BOARD_CREATED: &str = "2026-10-08T18:04:44.1608254+09:00";
const CHROME_ID: &str = "e87d9c55880dce0cfd7bb1d3fe3ca85e";
const CHROME_CREATED: &str = "2026-10-10T21:38:58.2660427+09:00";
const DECISION_ID: &str = "50189b66c7f196bf436305cb29f91b42";
const DECISION_CREATED: &str = "2026-10-11T12:02:44.5926082+09:00";
const INTERNAL_2184_ID: &str = "4b043a897799a7d7df9a0134cc464c7a";
const QUEUE_POINTERS: [&str; 2] = [
    "todo-note-tool/.git",
    "repo/.git/worktrees/todo-note-tool/gitdir",
];
const REBOOT_REPORT_SHA256: &str =
    "5b87f8926728c7e692e0ca14d8e02655a2f25474d85fee998eb9c756afd5a5e1";
const RESUME_ID: &str = "438ee7d343a6c84558201e2778d2c890";
const LIMIT: usize = 10000;
const REPARSE: u32 = 0x400;
mod scoopa;
mod unslop;

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

// Only the two pointers belonging to the fixed, entirely owned ToDo2184
// clone/worktree pair can be repaired. Original bytes remain in this record.
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct InternalPointer {
    original: Vec<u8>,
    replacement: Node,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct InternalRepair {
    ticket_sha256: String,
    pointers: [InternalPointer; 2],
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
    saved_merge: Option<String>,
    squash_tree: Option<String>,
    allowed_untracked: &'static [&'static str],
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
    pinned_identity: Option<(u32, u64)>,
    project_alias_target: Option<PathBuf>,
    project_alias_preservation_enabled: bool,
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
                    saved_merge: None,
                    squash_tree: None,
                    allowed_untracked: &[],
                    kind: RepoKind::Clone,
                },
                Repo {
                    name: "plane-jp",
                    origin: "https://github.com/katsushi2441/plane-jp.git",
                    head: "1c5078295bc8a33966d0b22ee77cd0194f49e1dc".into(),
                    saved_ref: None,
                    saved_merge: None,
                    squash_tree: None,
                    allowed_untracked: &[],
                    kind: RepoKind::Clone,
                },
            ],
            id: ID,
            project: "todo-2001",
            content: "tool-verification",
            pinned_identity: None,
            project_alias_target: None,
            project_alias_preservation_enabled: false,
        };
        let public = "https://github.com/AutoDevJapan/cc-pc-tools-rs.git";
        let clone = |name, origin, head: &str, saved_ref| Repo {
            name,
            origin,
            head: head.into(),
            saved_ref,
            saved_merge: None,
            squash_tree: None,
            allowed_untracked: &[],
            kind: RepoKind::Clone,
        };
        match id {
            DECISION_ID => {
                c.id = DECISION_ID;
                c.project = "cc-ask";
                c.content = "decision-dedup";
                c.store = PathBuf::from("D:/CC/Tools/safe-delete/receipts-go");
                c.temp = PathBuf::from("C:/Users/makhmeto/AppData/Local/Temp");
                c.pinned_identity = Some((2229119083, 39969446694675659));
                c.repos = vec![Repo {
                    name: "repo",
                    origin: public,
                    head: "94ab1cbec9a35e131ce1a2f135984cc07dca2505".into(),
                    saved_ref: Some("refs/remotes/origin/fix/todo2382-question-associations"),
                    saved_merge: None,
                    squash_tree: None,
                    allowed_untracked: &[],
                    kind: RepoKind::Linked {
                        admin: PathBuf::from(
                            "C:/Users/makhmeto/Desktop/dev/ADJ/cc-pc-tools-rs/.git/worktrees/repo",
                        ),
                        common: PathBuf::from(
                            "C:/Users/makhmeto/Desktop/dev/ADJ/cc-pc-tools-rs/.git",
                        ),
                    },
                }];
            }
            BOARD_ID => {
                c.id = BOARD_ID;
                c.project = "cc-board";
                c.content = "parent-post";
                c.store = PathBuf::from("D:/CC/Tools/safe-delete/receipts-go");
                c.temp = PathBuf::from("C:/Users/makhmeto/AppData/Local/Temp");
                c.pinned_identity = Some((2229119083, 2251799828682344));
                let head = "759f74fb8a1db87308feac491d982b18cde62382";
                c.repos = vec![Repo {
                    name: "work",
                    origin: public,
                    head: head.into(),
                    saved_ref: Some("refs/remotes/origin/main"),
                    // This deployed integration commit is already in main.
                    saved_merge: Some(head.into()),
                    squash_tree: None,
                    allowed_untracked: &[],
                    kind: RepoKind::Linked {
                        admin: PathBuf::from(
                            "D:/CC/Worktrees/cc-pc-tools-rs-chatgpt-web/.git/worktrees/work8",
                        ),
                        common: PathBuf::from("D:/CC/Worktrees/cc-pc-tools-rs-chatgpt-web/.git"),
                    },
                }];
            }
            QUEUE_ID => {
                c.id = QUEUE_ID;
                c.project = "cc-pc-tools-rs";
                c.content = "queue-loop-tier";
                c.store = PathBuf::from("D:/CC/Tools/safe-delete/receipts-go");
                c.temp = PathBuf::from("C:/Users/makhmeto/AppData/Local/Temp");
                c.pinned_identity = Some((2229119083, 23362423072822789));
                c.repos = vec![clone(
                    "repo",
                    public,
                    "80c4bb370fce4e7645fafd8ac3eda1422168626b",
                    Some("refs/remotes/origin/feat/queue-loop-tier-1967"),
                )];
            }
            id if unslop::profile(id).is_some() => unslop::configure(&mut c, id)?,
            scoopa::ID => {
                c.id = scoopa::ID;
                c.store = PathBuf::from("D:/CC/Tools/safe-delete/receipts-go");
                c.project = "scoopa-1242-ui";
                c.content = "review-and-validation";
                c.temp = PathBuf::from(r"C:\Users\makhmeto\AppData\Local\Temp");
                c.pinned_identity = Some((2229119083, 9851624200190911));
                c.repos.clear();
                c.project_alias_target = Some(PathBuf::from(scoopa::PROJECT_TARGET));
                // Preserve the owned link itself; its target is neither opened nor owned.
                c.project_alias_preservation_enabled = true;
            }
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
                        saved_merge: None,
                        squash_tree: None,
                        allowed_untracked: &[],
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
                    saved_merge: None,
                    squash_tree: None,
                    allowed_untracked: &[],
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
            CARGO_ID => {
                c.id = CARGO_ID;
                c.project = "cc-jobs";
                c.content = "dynamic-cargo-gate";
                c.store = PathBuf::from("D:/CC/Tools/safe-delete/receipts-go");
                c.temp = PathBuf::from("C:/Users/makhmeto/AppData/Local/Temp");
                c.pinned_identity = Some((2229119083, 2814749782464451));
                c.repos = vec![Repo {
                    name: "work",
                    origin: public,
                    head: "e5b9fb2858b0fc4357dcb9e3745a2ddaa1240801".into(),
                    saved_ref: Some("refs/remotes/origin/fix/dynamic-cargo-gate-2064"),
                    allowed_untracked: &[],
                    saved_merge: Some("2a676778c27f9225060dfaf5758b0cdfe8ef94c9".into()),
                    squash_tree: None,
                    kind: RepoKind::Linked {
                        admin: PathBuf::from(
                            "D:/CC/Worktrees/cc-pc-tools-rs-chatgpt-web/.git/worktrees/work9",
                        ),
                        common: PathBuf::from("D:/CC/Worktrees/cc-pc-tools-rs-chatgpt-web/.git"),
                    },
                }];
            }
            ADAPTIVE_ID => {
                c.id = ADAPTIVE_ID;
                c.project = "cc-jobs";
                c.content = "adaptive-concurrency";
                c.store = PathBuf::from("D:/CC/Tools/safe-delete/receipts-go");
                c.temp = PathBuf::from("C:/Users/makhmeto/AppData/Local/Temp");
                c.pinned_identity = Some((2229119083, 6473924479268101));
                c.repos = vec![Repo {
                    name: "work",
                    origin: public,
                    head: "0ac365ab478efa36206c968400e48ac5150abf1b".into(),
                    saved_ref: Some("refs/remotes/origin/fix/adaptive-account-windows"),
                    saved_merge: None,
                    squash_tree: None,
                    allowed_untracked: &[],
                    kind: RepoKind::Linked {
                        admin: PathBuf::from(
                            "D:/CC/Worktrees/cc-pc-tools-rs-chatgpt-web/.git/worktrees/work7",
                        ),
                        common: PathBuf::from("D:/CC/Worktrees/cc-pc-tools-rs-chatgpt-web/.git"),
                    },
                }];
            }
            REBOOT_ID => {
                c.id = REBOOT_ID;
                c.project = "cc-jobs";
                c.content = "reboot-recovery";
                c.store = PathBuf::from("D:/CC/Tools/safe-delete/receipts-go");
                c.temp = PathBuf::from("C:/Users/makhmeto/AppData/Local/Temp");
                c.pinned_identity = Some((2229119083, 13229323920896790));
                c.repos = vec![Repo {
                    name: "work",
                    origin: public,
                    head: "fa80a74f15b50c0ba0cf3865a19da53e2c23a00b".into(),
                    saved_ref: Some("refs/remotes/origin/fix/reboot-job-recovery"),
                    saved_merge: None,
                    squash_tree: None,
                    allowed_untracked: &[],
                    kind: RepoKind::Linked {
                        admin: PathBuf::from(
                            "D:/CC/Worktrees/cc-pc-tools-rs-chatgpt-web/.git/worktrees/work10",
                        ),
                        common: PathBuf::from("D:/CC/Worktrees/cc-pc-tools-rs-chatgpt-web/.git"),
                    },
                }];
            }
            "75cd71384cb094e47b19cf5d39b5b025" => {
                c.id = "75cd71384cb094e47b19cf5d39b5b025";
                c.project = "cc-jobs";
                c.content = "provider-routing-review";
                c.repos = vec![Repo {
                    name: "worktree",
                    origin: public,
                    head: "70a48f412331b232c3cebac99bd36bb34f9cc41f".into(),
                    saved_ref: Some("refs/remotes/origin/archive/claude-routing-2063"),
                    saved_merge: None,
                    squash_tree: None,
                    allowed_untracked: &[
                        ".godd/state/registry/314190d2cf653c778c45b8d8e50f829f.json",
                        ".godd/state/registry/314190d2cf653c778c45b8d8e50f829f.json.flock",
                    ],
                    kind: RepoKind::Linked {
                        admin: PathBuf::from(
                            "C:/Users/makhmeto/Desktop/dev/ADJ/cc-pc-tools-rs/.git/worktrees/worktree2",
                        ),
                        common: PathBuf::from(
                            "C:/Users/makhmeto/Desktop/dev/ADJ/cc-pc-tools-rs/.git",
                        ),
                    },
                }];
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
                    saved_merge: None,
                    squash_tree: None,
                    allowed_untracked: &[],
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
            INTERNAL_2184_ID => {
                c.id = INTERNAL_2184_ID;
                c.project = "cc-jobs-2184";
                c.content = "git-fix-and-verification";
                c.store = PathBuf::from("D:/CC/Tools/safe-delete/receipts-go");
                c.temp = PathBuf::from("C:/Users/makhmeto/AppData/Local/Temp");
                c.pinned_identity = Some((2229119083, 5348024573249873));
                let root = c.temp.join(format!("codex-owned-{INTERNAL_2184_ID}"));
                c.repos = vec![
                    Repo {
                        allowed_untracked: &[
                            ".godd/state/registry/314190d2cf653c778c45b8d8e50f829f.json",
                            ".godd/state/registry/314190d2cf653c778c45b8d8e50f829f.json.flock",
                        ],
                        ..clone(
                            "repo",
                            public,
                            "5b10ae4f0c0a55ee234813fc01e8aae8b6f7b025",
                            Some("refs/remotes/origin/fix/todo2184-queue-bootstrap"),
                        )
                    },
                    Repo {
                        kind: RepoKind::Linked {
                            admin: root.join("repo/.git/worktrees/todo-note-tool"),
                            common: root.join("repo/.git"),
                        },
                        ..clone(
                            "todo-note-tool",
                            public,
                            "fb62c76e74ac782b1b01a0539b332a8535c4907e",
                            Some("refs/remotes/origin/fix/todo-2172-resume"),
                        )
                    },
                ];
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
        if self.id != RESUME_ID && self.id != INTERNAL_2184_ID && self.id != BOARD_ID {
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
        if self.id == DECISION_ID {
            return self
                .quarantine
                .join(format!("20261011-cc-ask-2382-{DECISION_ID}"));
        }
        if self.id == INTERNAL_2184_ID {
            return self
                .quarantine
                .join(format!("20261010-cc-jobs-2184-{INTERNAL_2184_ID}"));
        }
        if unslop::profile(self.id).is_some() {
            return self.quarantine.join(format!(
                "20261010-{}-{}-{}",
                self.project, self.content, self.id
            ));
        }
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
            || (self.id == INTERNAL_2184_ID
                && r.identity.created_at != "2026-10-10T00:39:58.8390892+09:00")
            || (self.id == BOARD_ID && r.identity.created_at != BOARD_CREATED)
            || (self.id == CHROME_ID && r.identity.created_at != CHROME_CREATED)
            || (self.id == DECISION_ID && r.identity.created_at != DECISION_CREATED)
            || !same(Path::new(&r.identity.root), &self.temp)
            || !same(Path::new(&r.identity.path), &expected)
            || self.pinned_identity.is_some_and(|(volume, file_id)| {
                r.identity.volume != volume || r.identity.file_id != file_id
            })
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
        if self.id == INTERNAL_2184_ID {
            self.validate_internal_repos(path, before_move)?;
            return Ok(nodes);
        }
        for repo in &self.repos {
            self.client_guard(&path.join(repo.name))?;
            if let RepoKind::Linked { admin, common } = &repo.kind {
                self.client_guard(admin)?;
                self.client_guard(common)?;
            }
            validate_repo_scoped(
                &path.join(repo.name),
                repo,
                before_move,
                false,
                false,
                self.id == BOARD_ID,
            )?;
        }
        Ok(nodes)
    }
    fn plan(&self, id: &str) -> Result<serde_json::Value> {
        if unslop::profile(self.id).is_some() {
            return unslop::plan(self, id);
        }
        if self.id == scoopa::ID {
            return scoopa::plan(self, id);
        }
        let r = self.load(id)?;
        if r.state != "owned" {
            return Err("RECEIPT_NOT_OWNED");
        }
        let path = Path::new(&r.identity.path);
        let dest = self.destination();
        // Git's -C/discovery runs before core.longpaths is applied. Its root
        // must fit the startup limit even though internal object paths may not.
        for repo in &self.repos {
            if !matches!(repo.kind, RepoKind::OpaqueBare)
                && dest
                    .join("files")
                    .join(repo.name)
                    .as_os_str()
                    .encode_wide()
                    .count()
                    > 240
            {
                return Err("GIT_DESTINATION_ROOT_TOO_LONG");
            }
        }
        let nodes = self.validate(&r, path, true)?;
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
            backlink: if self.id == INTERNAL_2184_ID {
                None
            } else {
                self.prepare_backlink(path, &dest.join("files"))?
            },
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
            let name = entry.map_err(|_| "QUARANTINE_METADATA_FAILED")?.file_name();
            if name != OsStr::new("理由.txt")
                && !(self.id == INTERNAL_2184_ID
                    && (name == OsStr::new("git-pointer-0.replacement")
                        || name == OsStr::new("git-pointer-1.replacement")))
            {
                return Err("QUARANTINE_DESTINATION_NOT_EMPTY");
            }
        }
        write_or_verify(&dest.join("理由.txt"), why.as_bytes(), 8192)?;
        if self.id == INTERNAL_2184_ID {
            self.prepare_internal_repair(path, &ticket_bytes)?;
        }
        if self.id == RESUME_ID || self.id == INTERNAL_2184_ID || self.id == BOARD_ID {
            self.content_proof(path, &nodes, &ticket_bytes, true)?;
        }
        Ok(
            serde_json::json!({"id": self.id, "action": "submit_quarantine_to_exec_tool", "bytes": bytes,
            "entries": ticket.nodes.len(), "destination": dest, "reason": "owned_public_download_git",
            "powershell_command": format!("Move-Item -LiteralPath {} -Destination {} -ErrorAction Stop", quote(path), quote(&dest.join("files"))) }),
        )
    }
    /// The task wrote its final public PR description after a read-only
    /// preflight. Permit only that fixed addition before any source movement;
    /// archive the old intent rather than replacing or discarding its evidence.
    fn renew_reboot_preflight(&self, id: &str, report_hash: &str) -> Result<serde_json::Value> {
        if self.id != REBOOT_ID || id != REBOOT_ID {
            return Err("SCOPE_NOT_AUTHORIZED");
        }
        let r = self.load(id)?;
        let destination = self.destination();
        if r.state != "owned" || present(&destination.join("files"))? {
            return Err("MOVEMENT_ALREADY_STARTED");
        }
        let archived = self.ticket_path().with_extension("pre-final-report.json");
        let ticket_archived = present(&archived)?;
        let ticket_path = self.ticket_path();
        let saved = read_record(
            if ticket_archived {
                &archived
            } else {
                &ticket_path
            },
            8 * 1024 * 1024,
        )?;
        let old: Ticket = serde_json::from_slice(&saved).map_err(|_| "INVALID_TICKET")?;
        if old.id != id
            || old.source != r.identity.path
            || old.destination != destination.to_string_lossy()
        {
            return Err("INVALID_TICKET");
        }
        let source = Path::new(&r.identity.path);
        let nodes = self.validate(&r, source, true)?;
        let mut comparable = nodes.clone();
        if old.nodes.contains_key("pr-body-final.txt")
            || comparable.remove("pr-body-final.txt").is_none()
        {
            return Err("UNEXPECTED_PREFLIGHT_CHANGE");
        }
        let report = read_record(&source.join("pr-body-final.txt"), 64 * 1024)?;
        if format!("{:x}", Sha256::digest(&report)) != report_hash {
            return Err("UNEXPECTED_PREFLIGHT_CHANGE");
        }
        // Adding the one file changes only the containing root's write time.
        // Every original file, directory, alias and identity remains exact.
        comparable.get_mut("").ok_or("INVALID_TICKET")?.written =
            old.nodes.get("").ok_or("INVALID_TICKET")?.written;
        if comparable != old.nodes {
            return Err("UNEXPECTED_PREFLIGHT_CHANGE");
        }
        let current_backlink = self.prepare_backlink(source, &destination.join("files"))?;
        match (&old.backlink, &current_backlink) {
            (None, None) => {}
            (Some(old), Some(now))
                if old.original == now.original
                    && old.replacement == now.replacement
                    && old.admin.volume == now.admin.volume
                    && old.admin.file_id == now.admin.file_id
                    && old.common.volume == now.common.volume
                    && old.common.file_id == now.common.file_id => {}
            _ => return Err("INVALID_TICKET"),
        }
        let replacement = Ticket {
            id: id.into(),
            source: r.identity.path.clone(),
            destination: old.destination,
            nodes,
            backlink: old.backlink,
        };
        let replacement_bytes =
            serde_json::to_vec_pretty(&replacement).map_err(|_| "TICKET_JSON")?;
        let replacement_published = ticket_archived && present(&self.ticket_path())?;
        if replacement_published {
            write_or_verify(&self.ticket_path(), &replacement_bytes, 8 * 1024 * 1024)?;
        }
        let reason = destination.join("理由.txt");
        let archived_reason = destination.with_extension("pre-final-report.reason.txt");
        let reason_archived = present(&archived_reason)?;
        if reason_archived && !ticket_archived {
            return Err("INVALID_PREFLIGHT_REASON");
        }
        let old_reason = if reason_archived {
            &archived_reason
        } else {
            &reason
        };
        checked(old_reason)?;
        let reason_node = node(old_reason)?;
        if reason_node.attributes & (REPARSE | 0x10) != 0 || reason_node.links != 1 {
            return Err("INVALID_PREFLIGHT_REASON");
        }
        let old_bytes: u64 = old
            .nodes
            .values()
            .filter(|n| n.attributes & 0x10 == 0)
            .map(|n| n.size)
            .sum();
        let expected_reason = format!(
            "退避状態: 未完了（移動前）。\n元の場所: {}\n大きさ: {old_bytes} byte\n理由: 所有一時データのGit保護による退避停止。\n仕様に固定した取得物・成果保存・リンクを確認し、Gitを含む全内容を保持する。\n所有ID: {}\n",
            r.identity.path, self.id
        );
        if read_record(old_reason, 64 * 1024)? != expected_reason.as_bytes() {
            return Err("INVALID_PREFLIGHT_REASON");
        }
        if reason_archived && present(&reason)? && !replacement_published {
            return Err("INVALID_PREFLIGHT_REASON");
        }
        if self.validate(&r, source, true)? != replacement.nodes {
            return Err("SOURCE_CHANGED_DURING_PREFLIGHT");
        }
        if !ticket_archived {
            fs::rename(self.ticket_path(), &archived).map_err(|_| "PREFLIGHT_ARCHIVE_FAILED")?;
        }
        if !reason_archived {
            fs::rename(&reason, &archived_reason).map_err(|_| "PREFLIGHT_REASON_ARCHIVE_FAILED")?;
        }
        publish_proof(&self.ticket_path(), &replacement_bytes)?;
        self.plan(id)
    }
    fn finish(&self, id: &str) -> Result<serde_json::Value> {
        if unslop::profile(self.id).is_some() {
            return unslop::finish(self, id);
        }
        if self.id == scoopa::ID {
            return scoopa::finish(self, id);
        }
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
        let nodes = if self.id == INTERNAL_2184_ID {
            self.finish_internal_repair(&r, &files, &t, &ticket_bytes)?
        } else {
            self.validate(&r, &files, false)?
        };
        let has_proof = self.id == RESUME_ID || self.id == BOARD_ID;
        if (has_proof && !same_inventory(self.id, &t.nodes, &nodes))
            || (!has_proof && nodes != t.nodes)
        {
            return Err("CONTENT_CHANGED_AFTER_PLAN");
        }
        if has_proof {
            self.content_proof(&files, &nodes, &ticket_bytes, false)?;
        }
        if self.id != INTERNAL_2184_ID {
            self.repair_backlink(&r, &files, t.backlink.as_ref())?;
        }
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
    fn validate_internal_repos(&self, root: &Path, before: bool) -> Result<()> {
        if self.id != INTERNAL_2184_ID || self.repos.len() != 2 {
            return Err("SCOPE_NOT_AUTHORIZED");
        }
        let common = root.join("repo/.git");
        let admin = common.join("worktrees/todo-note-tool");
        for p in [&common, &admin, &root.join("todo-note-tool")] {
            self.client_guard(p)?;
        }
        // A fixed single worktree, entirely inside the owned tree; no extra
        // worktree, graft, submodule or object store may be referenced.
        let names = fs::read_dir(common.join("worktrees"))
            .map_err(|_| "INVALID_GIT_ADMIN_PATH")?
            .map(|entry| {
                entry
                    .map(|e| e.file_name())
                    .map_err(|_| "INVALID_GIT_ADMIN_PATH")
            })
            .collect::<Result<Vec<_>>>()?;
        if names != vec![OsStr::new("todo-note-tool").to_owned()]
            || present(&common.join("info/grafts"))?
            || !same(
                Path::new(&bounded_text(&admin.join("gitdir"))?),
                &root.join(QUEUE_POINTERS[0]),
            )
        {
            return Err("EXTERNAL_OR_SHARED_GIT_METADATA");
        }
        validate_repo_scoped(
            &root.join("repo"),
            &self.repos[0],
            before,
            false,
            true,
            false,
        )?;
        let linked = Repo {
            name: self.repos[1].name,
            origin: self.repos[1].origin,
            head: self.repos[1].head.clone(),
            saved_ref: self.repos[1].saved_ref,
            saved_merge: None,
            squash_tree: None,
            allowed_untracked: &[],
            kind: RepoKind::Linked { admin, common },
        };
        validate_repo(&root.join(linked.name), &linked, true)
    }

    fn internal_record_path(&self) -> PathBuf {
        self.store.join(format!("{}.internal-repair.json", self.id))
    }
    fn internal_stage(&self, index: usize) -> PathBuf {
        self.destination()
            .join(format!("git-pointer-{index}.replacement"))
    }
    fn internal_text(root: &Path, index: usize) -> Vec<u8> {
        if index == 0 {
            format!(
                "gitdir: {}\n",
                root.join("repo/.git/worktrees/todo-note-tool").display()
            )
            .into_bytes()
        } else {
            format!("{}\n", root.join(QUEUE_POINTERS[0]).display()).into_bytes()
        }
    }
    fn internal_pointer_bytes(&self, root: &Path, index: usize) -> Result<Vec<u8>> {
        if self.id != INTERNAL_2184_ID {
            return Err("SCOPE_NOT_AUTHORIZED");
        }
        let path = root.join(QUEUE_POINTERS[index]);
        checked_admin(path.parent().ok_or("INVALID_PATH")?)?;
        let before = node(&path)?;
        if before.links != 1 || before.size > 8192 || before.attributes & 0x10 != 0 {
            return Err("INVALID_INTERNAL_REPAIR");
        }
        let bytes = fs::read(&path).map_err(|_| "RECOVERY_RECORD_READ")?;
        if node(&path)? != before {
            return Err("CONTENT_CHANGED_DURING_PROOF");
        }
        Ok(bytes)
    }
    fn prepare_internal_repair(&self, source: &Path, ticket: &[u8]) -> Result<()> {
        if self.id != INTERNAL_2184_ID {
            return Err("SCOPE_NOT_AUTHORIZED");
        }
        let mut pointers = Vec::new();
        for index in 0..QUEUE_POINTERS.len() {
            let original = self.internal_pointer_bytes(source, index)?;
            let expected = Self::internal_text(source, index);
            // Git uses '/' in its pointers on this PC; compare their meaning,
            // retaining the original byte sequence without newline conversion.
            let text = std::str::from_utf8(&original)
                .map_err(|_| "EXTERNAL_GITDIR")?
                .trim();
            let expected = std::str::from_utf8(&expected)
                .map_err(|_| "EXTERNAL_GITDIR")?
                .trim();
            let (text, expected) = if index == 0 {
                (
                    text.strip_prefix("gitdir: ").ok_or("EXTERNAL_GITDIR")?,
                    expected.strip_prefix("gitdir: ").ok_or("EXTERNAL_GITDIR")?,
                )
            } else {
                (text, expected)
            };
            if !same(Path::new(text), Path::new(expected)) {
                return Err("EXTERNAL_GITDIR");
            }
            let stage = self.internal_stage(index);
            write_or_verify(
                &stage,
                &Self::internal_text(&self.destination().join("files"), index),
                8192,
            )?;
            pointers.push(InternalPointer {
                original,
                replacement: node(&stage)?,
            });
        }
        let record = InternalRepair {
            ticket_sha256: format!("{:x}", Sha256::digest(ticket)),
            pointers: pointers.try_into().map_err(|_| "INVALID_INTERNAL_REPAIR")?,
        };
        let bytes = serde_json::to_vec_pretty(&record).map_err(|_| "PROOF_JSON")?;
        write_or_verify(&self.internal_record_path(), &bytes, 32768)
    }
    fn verify_internal_inventory(
        &self,
        root: &Path,
        t: &Ticket,
        ticket: &[u8],
        record: &InternalRepair,
    ) -> Result<BTreeMap<String, Node>> {
        checked(root)?;
        self.client_guard(root)?;
        if record.ticket_sha256 != format!("{:x}", Sha256::digest(ticket)) || t.backlink.is_some() {
            return Err("RECOVERY_RECORD_CHANGED");
        }
        let current = scan(root, &self.repos)?;
        let mut expected = t.nodes.clone();
        let proof_bytes = read_record(&self.proof_path(), 8 * 1024 * 1024)?;
        let proof: ResumeProof =
            serde_json::from_slice(&proof_bytes).map_err(|_| "INVALID_INTERNAL_REPAIR")?;
        if proof.ticket_sha256 != record.ticket_sha256 {
            return Err("RECOVERY_RECORD_CHANGED");
        }
        let mut digests = file_digests(root, &current)?;
        for (i, name) in QUEUE_POINTERS.iter().enumerate() {
            let original_node = t.nodes.get(*name).ok_or("INVALID_INTERNAL_REPAIR")?;
            let actual_node = current.get(*name).ok_or("INVALID_INTERNAL_REPAIR")?;
            let pointer = &record.pointers[i];
            let original_hash = format!("{:x}", Sha256::digest(&pointer.original));
            if proof.files.get(*name) != Some(&original_hash) {
                return Err("RECOVERY_RECORD_CHANGED");
            }
            let bytes = self.internal_pointer_bytes(root, i)?;
            if actual_node == original_node && bytes == pointer.original {
                // Still waiting for this pointer's atomic replacement.
            } else if actual_node == &pointer.replacement && bytes == Self::internal_text(root, i) {
                expected.insert(name.to_string(), pointer.replacement.clone());
                let parent = Path::new(name)
                    .parent()
                    .ok_or("INVALID_PATH")?
                    .to_string_lossy()
                    .replace('\\', "/");
                let actual_parent = current.get(&parent).ok_or("INVALID_INTERNAL_REPAIR")?;
                expected
                    .get_mut(&parent)
                    .ok_or("INVALID_INTERNAL_REPAIR")?
                    .written = actual_parent.written;
                digests.insert(name.to_string(), original_hash);
            } else {
                return Err("WORKTREE_BACKLINK_CHANGED");
            }
        }
        if expected != current
            || digests != proof.files
            || scan(root, &self.repos)? != current
            || read_record(&self.proof_path(), 8 * 1024 * 1024)? != proof_bytes
        {
            return Err("CONTENT_CHANGED_AFTER_PLAN");
        }
        Ok(current)
    }
    fn finish_internal_repair(
        &self,
        r: &Receipt,
        root: &Path,
        t: &Ticket,
        ticket: &[u8],
    ) -> Result<BTreeMap<String, Node>> {
        if self.id != INTERNAL_2184_ID {
            return Err("SCOPE_NOT_AUTHORIZED");
        }
        let identity = node(root)?;
        if identity.volume != r.identity.volume || identity.file_id != r.identity.file_id {
            return Err("TARGET_IDENTITY_CHANGED");
        }
        let bytes = read_record(&self.internal_record_path(), 32768)?;
        let record: InternalRepair =
            serde_json::from_slice(&bytes).map_err(|_| "INVALID_INTERNAL_REPAIR")?;
        self.verify_internal_inventory(root, t, ticket, &record)?;
        for (i, name) in QUEUE_POINTERS.iter().enumerate() {
            let target = root.join(name);
            if node(&target)? == record.pointers[i].replacement {
                continue;
            }
            if r.state != "owned" {
                return Err("RECEIPT_NOT_OWNED");
            }
            let stage = self.internal_stage(i);
            if node(&stage)? != record.pointers[i].replacement
                || read_record(&stage, 8192)? != Self::internal_text(root, i)
            {
                return Err("WORKTREE_REPAIR_FILE_CHANGED");
            }
            // Both the stage and target are on C:. Only these fixed pointer
            // files change; no Git repair command traverses the old paths.
            fs::rename(&stage, &target).map_err(|_| "WORKTREE_REPAIR_FAILED")?;
        }
        self.verify_internal_inventory(root, t, ticket, &record)?;
        self.validate(r, root, false)?;
        if read_record(&self.internal_record_path(), 32768)? != bytes {
            return Err("RECOVERY_RECORD_CHANGED");
        }
        // Normal finish commits the existing receipt only after every original
        // byte is accounted for (including the archived original pointers).
        Ok(t.nodes.clone())
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
                validate_repo_scoped(
                    &root.join(repo.name),
                    repo,
                    false,
                    self.id == unslop::INTEGRATION,
                    false,
                    self.id == BOARD_ID,
                )?;
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
    let (output, status) = hidden_status(exe, args)?;
    if !status.success() {
        return Err("VALIDATOR_FAILED");
    }
    Ok(output)
}
fn hidden_status(exe: &Path, args: &[&OsStr]) -> Result<(Vec<u8>, ExitStatus)> {
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
    #[cfg(test)]
    if !status.success() || output.len() > 1024 * 1024 {
        eprintln!(
            "fixture validator {:?}: {status}, output bytes {}",
            exe.file_name(),
            output.len()
        );
    }
    if output.len() > 1024 * 1024 {
        return Err("VALIDATOR_FAILED");
    }
    Ok((output, status))
}
fn git(path: &Path, args: &[&str]) -> Result<String> {
    let (output, status) = git_status(path, args)?;
    if !status.success() {
        return Err("VALIDATOR_FAILED");
    }
    Ok(output)
}
fn git_status(path: &Path, args: &[&str]) -> Result<(String, ExitStatus)> {
    let fixed = [
        "--no-optional-locks",
        "--no-replace-objects",
        "-c",
        "core.longpaths=true",
        "-c",
        "core.fsmonitor=false",
        "-c",
        "core.hooksPath=NUL",
        "-C",
    ];
    let mut argv: Vec<&OsStr> = fixed.iter().map(OsStr::new).collect();
    argv.push(path.as_os_str());
    argv.extend(args.iter().map(OsStr::new));
    let (output, status) = hidden_status(Path::new("git.exe"), &argv)?;
    let output = String::from_utf8(output).map_err(|_| "GIT_OUTPUT")?;
    Ok((output.trim().into(), status))
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
    validate_repo_options(path, repo, before_move, false)
}

// ToDo1940 separately verifies its fixed submodule before archival.
fn validate_repo_options(
    path: &Path,
    repo: &Repo,
    before_move: bool,
    archived_submodule: bool,
) -> Result<()> {
    validate_repo_scoped(path, repo, before_move, archived_submodule, false, false)
}
fn validate_repo_scoped(
    path: &Path,
    repo: &Repo,
    before_move: bool,
    archived_submodule: bool,
    owned_worktree: bool,
    board_checkout_crlf: bool,
) -> Result<()> {
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
                if special == "modules" && (!before_move || archived_submodule) {
                    continue;
                }
                if present(&common.join(special))? {
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
        if owned_worktree && special == "worktrees" {
            continue;
        }
        if archived_submodule && special == "modules" {
            continue;
        }
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
    let mut status_args = Vec::new();
    if board_checkout_crlf {
        // This fixed worktree was checked out with Windows CRLF conversion.
        // Normalize only Git's comparison; archival hashes cover actual bytes.
        status_args.extend(["-c", "core.autocrlf=true"]);
    }
    status_args.extend(["status", "--porcelain=v1", untracked]);
    if archived_submodule {
        status_args.push("--ignore-submodules=all");
    }
    let status = git(path, &status_args)?;
    if !status.lines().all(|line| {
        repo.allowed_untracked
            .iter()
            .any(|name| line == format!("?? {name}"))
    }) {
        return Err("GIT_TRACKED_CHANGES");
    }
    if let Some(saved) = repo.saved_ref.filter(|_| saved_checks) {
        validate_saved_commit(path, repo, saved)?;
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

fn validate_saved_commit(path: &Path, repo: &Repo, saved: &str) -> Result<()> {
    if let Some(tree) = &repo.squash_tree {
        let merge = repo.saved_merge.as_deref().ok_or("GIT_UNPUSHED_COMMITS")?;
        if saved != "refs/remotes/origin/main" {
            return Err("GIT_UNPUSHED_COMMITS");
        }
        if let RepoKind::Linked { common, .. } = &repo.kind
            && present(&common.join("info/grafts"))?
        {
            return Err("EXTERNAL_OR_SHARED_GIT_METADATA");
        }
        // PR98 was squash-merged: preserve the original HEAD and prove its
        // exact tree was published, without claiming HEAD is an ancestor.
        for revision in [
            format!("{}^{{tree}}", repo.head),
            format!("{merge}^{{tree}}"),
        ] {
            if git(path, &["rev-parse", "--verify", &revision])? != *tree {
                return Err("GIT_PUBLISHED_TREE_MISMATCH");
            }
        }
        git(
            path,
            &["rev-parse", "--verify", "refs/remotes/origin/main^{commit}"],
        )
        .map_err(|_| "GIT_UNPUSHED_COMMITS")?;
        return git(path, &["merge-base", "--is-ancestor", merge, saved])
            .map(|_| ())
            .map_err(|_| "GIT_UNPUSHED_COMMITS");
    }
    if repo.saved_merge.is_none() {
        return if git(path, &["rev-parse", saved])? == repo.head {
            Ok(())
        } else {
            Err("GIT_UNPUSHED_COMMITS")
        };
    }
    // Distinguish an absent ref from a failed validator or a changed existing ref.
    if let RepoKind::Linked { common, .. } = &repo.kind
        && present(&common.join("info/grafts"))?
    {
        return Err("EXTERNAL_OR_SHARED_GIT_METADATA");
    }
    if saved == "refs/remotes/origin/main"
        && repo.saved_merge.as_deref() == Some(repo.head.as_str())
    {
        // Only the board profile pins its published integration commit as
        // both HEAD and saved merge. Main may advance, but cannot lose HEAD.
        git(
            path,
            &["rev-parse", "--verify", "refs/remotes/origin/main^{commit}"],
        )
        .map_err(|_| "GIT_UNPUSHED_COMMITS")?;
        return git(path, &["merge-base", "--is-ancestor", &repo.head, saved])
            .map(|_| ())
            .map_err(|_| "GIT_UNPUSHED_COMMITS");
    }
    match git_status(path, &["show-ref", "--exists", saved])?.1.code() {
        Some(0) => {
            return if git(path, &["rev-parse", saved])? == repo.head {
                Ok(())
            } else {
                Err("GIT_UNPUSHED_COMMITS")
            };
        }
        Some(2) => (),
        _ => return Err("GIT_UNPUSHED_COMMITS"),
    }
    let merge = repo.saved_merge.as_deref().ok_or("GIT_UNPUSHED_COMMITS")?;
    // Only the fixed PR56 merge may replace the pruned branch evidence. The
    // target HEAD must reach that merge, which must reach saved origin/main.
    for args in [
        vec!["rev-parse", "--verify", "refs/remotes/origin/main^{commit}"],
        vec!["merge-base", "--is-ancestor", &repo.head, merge],
        vec![
            "merge-base",
            "--is-ancestor",
            merge,
            "refs/remotes/origin/main",
        ],
    ] {
        git(path, &args).map_err(|_| "GIT_UNPUSHED_COMMITS")?;
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
    let info = node_no_follow(path)?;
    if info.attributes & REPARSE != 0 {
        return Err("LINK_OR_REPARSE_POINT");
    }
    Ok(info)
}
// Used only by the fixed Scoopa inventory, which validates symlink reparse data.
fn node_no_follow(path: &Path) -> Result<Node> {
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
        "inspect" if unslop::profile(c.id).is_some() => unslop::inspect(&c, &args[2]),
        "inspect" if c.id == scoopa::ID => scoopa::inspect(&c, &args[2]),
        "plan" => c.plan(&args[2]),
        "renew-plan" if c.id == REBOOT_ID => {
            c.renew_reboot_preflight(&args[2], REBOOT_REPORT_SHA256)
        }
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
    #[test]
    fn decision_archive_is_bound_to_one_owned_worktree_and_its_published_head() {
        let c = Config::production(DECISION_ID).unwrap();
        assert_eq!(c.pinned_identity, Some((2229119083, 39969446694675659)));
        assert_eq!((c.project, c.content), ("cc-ask", "decision-dedup"));
        assert_eq!(c.repos.len(), 1);
        assert_eq!(c.repos[0].head, "94ab1cbec9a35e131ce1a2f135984cc07dca2505");
        assert_eq!(
            c.repos[0].saved_ref,
            Some("refs/remotes/origin/fix/todo2382-question-associations")
        );
        assert!(matches!(c.repos[0].kind, RepoKind::Linked { .. }));
        assert!(c.repos[0].allowed_untracked.is_empty());
        assert!(
            c.destination()
                .ends_with(format!("20261011-cc-ask-2382-{DECISION_ID}"))
        );
    }

    #[test]
    fn decision_archive_preserves_inventory_receipt_and_external_git_backlink() {
        let c = fixture_for(DECISION_ID);
        let old = source(&c);
        let before = scan(&old, &c.repos).unwrap();
        let plan = c.plan(DECISION_ID).unwrap();
        let dest = PathBuf::from(plan["destination"].as_str().unwrap());
        assert!(
            plan["powershell_command"]
                .as_str()
                .unwrap()
                .contains("Move-Item -LiteralPath")
        );
        let files = dest.join("files");
        fs::rename(&old, &files).unwrap();
        c.finish(DECISION_ID).unwrap();
        assert_eq!(scan(&files, &c.repos).unwrap(), before);
        assert!(!old.exists());
        let receipt = c.load(DECISION_ID).unwrap();
        assert_eq!(receipt.state, "quarantined");
        assert_eq!(receipt.destination.as_deref(), dest.to_str());
        assert_eq!(
            git(&files.join("repo"), &["rev-parse", "HEAD"]).unwrap(),
            c.repos[0].head
        );
    }
    #[test]
    fn chrome_squash_and_full_archive_preserve_hidden_files_hardlinks_acl_and_backlink() {
        let c = fixture_for(CHROME_ID);
        let old = source(&c);
        let work = old.join("work");
        assert_eq!(
            git_status(
                &work,
                &[
                    "merge-base",
                    "--is-ancestor",
                    &c.repos[0].head,
                    c.repos[0].saved_merge.as_ref().unwrap()
                ]
            )
            .unwrap()
            .1
            .code(),
            Some(1)
        );
        fs::write(old.join("verification.log"), b"preserved verification\n").unwrap();
        fs::hard_link(
            old.join("verification.log"),
            old.join("verification-alias.log"),
        )
        .unwrap();
        fs::write(old.join("old.exe"), b"preserved old binary\n").unwrap();
        hidden(
            Path::new("attrib.exe"),
            &[OsStr::new("+H"), old.join("old.exe").as_os_str()],
        )
        .unwrap();
        let plan = c.plan(c.id).unwrap();
        assert_eq!(plan["action"], "submit_quarantine_to_exec_tool");
        fs::rename(&old, c.destination().join("files")).unwrap();
        let result = c.finish(c.id).unwrap();
        assert_eq!(result["inventory_verified"], true);
        let files = c.destination().join("files");
        assert_eq!(
            fs::read(files.join("verification.log")).unwrap(),
            b"preserved verification\n"
        );
        assert_eq!(node(&files.join("verification.log")).unwrap().links, 2);
        assert_ne!(node(&files.join("old.exe")).unwrap().attributes & 2, 0);
        let RepoKind::Linked { admin, .. } = &c.repos[0].kind else {
            panic!()
        };
        assert!(same(
            Path::new(&bounded_text(&admin.join("gitdir")).unwrap()),
            &files.join("work/.git")
        ));
        assert!(!old.exists());
        assert_eq!(c.load(c.id).unwrap().state, "quarantined");
        c.finish(c.id).unwrap();
    }

    #[test]
    fn chrome_ref_tree_git_scope_and_receipt_substitutions_do_not_prepare_moves() {
        for failure in [
            "missing-main",
            "unrelated-main",
            "different-tree",
            "dirty",
            "untracked",
            "additional-git",
            "external-hardlink",
            "identity",
            "created",
        ] {
            let mut c = fixture_for(CHROME_ID);
            let old = source(&c);
            let work = old.join("work");
            match failure {
                "missing-main" => {
                    git(&work, &["update-ref", "-d", "refs/remotes/origin/main"]).unwrap();
                }
                "unrelated-main" => {
                    git(
                        &work,
                        &["update-ref", "refs/remotes/origin/main", &c.repos[0].head],
                    )
                    .unwrap();
                }
                "different-tree" => {
                    c.repos[0].squash_tree = Some("0".repeat(40));
                }
                "dirty" => fs::write(work.join("source.txt"), b"changed").unwrap(),
                "untracked" => fs::write(work.join("unpublished.txt"), b"changed").unwrap(),
                "additional-git" => {
                    fs::create_dir(old.join("extra")).unwrap();
                    fs::write(old.join("extra/.git"), b"unapproved").unwrap();
                }
                "external-hardlink" => {
                    fs::write(old.join("record"), b"record").unwrap();
                    fs::hard_link(old.join("record"), c.temp.join("outside-alias")).unwrap();
                }
                "identity" | "created" => {
                    let mut r = c.load(c.id).unwrap();
                    if failure == "identity" {
                        r.identity.file_id += 1;
                    } else {
                        r.identity.created_at = BOARD_CREATED.into();
                    }
                    r.integrity = format!(
                        "{:x}",
                        Sha256::digest(serde_json::to_vec(&r.identity).unwrap())
                    );
                    fs::write(c.receipt_path(), serde_json::to_vec(&r).unwrap()).unwrap();
                }
                _ => unreachable!(),
            }
            assert!(c.plan(c.id).is_err(), "{failure}");
            assert!(!c.destination().exists(), "{failure}");
            assert!(old.exists());
        }
    }

    #[test]
    fn chrome_finish_rejects_same_size_and_timestamp_restored_content_change() {
        let c = fixture_for(CHROME_ID);
        let old = source(&c);
        fs::write(old.join("verification.log"), b"original").unwrap();
        c.plan(c.id).unwrap();
        fs::rename(&old, c.destination().join("files")).unwrap();
        let p = c.destination().join("files/verification.log");
        let old_time = fs::metadata(&p).unwrap().modified().unwrap();
        fs::write(&p, b"modified").unwrap();
        fs::File::options()
            .write(true)
            .open(&p)
            .unwrap()
            .set_times(fs::FileTimes::new().set_modified(old_time))
            .unwrap();
        assert_eq!(c.finish(c.id).unwrap_err(), "CONTENT_CHANGED_AFTER_PLAN");
        assert_eq!(c.load(c.id).unwrap().state, "owned");
    }

    #[test]
    fn board_crlf_checkout_is_archived_verbatim_but_real_edits_are_rejected() {
        for changed in [false, true] {
            let c = fixture_for(BOARD_ID);
            let old = source(&c);
            let tracked = old.join("work/source.txt");
            let content = if changed {
                b"edited fixture\r\n".as_slice()
            } else {
                b"public fixture\r\n".as_slice()
            };
            assert_eq!(fs::read(&tracked).unwrap(), b"public fixture\r\n");
            if changed {
                fs::write(&tracked, content).unwrap();
            }
            if changed {
                assert!(matches!(c.plan(c.id), Err("GIT_TRACKED_CHANGES")));
                assert!(!c.destination().exists());
                continue;
            }
            // The normal isolated comparison really sees a change: this
            // ensures the new profile-specific comparison is exercised.
            assert!(matches!(
                validate_repo(&old.join("work"), &c.repos[0], true),
                Err("GIT_TRACKED_CHANGES")
            ));
            c.plan(c.id).unwrap();
            let dest = c.destination().join("files");
            fs::rename(old, &dest).unwrap();
            c.finish(c.id).unwrap();
            assert_eq!(fs::read(dest.join("work/source.txt")).unwrap(), content);
            assert_eq!(c.load(c.id).unwrap().state, "quarantined");
        }
    }
    #[test]
    fn board_receipt_pins_original_identity_and_rejects_recomputed_tampering() {
        let production = Config::production(BOARD_ID).unwrap();
        assert_eq!(
            production.pinned_identity,
            Some((2229119083, 2251799828682344))
        );
        assert_eq!(
            production.repos[0].head,
            "759f74fb8a1db87308feac491d982b18cde62382"
        );
        assert_eq!(
            production.repos[0].saved_ref,
            Some("refs/remotes/origin/main")
        );
        assert_eq!(
            production.repos[0].saved_merge.as_deref(),
            Some(production.repos[0].head.as_str())
        );
        assert!(Config::production("67feb7811b6d0a94c98465c784b64f46").is_err());
        for field in [
            "project",
            "content",
            "path",
            "volume",
            "file_id",
            "created_at",
        ] {
            let c = fixture_for(BOARD_ID);
            let mut r = c.load(c.id).unwrap();
            match field {
                "project" => r.identity.project = "other-task".into(),
                "content" => r.identity.content = "other-content".into(),
                "path" => r.identity.path.push_str("-other"),
                "volume" => r.identity.volume += 1,
                "file_id" => r.identity.file_id += 1,
                "created_at" => r.identity.created_at = "2026-10-09T18:04:44.1608254+09:00".into(),
                _ => unreachable!(),
            }
            r.integrity = format!(
                "{:x}",
                Sha256::digest(serde_json::to_vec(&r.identity).unwrap())
            );
            fs::write(c.receipt_path(), serde_json::to_vec(&r).unwrap()).unwrap();
            assert!(
                matches!(c.plan(c.id), Err("RECEIPT_SCOPE_MISMATCH")),
                "{field}"
            );
            assert!(!c.destination().exists());
        }
    }
    #[test]
    fn board_published_integration_can_precede_main_and_preserves_all_bytes() {
        let c = fixture_for(BOARD_ID);
        let old = source(&c);
        let work = old.join("work");
        let refs = git(&work, &["show-ref"]).unwrap();
        assert_ne!(
            git(&work, &["rev-parse", "refs/remotes/origin/main"]).unwrap(),
            c.repos[0].head
        );
        fs::write(old.join("verification.log"), b"saved board evidence\n").unwrap();
        c.plan(c.id).unwrap();
        let ticket = fs::read(c.ticket_path()).unwrap();
        let nodes = scan(&old, &c.repos).unwrap();
        let hashes = file_digests(&old, &nodes).unwrap();
        let dest = c.destination().join("files");
        fs::rename(&old, &dest).unwrap();
        c.finish(c.id).unwrap();
        c.finish(c.id).unwrap();
        assert!(!old.exists());
        assert_eq!(c.load(c.id).unwrap().state, "quarantined");
        assert_eq!(
            file_digests(&dest, &scan(&dest, &c.repos).unwrap()).unwrap(),
            hashes
        );
        assert_eq!(fs::read(c.ticket_path()).unwrap(), ticket);
        assert_eq!(git(&dest.join("work"), &["show-ref"]).unwrap(), refs);
        let RepoKind::Linked { admin, .. } = &c.repos[0].kind else {
            unreachable!()
        };
        assert!(same(
            Path::new(&bounded_text(&admin.join("gitdir")).unwrap()),
            &dest.join("work/.git")
        ));
    }
    #[test]
    fn board_rejects_missing_or_unrelated_main_dirty_worktree_and_grafts() {
        for failure in ["missing-main", "unrelated-main", "dirty", "grafts"] {
            let c = fixture_for(BOARD_ID);
            let work = source(&c).join("work");
            match failure {
                "missing-main" => {
                    git(&work, &["update-ref", "-d", "refs/remotes/origin/main"]).unwrap();
                }
                "unrelated-main" => {
                    let tree = format!("{}^{{tree}}", c.repos[0].head);
                    let unrelated = git(
                        &work,
                        &[
                            "-c",
                            "user.name=Fixture",
                            "-c",
                            "user.email=fixture@example.invalid",
                            "commit-tree",
                            &tree,
                            "-m",
                            "unrelated main",
                        ],
                    )
                    .unwrap();
                    git(
                        &work,
                        &["update-ref", "refs/remotes/origin/main", &unrelated],
                    )
                    .unwrap();
                }
                "dirty" => fs::write(work.join("source.txt"), b"unsaved change").unwrap(),
                "grafts" => {
                    let RepoKind::Linked { common, .. } = &c.repos[0].kind else {
                        unreachable!()
                    };
                    fs::write(common.join("info/grafts"), format!("{}\n", c.repos[0].head))
                        .unwrap();
                }
                _ => unreachable!(),
            }
            assert!(c.plan(c.id).is_err(), "{failure}");
            assert!(!c.destination().exists());
            assert!(source(&c).exists());
        }
    }
    #[test]
    fn board_content_proof_rejects_changes_that_restore_size_and_time() {
        let c = fixture_for(BOARD_ID);
        let old = source(&c);
        let log = old.join("verification.log");
        fs::write(&log, b"first evidence").unwrap();
        let stamp = fs::metadata(&log).unwrap().modified().unwrap();
        c.plan(c.id).unwrap();
        let dest = c.destination().join("files");
        fs::rename(old, &dest).unwrap();
        let log = dest.join("verification.log");
        fs::write(&log, b"other evidence").unwrap();
        fs::OpenOptions::new()
            .write(true)
            .open(&log)
            .unwrap()
            .set_times(fs::FileTimes::new().set_modified(stamp))
            .unwrap();
        assert!(matches!(c.finish(c.id), Err("RECOVERY_RECORD_CHANGED")));
        assert_eq!(c.load(c.id).unwrap().state, "owned");
    }
    #[test]
    fn reboot_renewal_resumes_every_publication_boundary_and_preserves_archives() {
        for stage in ["ticket", "reason", "staging", "published", "complete"] {
            let c = fixture_for(REBOOT_ID);
            c.plan(c.id).unwrap();
            let old = fs::read(c.ticket_path()).unwrap();
            let reason = c.destination().join("理由.txt");
            let old_reason = fs::read(&reason).unwrap();
            let archived = c.ticket_path().with_extension("pre-final-report.json");
            let archived_reason = c
                .destination()
                .with_extension("pre-final-report.reason.txt");
            fs::write(source(&c).join("pr-body-final.txt"), b"final public report").unwrap();
            let hash = format!("{:x}", Sha256::digest(b"final public report"));
            let mut replacement: Ticket = serde_json::from_slice(&old).unwrap();
            replacement.nodes = c
                .validate(&c.load(c.id).unwrap(), &source(&c), true)
                .unwrap();
            let bytes = serde_json::to_vec_pretty(&replacement).unwrap();
            fs::rename(c.ticket_path(), &archived).unwrap();
            if stage != "ticket" {
                fs::rename(&reason, &archived_reason).unwrap();
            }
            let unpublished = c.ticket_path().with_extension("proof-writing-interrupted");
            if stage == "staging" {
                write_new(&unpublished, b"partial unpublished record").unwrap();
            }
            if matches!(stage, "published" | "complete") {
                publish_proof(&c.ticket_path(), &bytes).unwrap();
            }
            if stage == "complete" {
                c.plan(c.id).unwrap();
            }
            c.renew_reboot_preflight(c.id, &hash).unwrap();
            c.renew_reboot_preflight(c.id, &hash).unwrap();
            assert_eq!(fs::read(&archived).unwrap(), old, "{stage}");
            assert_eq!(fs::read(&archived_reason).unwrap(), old_reason, "{stage}");
            assert_eq!(fs::read(c.ticket_path()).unwrap(), bytes, "{stage}");
            if stage == "staging" {
                assert_eq!(
                    fs::read(unpublished).unwrap(),
                    b"partial unpublished record"
                );
            }
            fs::rename(source(&c), c.destination().join("files")).unwrap();
            c.finish(c.id).unwrap();
            assert_eq!(c.load(c.id).unwrap().state, "quarantined");
        }
    }
    #[test]
    fn reboot_renewal_rejects_changed_archives_and_published_replacement() {
        for change in ["archive", "reason", "new-ticket", "reason-link"] {
            let c = fixture_for(REBOOT_ID);
            c.plan(c.id).unwrap();
            fs::write(source(&c).join("pr-body-final.txt"), b"final public report").unwrap();
            let hash = format!("{:x}", Sha256::digest(b"final public report"));
            c.renew_reboot_preflight(c.id, &hash).unwrap();
            let archived = c.ticket_path().with_extension("pre-final-report.json");
            let archived_reason = c
                .destination()
                .with_extension("pre-final-report.reason.txt");
            match change {
                "archive" => fs::write(&archived, b"{}").unwrap(),
                "reason" => fs::write(&archived_reason, b"changed").unwrap(),
                "new-ticket" => fs::write(c.ticket_path(), b"{}").unwrap(),
                "reason-link" => {
                    fs::hard_link(&archived_reason, c.destination().join("alias")).unwrap()
                }
                _ => unreachable!(),
            }
            let before = fs::read(&archived).unwrap();
            let reason_before = fs::read(&archived_reason).unwrap();
            let ticket_before = fs::read(c.ticket_path()).unwrap();
            assert!(c.renew_reboot_preflight(c.id, &hash).is_err(), "{change}");
            assert_eq!(fs::read(&archived).unwrap(), before);
            assert_eq!(fs::read(&archived_reason).unwrap(), reason_before);
            assert_eq!(fs::read(c.ticket_path()).unwrap(), ticket_before);
            assert!(source(&c).exists());
        }
    }
    #[test]
    fn reboot_final_report_renewal_preserves_old_intent_and_moves_only_after_validation() {
        let c = fixture_for(REBOOT_ID);
        c.plan(c.id).unwrap();
        let old = fs::read(c.ticket_path()).unwrap();
        fs::write(source(&c).join("pr-body-final.txt"), b"final public report").unwrap();
        assert!(matches!(c.plan(c.id), Err("RECOVERY_RECORD_CHANGED")));
        let hash = format!("{:x}", Sha256::digest(b"final public report"));
        c.renew_reboot_preflight(c.id, &hash).unwrap();
        assert_eq!(
            fs::read(c.ticket_path().with_extension("pre-final-report.json")).unwrap(),
            old
        );
        fs::rename(source(&c), c.destination().join("files")).unwrap();
        assert!(matches!(
            c.renew_reboot_preflight(c.id, &hash),
            Err("MOVEMENT_ALREADY_STARTED")
        ));
        c.finish(c.id).unwrap();
        assert_eq!(c.load(c.id).unwrap().state, "quarantined");
    }
    #[test]
    fn reboot_renewal_rejects_any_other_artifact_or_modified_original() {
        for change in ["wrong-report", "extra-artifact", "modified-original"] {
            let c = fixture_for(REBOOT_ID);
            c.plan(c.id).unwrap();
            let old = fs::read(c.ticket_path()).unwrap();
            fs::write(source(&c).join("pr-body-final.txt"), b"final public report").unwrap();
            match change {
                "wrong-report" => {
                    fs::write(source(&c).join("pr-body-final.txt"), b"unexpected").unwrap()
                }
                "extra-artifact" => {
                    fs::write(source(&c).join("unrelated.txt"), b"different").unwrap()
                }
                "modified-original" => {
                    fs::write(source(&c).join("work/source.txt"), b"changed").unwrap()
                }
                _ => unreachable!(),
            }
            let hash = format!("{:x}", Sha256::digest(b"final public report"));
            assert!(c.renew_reboot_preflight(c.id, &hash).is_err(), "{change}");
            assert_eq!(fs::read(c.ticket_path()).unwrap(), old);
            assert!(
                !c.ticket_path()
                    .with_extension("pre-final-report.json")
                    .exists()
            );
            assert!(source(&c).exists());
        }
    }
    #[test]
    fn adaptive_quarantine_is_bound_to_its_original_receipt_and_pushed_worktree() {
        let config = Config::production(ADAPTIVE_ID).unwrap();
        assert_eq!(config.project, "cc-jobs");
        assert_eq!(config.content, "adaptive-concurrency");
        assert_eq!(config.pinned_identity, Some((2229119083, 6473924479268101)));
        assert_eq!(config.repos.len(), 1);
        assert_eq!(config.repos[0].name, "work");
        assert_eq!(
            config.repos[0].head,
            "0ac365ab478efa36206c968400e48ac5150abf1b"
        );
        assert_eq!(
            config.repos[0].saved_ref,
            Some("refs/remotes/origin/fix/adaptive-account-windows")
        );
        assert!(config.repos[0].allowed_untracked.is_empty());
        assert!(config.repos[0].saved_merge.is_none());
        match &config.repos[0].kind {
            RepoKind::Linked { admin, common } => {
                assert_eq!(
                    admin,
                    Path::new("D:/CC/Worktrees/cc-pc-tools-rs-chatgpt-web/.git/worktrees/work7")
                );
                assert_eq!(
                    common,
                    Path::new("D:/CC/Worktrees/cc-pc-tools-rs-chatgpt-web/.git")
                );
            }
            _ => panic!("expected linked worktree"),
        }
        assert!(Config::production("b6ef1394c70c4f11ee5ac370ad82886f").is_err());
    }
    #[test]
    fn adaptive_profile_preserves_build_hardlinks_refs_and_repairs_its_backlink() {
        let c = fixture_for(ADAPTIVE_ID);
        let source_root = source(&c);
        fs::create_dir(source_root.join("target")).unwrap();
        let artifact = source_root.join("target/test.exe");
        fs::write(&artifact, b"preserved adaptive build").unwrap();
        fs::hard_link(&artifact, source_root.join("target/test-copy.exe")).unwrap();
        let (admin, common) = match &c.repos[0].kind {
            RepoKind::Linked { admin, common } => (admin, common),
            _ => unreachable!(),
        };
        let refs = git(common.parent().unwrap(), &["show-ref"]).unwrap();
        c.plan(c.id).unwrap();
        let destination = c.destination().join("files");
        fs::rename(&source_root, &destination).unwrap();
        c.finish(c.id).unwrap();
        c.finish(c.id).unwrap();
        assert_eq!(c.load(c.id).unwrap().state, "quarantined");
        assert!(!source_root.exists());
        assert_eq!(
            fs::read(destination.join("target/test.exe")).unwrap(),
            b"preserved adaptive build"
        );
        assert_eq!(node(&destination.join("target/test.exe")).unwrap().links, 2);
        assert_eq!(git(common.parent().unwrap(), &["show-ref"]).unwrap(), refs);
        assert!(same(
            Path::new(&bounded_text(&admin.join("gitdir")).unwrap()),
            &destination.join("work/.git")
        ));
        assert_eq!(
            git(&destination.join("work"), &["rev-parse", "HEAD"]).unwrap(),
            c.repos[0].head
        );
    }
    #[test]
    fn adaptive_profile_rejects_dirty_unpushed_and_substituted_receipts() {
        for failure in ["dirty", "missing-pushed-ref", "wrong-file-id"] {
            let c = fixture_for(ADAPTIVE_ID);
            let work = source(&c).join("work");
            match failure {
                "dirty" => fs::write(work.join("source.txt"), b"unsaved change").unwrap(),
                "missing-pushed-ref" => {
                    git(&work, &["update-ref", "-d", c.repos[0].saved_ref.unwrap()]).unwrap();
                }
                "wrong-file-id" => {
                    let mut receipt = c.load(c.id).unwrap();
                    receipt.identity.file_id += 1;
                    receipt.integrity = format!(
                        "{:x}",
                        Sha256::digest(serde_json::to_vec(&receipt.identity).unwrap())
                    );
                    fs::write(c.receipt_path(), serde_json::to_vec(&receipt).unwrap()).unwrap();
                }
                _ => unreachable!(),
            }
            assert!(c.plan(c.id).is_err(), "{failure}");
            assert!(!c.destination().exists());
            assert!(source(&c).exists());
        }
    }
    #[test]
    fn reboot_quarantine_is_bound_to_its_original_receipt_and_pushed_worktree() {
        let config = super::Config::production(super::REBOOT_ID).unwrap();
        assert_eq!(config.project, "cc-jobs");
        assert_eq!(config.content, "reboot-recovery");
        assert_eq!(
            config.pinned_identity,
            Some((2229119083, 13229323920896790))
        );
        assert_eq!(config.repos.len(), 1);
        assert_eq!(config.repos[0].name, "work");
        assert_eq!(
            config.repos[0].head,
            "fa80a74f15b50c0ba0cf3865a19da53e2c23a00b"
        );
        assert_eq!(
            config.repos[0].saved_ref,
            Some("refs/remotes/origin/fix/reboot-job-recovery")
        );
        assert!(config.repos[0].allowed_untracked.is_empty());
        assert!(super::Config::production("464577ad28cdf40461bb04a301f7e8e8").is_err());
    }
    #[test]
    fn reboot_profile_moves_its_worktree_without_losing_git_or_receipt_identity() {
        let c = fixture_for(REBOOT_ID);
        let plan = c.plan(c.id).unwrap();
        assert_eq!(plan["action"], "submit_quarantine_to_exec_tool");
        let old = source(&c);
        let destination = c.destination().join("files");
        fs::rename(&old, &destination).unwrap();
        c.finish(c.id).unwrap();
        assert_eq!(
            git(&destination.join("work"), &["rev-parse", "HEAD"]).unwrap(),
            c.repos[0].head
        );
        assert_eq!(c.load(c.id).unwrap().state, "quarantined");
        assert!(!old.exists());
    }
    #[test]
    fn reboot_profile_rejects_dirty_unpushed_or_substituted_receipts() {
        for failure in ["dirty", "missing-pushed-ref", "wrong-file-id"] {
            let c = fixture_for(REBOOT_ID);
            let work = source(&c).join("work");
            match failure {
                "dirty" => fs::write(work.join("source.txt"), b"unsaved change").unwrap(),
                "missing-pushed-ref" => {
                    git(&work, &["update-ref", "-d", c.repos[0].saved_ref.unwrap()]).unwrap();
                }
                "wrong-file-id" => {
                    let mut r = c.load(c.id).unwrap();
                    r.identity.file_id += 1;
                    r.integrity = format!(
                        "{:x}",
                        Sha256::digest(serde_json::to_vec(&r.identity).unwrap())
                    );
                    fs::write(c.receipt_path(), serde_json::to_vec(&r).unwrap()).unwrap();
                }
                _ => unreachable!(),
            }
            assert!(c.plan(c.id).is_err(), "{failure}");
            assert!(!c.destination().exists());
            assert!(source(&c).exists());
        }
    }
    use super::*;
    use std::ops::{Deref, DerefMut};
    use std::sync::atomic::{AtomicU64, Ordering};
    static SERIAL: AtomicU64 = AtomicU64::new(0);
    #[test]
    fn unslop_profiles_pin_four_original_identities_and_the_saved_worktree() {
        let expected = [
            (unslop::BUILD, 1215965535, 8162774328342697),
            (unslop::CORE, 2229119083, 3377699730734494),
            (unslop::MEASUREMENT, 2229119083, 25895697868367764),
            (unslop::INTEGRATION, 2229119083, 2533274803590816),
        ];
        for (id, volume, file_id) in expected {
            let c = Config::production(id).unwrap();
            assert_eq!(c.pinned_identity, Some((volume, file_id)));
            assert!(!c.project_alias_preservation_enabled);
        }
        let c = Config::production(unslop::BUILD).unwrap();
        assert_eq!(c.temp, PathBuf::from("D:/CC/Apps/GoDD-Unslop"));
        assert_eq!(c.quarantine, PathBuf::from("D:/削除希望"));
        let c = Config::production(unslop::INTEGRATION).unwrap();
        assert_eq!(c.repos[0].head, "0621aac55a3035ab473cb2f7b49887a5a0925d6c");
        assert_eq!(
            c.repos[0].saved_ref,
            Some("refs/remotes/origin/codex/todo1931-unslop-20261006")
        );
        assert!(Config::production("ef300280dcb8c0e08fcb9be1318565ee").is_err());
    }
    #[test]
    fn unslop_large_metadata_envelope_is_readable_only_for_the_fixed_integration() {
        let c = fixture_for(unslop::INTEGRATION);
        let large = c.temp.join("metadata-envelope.json");
        let bytes = vec![b' '; 9 * 1024 * 1024];
        fs::write(&large, &bytes).unwrap();
        assert_eq!(
            read_record(&large, unslop::record_limit(&c)).unwrap(),
            bytes
        );
        for id in [unslop::BUILD, unslop::CORE, unslop::MEASUREMENT] {
            let other = Config::production(id).unwrap();
            assert!(read_record(&large, unslop::record_limit(&other)).is_err());
        }
        let too_large = c.temp.join("oversize-envelope.json");
        fs::write(&too_large, vec![b' '; 16 * 1024 * 1024 + 1]).unwrap();
        assert!(read_record(&too_large, unslop::record_limit(&c)).is_err());
    }
    fn unslop_move(c: &Config) {
        fs::rename(source(c), c.destination().join("files")).unwrap();
    }
    fn unslop_registry_fixture() -> Fixture {
        let c = fixture_for(unslop::MEASUREMENT);
        let registry = c.temp.join("registry-target");
        fs::create_dir(&registry).unwrap();
        fs::write(registry.join("sentinel"), b"owned fixture target").unwrap();
        for name in [
            "measurement-63628-1791287330489098600",
            "measurement-80712-1791286803451627800",
        ] {
            let parent = source(&c).join("measurement").join(name).join("cargo-home");
            fs::create_dir_all(&parent).unwrap();
            std::os::windows::fs::symlink_dir(&registry, parent.join("registry")).unwrap();
        }
        c
    }
    #[test]
    fn unslop_build_internal_hardlinks_and_repeat_finish_preserve_the_same_files() {
        for id in [unslop::BUILD, unslop::CORE] {
            let c = fixture_for(id);
            let target = source(&c).join("target");
            fs::create_dir(&target).unwrap();
            fs::write(target.join("binary"), b"retired build").unwrap();
            fs::hard_link(target.join("binary"), target.join("alias")).unwrap();
            let before = node(&target.join("binary")).unwrap();
            let plan = c.plan(id).unwrap();
            assert_eq!(plan, c.plan(id).unwrap());
            unslop_move(&c);
            let done = c.finish(id).unwrap();
            assert_eq!(done, c.finish(id).unwrap());
            assert_eq!(
                before,
                node(&c.destination().join("files/target/binary")).unwrap()
            );
            assert_eq!(c.load(id).unwrap().state, "quarantined");
        }
    }
    #[test]
    fn unslop_external_build_alias_is_rejected_before_preparing_any_destination() {
        let c = fixture_for(unslop::CORE);
        fs::write(source(&c).join("binary"), b"fixture").unwrap();
        fs::hard_link(source(&c).join("binary"), c.temp.join("external-alias")).unwrap();
        assert_eq!(c.plan(c.id).unwrap_err(), "EXTERNAL_HARD_LINK");
        assert!(!c.destination().exists());
        assert!(source(&c).exists());
    }
    #[test]
    fn unslop_registry_links_are_preserved_without_mutating_the_owned_mock_target() {
        let c = unslop_registry_fixture();
        let sentinel = c.temp.join("registry-target/sentinel");
        let before = node(&sentinel).unwrap();
        assert_eq!(unslop::inspect(&c, c.id).unwrap()["links"], 2);
        assert!(!c.ticket_path().with_extension("unslop.json").exists());
        c.plan(c.id).unwrap();
        unslop_move(&c);
        c.finish(c.id).unwrap();
        assert_eq!(node(&sentinel).unwrap(), before);
        assert_eq!(fs::read(&sentinel).unwrap(), b"owned fixture target");
    }
    #[test]
    fn unslop_registry_link_retargeting_is_rejected_before_move() {
        let c = unslop_registry_fixture();
        let link = source(&c)
            .join("measurement/measurement-63628-1791287330489098600/cargo-home/registry");
        fs::remove_dir(&link).unwrap();
        std::os::windows::fs::symlink_dir(&c.temp, &link).unwrap();
        assert_eq!(c.plan(c.id).unwrap_err(), "REGISTRY_LINK_CHANGED");
        assert!(!c.destination().exists());
    }
    fn unslop_junction_fixture() -> Fixture {
        let c = fixture_for(unslop::INTEGRATION);
        // Match the real repository's ignored installed dependencies. The
        // fixture still rejects untracked sources outside node_modules.
        if let RepoKind::Linked { common, .. } = &c.repos[0].kind {
            fs::write(
                common.join("info/exclude"),
                b"godd-mcp-alpha/node_modules/\n",
            )
            .unwrap();
        }
        let target =
            source(&c).join("worktree/godd-mcp-alpha/node_modules/.pnpm/pkg/node_modules/pkg");
        fs::create_dir_all(&target).unwrap();
        fs::write(target.join("index.js"), b"owned fixture package").unwrap();
        fs::hard_link(target.join("index.js"), c.temp.join("package-store-alias")).unwrap();
        let junction = source(&c).join("worktree/godd-mcp-alpha/node_modules/pkg");
        let junction_arg = junction.to_string_lossy().replace('/', "\\");
        let target_arg = target.to_string_lossy().replace('/', "\\");
        hidden(
            Path::new("cmd.exe"),
            &[
                OsStr::new("/C"),
                OsStr::new("mklink"),
                OsStr::new("/J"),
                OsStr::new(&junction_arg),
                OsStr::new(&target_arg),
            ],
        )
        .unwrap();
        c
    }
    #[test]
    fn unslop_pnpm_junction_shared_files_and_git_backlink_survive_archival() {
        let c = unslop_junction_fixture();
        let link = source(&c).join("worktree/godd-mcp-alpha/node_modules/pkg");
        let raw = scoopa::raw_reparse(&link).unwrap();
        let outside = c.temp.join("package-store-alias");
        let outside_node = node(&outside).unwrap();
        assert_eq!(unslop::inspect(&c, c.id).unwrap()["links"], 1);
        c.plan(c.id).unwrap();
        unslop_move(&c);
        c.finish(c.id).unwrap();
        assert_eq!(
            raw,
            scoopa::raw_reparse(
                &c.destination()
                    .join("files/worktree/godd-mcp-alpha/node_modules/pkg")
            )
            .unwrap()
        );
        assert_eq!(outside_node, node(&outside).unwrap());
        assert_eq!(
            git(
                &c.destination().join("files/worktree"),
                &["rev-parse", "HEAD"]
            )
            .unwrap(),
            c.repos[0].head
        );
        if let RepoKind::Linked { admin, .. } = &c.repos[0].kind {
            assert!(same(
                Path::new(&bounded_text(&admin.join("gitdir")).unwrap()),
                &c.destination().join("files/worktree/.git")
            ));
        }
    }
    #[test]
    fn unslop_external_pnpm_alias_count_can_change_without_losing_owned_names() {
        let c = unslop_junction_fixture();
        c.plan(c.id).unwrap();
        let package = source(&c)
            .join("worktree/godd-mcp-alpha/node_modules/.pnpm/pkg/node_modules/pkg/index.js");
        fs::hard_link(&package, c.temp.join("another-external-alias")).unwrap();
        let before = node(&package).unwrap();
        unslop_move(&c);
        c.finish(c.id).unwrap();
        assert_eq!(
            before,
            node(&c.destination().join(
                "files/worktree/godd-mcp-alpha/node_modules/.pnpm/pkg/node_modules/pkg/index.js"
            ))
            .unwrap()
        );
        assert!(c.temp.join("another-external-alias").exists());
    }
    #[test]
    fn unslop_dirty_worktree_and_additional_git_are_rejected() {
        let c = fixture_for(unslop::INTEGRATION);
        fs::write(source(&c).join("worktree/source.txt"), b"unsaved change").unwrap();
        assert_eq!(c.plan(c.id).unwrap_err(), "GIT_TRACKED_CHANGES");
        let c = fixture_for(unslop::BUILD);
        fs::create_dir(source(&c).join(".git")).unwrap();
        assert_eq!(c.plan(c.id).unwrap_err(), "UNDECLARED_GIT_METADATA");
    }
    fn unslop_submodule_fixture() -> Fixture {
        let mut c = fixture_for(unslop::INTEGRATION);
        let seed = c.temp.join("site-seed");
        fs::create_dir(&seed).unwrap();
        git(&seed, &["init", "-b", "main"]).unwrap();
        git(&seed, &["config", "user.name", "fixture"]).unwrap();
        git(&seed, &["config", "user.email", "fixture@example.invalid"]).unwrap();
        fs::write(seed.join("source.txt"), b"owned submodule fixture").unwrap();
        git(&seed, &["add", "source.txt"]).unwrap();
        git(&seed, &["commit", "-m", "fixture"]).unwrap();
        let work = source(&c).join("worktree");
        // Build the same independent module metadata layout as the archived
        // source. Modern `submodule add` in a linked fixture may instead share
        // objects through an extra commondir, which is intentionally rejected.
        let RepoKind::Linked { admin, .. } = &c.repos[0].kind else {
            panic!("fixture kind");
        };
        let module = admin.join("modules/officialsite");
        fs::create_dir_all(module.parent().unwrap()).unwrap();
        let site = work.join("officialsite");
        git(
            &work,
            &[
                "clone",
                "--no-hardlinks",
                "--separate-git-dir",
                module.to_str().unwrap(),
                seed.to_str().unwrap(),
                site.to_str().unwrap(),
            ],
        )
        .unwrap();
        let pointer = format!(
            "gitdir: {}{}\n",
            "../".repeat(site.strip_prefix(&c.temp).unwrap().components().count()),
            module.strip_prefix(&c.temp).unwrap().display()
        );
        fs::write(site.join(".git"), pointer).unwrap();
        git(
            &work,
            &[
                "config",
                "--file",
                ".gitmodules",
                "submodule.officialsite.path",
                "officialsite",
            ],
        )
        .unwrap();
        git(
            &work,
            &[
                "config",
                "--file",
                ".gitmodules",
                "submodule.officialsite.url",
                seed.to_str().unwrap(),
            ],
        )
        .unwrap();
        git(&work, &["add", ".gitmodules", "officialsite"]).unwrap();
        git(
            &work,
            &[
                "-c",
                "user.name=fixture",
                "-c",
                "user.email=fixture@example.invalid",
                "commit",
                "-m",
                "owned submodule fixture",
            ],
        )
        .unwrap();
        c.repos[0].head = git(&work, &["rev-parse", "HEAD"]).unwrap();
        git(
            &work,
            &[
                "update-ref",
                c.repos[0].saved_ref.unwrap(),
                &c.repos[0].head,
            ],
        )
        .unwrap();
        c
    }
    #[test]
    fn unslop_fixed_submodule_is_archived_verbatim_and_unsaved_changes_are_rejected() {
        let c = unslop_submodule_fixture();
        let pointer = fs::read(source(&c).join("worktree/officialsite/.git")).unwrap();
        let before = node(&source(&c).join("worktree/officialsite/source.txt")).unwrap();
        c.plan(c.id).unwrap();
        unslop_move(&c);
        c.finish(c.id).unwrap();
        assert_eq!(
            pointer,
            fs::read(c.destination().join("files/worktree/officialsite/.git")).unwrap()
        );
        assert_eq!(
            before,
            node(
                &c.destination()
                    .join("files/worktree/officialsite/source.txt")
            )
            .unwrap()
        );
        let c = unslop_submodule_fixture();
        fs::write(
            source(&c).join("worktree/officialsite/source.txt"),
            b"unsaved submodule",
        )
        .unwrap();
        assert_eq!(c.plan(c.id).unwrap_err(), "SUBMODULE_UNSAVED_CHANGES");
        assert!(!c.destination().exists());
    }
    #[test]
    fn unslop_backlink_acl_difference_is_rejected_before_movement() {
        let c = fixture_for(unslop::INTEGRATION);
        c.prepare_backlink(&source(&c), &c.destination().join("files"))
            .unwrap();
        let RepoKind::Linked { admin, .. } = &c.repos[0].kind else {
            panic!("fixture kind");
        };
        let replacement = admin.join("gitdir.todo2052-repair");
        hidden(
            Path::new("icacls.exe"),
            &[replacement.as_os_str(), OsStr::new("/inheritance:d")],
        )
        .unwrap();
        assert_eq!(c.plan(c.id).unwrap_err(), "BACKLINK_SECURITY_MISMATCH");
        assert!(source(&c).exists());
        assert!(!c.destination().exists());
    }
    #[test]
    fn unslop_late_replacement_acl_change_cannot_commit_the_git_backlink() {
        let c = fixture_for(unslop::INTEGRATION);
        c.plan(c.id).unwrap();
        let RepoKind::Linked { admin, .. } = &c.repos[0].kind else {
            panic!("fixture kind");
        };
        let old = bounded_text(&admin.join("gitdir")).unwrap();
        let replacement = admin.join("gitdir.todo2052-repair");
        hidden(
            Path::new("icacls.exe"),
            &[replacement.as_os_str(), OsStr::new("/inheritance:d")],
        )
        .unwrap();
        unslop_move(&c);
        assert_eq!(c.finish(c.id).unwrap_err(), "BACKLINK_SECURITY_MISMATCH");
        assert_eq!(old, bounded_text(&admin.join("gitdir")).unwrap());
        assert!(replacement.exists());
        assert_eq!(c.load(c.id).unwrap().state, "owned");
    }
    #[test]
    fn unslop_changed_file_and_missing_proof_never_mark_the_receipt_complete() {
        for failure in ["file", "proof"] {
            let c = fixture_for(unslop::CORE);
            fs::write(source(&c).join("binary"), b"original").unwrap();
            c.plan(c.id).unwrap();
            unslop_move(&c);
            if failure == "file" {
                fs::write(c.destination().join("files/binary"), b"changed bytes").unwrap();
            } else {
                fs::remove_file(
                    c.ticket_path()
                        .with_extension("unslop.json")
                        .with_extension("sha256"),
                )
                .unwrap();
            }
            assert!(c.finish(c.id).is_err(), "{failure}");
            assert_eq!(c.load(c.id).unwrap().state, "owned");
            if failure == "proof" {
                assert!(
                    !c.ticket_path()
                        .with_extension("unslop.json")
                        .with_extension("sha256")
                        .exists()
                );
            }
        }
    }
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
        c.project_alias_preservation_enabled = false;
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
            let internal = c.id == INTERNAL_2184_ID && matches!(repo.kind, RepoKind::Linked { .. });
            let seed = if internal {
                target.join("repo")
            } else if matches!(repo.kind, RepoKind::Linked { .. }) {
                base.join("common")
            } else {
                path.clone()
            };
            if internal {
                repo.head = git(&seed, &["rev-parse", "HEAD"]).unwrap();
                git(&seed, &["update-ref", repo.saved_ref.unwrap(), &repo.head]).unwrap();
            } else {
                fs::create_dir(&seed).unwrap();
                seed_repo(&seed, repo);
            }
            if matches!(repo.kind, RepoKind::Linked { .. }) {
                let mut add = Vec::new();
                if c.id == BOARD_ID {
                    add.extend(["-c", "core.autocrlf=true"]);
                }
                add.extend([
                    "worktree",
                    "add",
                    "-b",
                    "fixture-linked",
                    path.to_str().unwrap(),
                ]);
                git(&seed, &add).unwrap();
                repo.kind = RepoKind::Linked {
                    admin: seed.join(".git/worktrees").join(repo.name),
                    common: seed.join(".git"),
                };
            }
            if c.id == BOARD_ID {
                repo.saved_merge = Some(repo.head.clone());
            }
        }
        let n = node(&target).unwrap();
        if c.pinned_identity.is_some() {
            c.pinned_identity = Some((n.volume, n.file_id));
        }
        let identity = Identity {
            version: 2,
            id: c.id.into(),
            root: base.to_string_lossy().into_owned(),
            path: target.to_string_lossy().into_owned(),
            volume: n.volume,
            file_id: n.file_id,
            created_at: if c.id == INTERNAL_2184_ID {
                "2026-10-10T00:39:58.8390892+09:00"
            } else if c.id == BOARD_ID {
                BOARD_CREATED
            } else if c.id == CHROME_ID {
                CHROME_CREATED
            } else if c.id == DECISION_ID {
                DECISION_CREATED
            } else {
                "2026-10-08T15:34:13.325996+09:00"
            }
            .into(),
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
        if repo.saved_merge.is_some() {
            let tree = format!("{}^{{tree}}", repo.head);
            if repo.squash_tree.is_some() {
                let tree_id = git(path, &["rev-parse", &tree]).unwrap();
                let squash = git(
                    path,
                    &[
                        "-c",
                        "user.name=Fixture",
                        "-c",
                        "user.email=fixture@example.invalid",
                        "commit-tree",
                        &tree_id,
                        "-m",
                        "published squash fixture",
                    ],
                )
                .unwrap();
                git(path, &["update-ref", "refs/remotes/origin/main", &squash]).unwrap();
                repo.saved_merge = Some(squash);
                repo.squash_tree = Some(tree_id);
                return;
            }
            let merge = git(
                path,
                &[
                    "-c",
                    "user.name=Fixture",
                    "-c",
                    "user.email=fixture@example.invalid",
                    "commit-tree",
                    &tree,
                    "-p",
                    &repo.head,
                    "-m",
                    "fixture merge",
                ],
            )
            .unwrap();
            git(path, &["update-ref", "refs/remotes/origin/main", &merge]).unwrap();
            repo.saved_merge = Some(merge);
        }
    }
    #[test]
    fn queue_owned_internal_pair_moves_and_resumes_after_each_pointer_replacement() {
        for replaced in 0..=2 {
            let c = fixture_for(INTERNAL_2184_ID);
            let root = source(&c);
            fs::write(root.join("verification.log"), b"saved evidence\n").unwrap();
            for name in c.repos[0].allowed_untracked {
                let path = root.join("repo").join(name);
                fs::create_dir_all(path.parent().unwrap()).unwrap();
                fs::write(path, b"runtime evidence\n").unwrap();
            }
            let original = QUEUE_POINTERS.map(|name| fs::read(root.join(name)).unwrap());
            c.plan(c.id).unwrap();
            c.plan(c.id).unwrap();
            let t: Ticket = serde_json::from_slice(&fs::read(c.ticket_path()).unwrap()).unwrap();
            let record: InternalRepair =
                serde_json::from_slice(&fs::read(c.internal_record_path()).unwrap()).unwrap();
            for (i, bytes) in original.iter().enumerate() {
                assert_eq!(record.pointers[i].original, *bytes);
            }
            let moved = c.destination().join("files");
            fs::rename(&root, &moved).unwrap();
            for (i, name) in QUEUE_POINTERS.iter().enumerate().take(replaced) {
                fs::rename(c.internal_stage(i), moved.join(name)).unwrap();
            }
            c.finish(c.id).unwrap();
            c.finish(c.id).unwrap();
            assert_eq!(c.load(c.id).unwrap().state, "quarantined");
            assert_eq!(
                fs::read(moved.join("verification.log")).unwrap(),
                b"saved evidence\n"
            );
            for repo in &c.repos {
                assert_eq!(
                    git(&moved.join(repo.name), &["rev-parse", "HEAD"]).unwrap(),
                    repo.head
                );
            }
            assert_eq!(t.nodes.len(), scan(&moved, &c.repos).unwrap().len());
        }
    }
    #[test]
    fn queue_profile_rejects_other_worktrees_unsaved_and_unpushed_changes() {
        for kind in [
            "extra-worktree",
            "tracked",
            "untracked",
            "ref",
            "pointer",
            "grafts",
        ] {
            let c = fixture_for(INTERNAL_2184_ID);
            let root = source(&c);
            match kind {
                "extra-worktree" => {
                    fs::create_dir(root.join("repo/.git/worktrees/other")).unwrap();
                }
                "tracked" => fs::write(root.join("todo-note-tool/source.txt"), b"unsaved").unwrap(),
                "untracked" => fs::write(root.join("repo/unpublished.rs"), b"unsaved").unwrap(),
                "ref" => {
                    git(
                        &root.join("repo"),
                        &["update-ref", "-d", c.repos[1].saved_ref.unwrap()],
                    )
                    .unwrap();
                }
                "pointer" => {
                    fs::write(root.join(QUEUE_POINTERS[0]), b"gitdir: C:/outside\n").unwrap()
                }
                _ => fs::write(root.join("repo/.git/info/grafts"), b"replacement").unwrap(),
            }
            assert!(c.plan(c.id).is_err(), "{kind}");
            assert!(!c.ticket_path().exists());
            assert!(!c.destination().exists());
            assert_eq!(c.load(c.id).unwrap().state, "owned");
        }
    }
    #[test]
    fn queue_internal_finish_rejects_changed_content_pointer_and_record() {
        for kind in ["content", "pointer", "stage", "record", "proof", "root"] {
            let c = fixture_for(INTERNAL_2184_ID);
            let root = source(&c);
            let file = root.join("verification.log");
            fs::write(&file, b"aaaa").unwrap();
            c.plan(c.id).unwrap();
            let moved = c.destination().join("files");
            fs::rename(&root, &moved).unwrap();
            match kind {
                "content" => {
                    let file = moved.join("verification.log");
                    let stamp = fs::metadata(&file).unwrap().modified().unwrap();
                    fs::write(&file, b"bbbb").unwrap();
                    fs::File::options()
                        .write(true)
                        .open(file)
                        .unwrap()
                        .set_times(fs::FileTimes::new().set_modified(stamp))
                        .unwrap();
                }
                "pointer" => {
                    fs::write(moved.join(QUEUE_POINTERS[0]), b"gitdir: C:/outside\n").unwrap()
                }
                "stage" => fs::write(c.internal_stage(1), b"replacement").unwrap(),
                "record" => fs::write(c.internal_record_path(), b"{}").unwrap(),
                "proof" => fs::write(c.proof_path(), b"{}").unwrap(),
                _ => {
                    fs::rename(&moved, c.destination().join("kept")).unwrap();
                    fs::create_dir(&moved).unwrap();
                }
            }
            assert!(c.finish(c.id).is_err(), "{kind}");
            assert_eq!(c.load(c.id).unwrap().state, "owned");
        }
    }
    fn source(c: &Config) -> PathBuf {
        c.temp.join(format!("codex-owned-{}", c.id))
    }
    fn scoopa_fixture() -> Fixture {
        let c = fixture_for(scoopa::ID);
        let modules = source(&c).join("frontend-runtime/node_modules");
        fs::create_dir_all(modules.join(".pnpm/pkg/node_modules/pkg")).unwrap();
        fs::write(modules.join(".pnpm/pkg/node_modules/pkg/value"), b"keep me").unwrap();
        std::os::windows::fs::symlink_dir(r".pnpm\pkg\node_modules\pkg", modules.join("pkg"))
            .unwrap();
        c
    }
    #[test]
    fn scoopa_fixed_production_identity_matches_original_go_checksum() {
        let c = Config::production(scoopa::ID).unwrap();
        assert_eq!(c.store, Path::new("D:/CC/Tools/safe-delete/receipts-go"));
        let (volume, file_id) = c.pinned_identity.unwrap();
        let identity = Identity {
            version: 2,
            id: scoopa::ID.into(),
            root: c.temp.to_string_lossy().into_owned(),
            path: source(&c).to_string_lossy().into_owned(),
            volume,
            file_id,
            created_at: scoopa::CREATED_AT.into(),
            project: c.project.into(),
            content: c.content.into(),
        };
        assert_eq!(
            format!(
                "{:x}",
                Sha256::digest(serde_json::to_vec(&identity).unwrap())
            ),
            scoopa::INTEGRITY
        );
    }
    fn move_scoopa_fixture(c: &Config) {
        // Simulate the caller's ordinary directory rename, never a production API.
        fs::rename(source(c), c.destination().join("files")).unwrap();
    }
    #[test]
    fn scoopa_relative_link_is_enumerated_once_and_move_preserves_identity() {
        let c = scoopa_fixture();
        let original = node(&source(&c)).unwrap();
        let plan = c.plan(scoopa::ID).unwrap();
        assert_eq!(plan["links"], 1);
        assert_eq!(plan["bytes"], 7);
        let ticket: serde_json::Value =
            serde_json::from_slice(&fs::read(c.ticket_path()).unwrap()).unwrap();
        let names = ticket["nodes"].as_object().unwrap();
        assert!(!names.contains_key("frontend-runtime/node_modules/pkg/value"));
        assert_eq!(
            names["frontend-runtime/node_modules/pkg"]["link"]["target"],
            r".pnpm\pkg\node_modules\pkg"
        );
        assert_eq!(c.plan(scoopa::ID).unwrap(), plan);
        move_scoopa_fixture(&c);
        assert_eq!(node(&c.destination().join("files")).unwrap(), original);
        let result = c.finish(scoopa::ID).unwrap();
        assert_eq!(result["inventory_verified"], true);
        assert_eq!(result["links"], 1);
        assert_eq!(c.load(scoopa::ID).unwrap().state, "quarantined");
        assert_eq!(c.finish(scoopa::ID).unwrap(), result);
        assert_eq!(
            fs::read(
                c.destination()
                    .join("files/frontend-runtime/node_modules/pkg/value")
            )
            .unwrap(),
            b"keep me"
        );
    }
    #[test]
    fn scoopa_rejects_external_absolute_and_missing_targets_without_traversing() {
        for target in [r"..\..\..\..\outside", "missing", "C:/Windows"] {
            let c = fixture_for(scoopa::ID);
            let modules = source(&c).join("frontend-runtime/node_modules");
            fs::create_dir_all(&modules).unwrap();
            std::os::windows::fs::symlink_dir(target, modules.join("bad")).unwrap();
            assert!(matches!(
                c.plan(scoopa::ID),
                Err("EXTERNAL_LINK" | "LINK_TARGET_MISSING" | "NOT_RELATIVE_SYMLINK")
            ));
            assert!(!c.ticket_path().exists());
            assert!(!c.destination().exists());
        }
    }
    #[test]
    fn scoopa_rejects_cycle_and_link_ancestors() {
        let c = scoopa_fixture();
        let modules = source(&c).join("frontend-runtime/node_modules");
        std::os::windows::fs::symlink_dir("pkg", modules.join("cycle")).unwrap();
        assert!(matches!(c.plan(scoopa::ID), Err("LINK_TARGET_IS_LINK")));
        fs::remove_dir(modules.join("cycle")).unwrap();
        std::os::windows::fs::symlink_dir(r"pkg\..", modules.join("cycle")).unwrap();
        assert!(matches!(c.plan(scoopa::ID), Err("LINK_NON_PNPM_TARGET")));
        fs::remove_dir(modules.join("cycle")).unwrap();
        std::os::windows::fs::symlink_dir(r"..\..", modules.join("cycle")).unwrap();
        assert!(matches!(c.plan(scoopa::ID), Err("LINK_TARGET_ANCESTOR")));
    }
    #[test]
    fn scoopa_root_replacement_and_receipt_tampering_are_rejected() {
        let c = scoopa_fixture();
        let mut receipt = c.load(scoopa::ID).unwrap();
        receipt.integrity = "changed".into();
        fs::write(c.receipt_path(), serde_json::to_vec(&receipt).unwrap()).unwrap();
        assert!(matches!(
            c.plan(scoopa::ID),
            Err("RECEIPT_INTEGRITY_MISMATCH")
        ));
        let c = scoopa_fixture();
        fs::rename(source(&c), c.base.join("original")).unwrap();
        fs::create_dir(source(&c)).unwrap();
        assert!(matches!(c.plan(scoopa::ID), Err("TARGET_IDENTITY_CHANGED")));
        let c = scoopa_fixture();
        fs::rename(source(&c), c.base.join("original")).unwrap();
        std::os::windows::fs::symlink_dir(c.base.join("original"), source(&c)).unwrap();
        assert!(matches!(c.plan(scoopa::ID), Err("LINK_OR_REPARSE_POINT")));
    }
    #[test]
    fn scoopa_rejects_changed_file_before_move_and_after_move() {
        for moved in [false, true] {
            let c = scoopa_fixture();
            c.plan(scoopa::ID).unwrap();
            let root = if moved {
                move_scoopa_fixture(&c);
                c.destination().join("files")
            } else {
                source(&c)
            };
            fs::write(
                root.join("frontend-runtime/node_modules/.pnpm/pkg/node_modules/pkg/value"),
                b"changed size",
            )
            .unwrap();
            if moved {
                assert!(matches!(
                    c.finish(scoopa::ID),
                    Err("CONTENT_CHANGED_AFTER_PLAN")
                ));
            } else {
                assert!(matches!(c.plan(scoopa::ID), Err("RECOVERY_RECORD_CHANGED")));
            }
            assert_eq!(c.load(scoopa::ID).unwrap().state, "owned");
        }
    }
    #[test]
    fn scoopa_changed_link_and_missing_proof_are_rejected_after_move() {
        let c = scoopa_fixture();
        c.plan(scoopa::ID).unwrap();
        move_scoopa_fixture(&c);
        let modules = c.destination().join("files/frontend-runtime/node_modules");
        fs::remove_dir(modules.join("pkg")).unwrap();
        std::os::windows::fs::symlink_dir(r".pnpm\pkg\node_modules", modules.join("pkg")).unwrap();
        assert!(matches!(
            c.finish(scoopa::ID),
            Err("CONTENT_CHANGED_AFTER_PLAN")
        ));
        assert_eq!(c.load(scoopa::ID).unwrap().state, "owned");
        let c = scoopa_fixture();
        c.plan(scoopa::ID).unwrap();
        move_scoopa_fixture(&c);
        fs::remove_file(c.ticket_path().with_extension("scoopa-proof.json")).unwrap();
        assert!(c.finish(scoopa::ID).is_err());
        assert_eq!(c.load(scoopa::ID).unwrap().state, "owned");
    }
    #[test]
    fn scoopa_ticket_tampering_and_destination_replacement_are_rejected() {
        let c = scoopa_fixture();
        c.plan(scoopa::ID).unwrap();
        move_scoopa_fixture(&c);
        let mut bytes = fs::read(c.ticket_path()).unwrap();
        bytes.push(b' ');
        fs::write(c.ticket_path(), bytes).unwrap();
        assert!(matches!(
            c.finish(scoopa::ID),
            Err("RECOVERY_RECORD_CHANGED")
        ));
        let c = scoopa_fixture();
        c.plan(scoopa::ID).unwrap();
        move_scoopa_fixture(&c);
        fs::rename(c.destination().join("files"), c.base.join("original")).unwrap();
        fs::create_dir(c.destination().join("files")).unwrap();
        assert!(matches!(
            c.finish(scoopa::ID),
            Err("TARGET_IDENTITY_CHANGED")
        ));
    }
    #[test]
    fn scoopa_keeps_normal_scanner_and_protected_paths_closed() {
        let c = scoopa_fixture();
        assert!(matches!(
            scan(&source(&c), &[]),
            Err("LINK_OR_REPARSE_POINT")
        ));
        fs::create_dir(source(&c).join("OpenMausBot")).unwrap();
        assert!(matches!(c.plan(scoopa::ID), Err("PROTECTED_PATH")));
        assert!(!c.ticket_path().exists());
        assert!(matches!(c.plan(ID), Err("SCOPE_NOT_AUTHORIZED")));
    }
    #[test]
    fn scoopa_supports_measured_scale_but_keeps_its_fixed_entry_bound() {
        let c = scoopa_fixture();
        let root = source(&c);
        let generated = root.join("generated");
        fs::create_dir(&generated).unwrap();
        for i in 0..10001 {
            fs::write(generated.join(format!("{i}")), b"").unwrap();
        }
        assert!(matches!(
            scan(&root, &[]),
            Err("ENTRY_LIMIT_EXCEEDED" | "LINK_OR_REPARSE_POINT")
        ));
        assert!(c.plan(scoopa::ID).unwrap()["entries"].as_u64().unwrap() > 10000);
        for i in 10001..17800 {
            fs::write(generated.join(format!("{i}")), b"").unwrap();
        }
        assert!(matches!(c.plan(scoopa::ID), Err("ENTRY_LIMIT_EXCEEDED")));
    }
    #[test]
    fn scoopa_rejects_external_hardlinks_and_unexpected_git_metadata() {
        let c = scoopa_fixture();
        fs::write(source(&c).join("outside-pnpm"), b"fixture").unwrap();
        fs::hard_link(source(&c).join("outside-pnpm"), c.base.join("outside")).unwrap();
        assert!(matches!(c.plan(scoopa::ID), Err("HARD_LINK")));
        let c = scoopa_fixture();
        fs::create_dir(source(&c).join(".git")).unwrap();
        assert!(matches!(c.plan(scoopa::ID), Err("PROTECTED_PATH")));
    }
    #[test]
    fn scoopa_pnpm_regular_alias_and_dangling_internal_link_survive_move() {
        let c = scoopa_fixture();
        let file =
            source(&c).join("frontend-runtime/node_modules/.pnpm/pkg/node_modules/pkg/value");
        let external = c.base.join("external-regular-name");
        fs::hard_link(&file, &external).unwrap();
        let original = node(&external).unwrap();
        let modules = source(&c).join("frontend-runtime/node_modules");
        std::os::windows::fs::symlink_dir(r".pnpm\pruned\node_modules\pkg", modules.join("pruned"))
            .unwrap();
        let inspected = c.plan(scoopa::ID).unwrap();
        assert_eq!(inspected["preservation"]["outside_regular_alias_names"], 1);
        assert_eq!(
            inspected["preservation"]["missing_internal_pnpm_targets"],
            1
        );
        let ticket: serde_json::Value =
            serde_json::from_slice(&fs::read(c.ticket_path()).unwrap()).unwrap();
        let security = ticket["nodes"]["frontend-runtime/node_modules/.pnpm/pkg/node_modules/pkg/value"]["security_sha256"].clone();
        move_scoopa_fixture(&c);
        c.finish(scoopa::ID).unwrap();
        assert_eq!(node(&external).unwrap(), original);
        assert_eq!(
            security,
            scoopa::security_hash(&external, &original).unwrap()
        );
        assert_eq!(fs::read(&external).unwrap(), b"keep me");
        assert!(
            fs::symlink_metadata(
                c.destination()
                    .join("files/frontend-runtime/node_modules/pruned")
            )
            .is_ok()
        );
    }
    #[test]
    fn scoopa_shared_alias_count_changes_refuse_finish() {
        let c = scoopa_fixture();
        let file =
            source(&c).join("frontend-runtime/node_modules/.pnpm/pkg/node_modules/pkg/value");
        fs::hard_link(&file, c.base.join("outside")).unwrap();
        c.plan(scoopa::ID).unwrap();
        move_scoopa_fixture(&c);
        fs::hard_link(
            c.destination()
                .join("files/frontend-runtime/node_modules/.pnpm/pkg/node_modules/pkg/value"),
            c.base.join("new-name"),
        )
        .unwrap();
        assert!(matches!(
            c.finish(scoopa::ID),
            Err("CONTENT_CHANGED_AFTER_PLAN")
        ));
        assert_eq!(c.load(scoopa::ID).unwrap().state, "owned");
    }
    fn scoopa_project_fixture() -> Fixture {
        let mut c = scoopa_fixture();
        c.project_alias_preservation_enabled = false;
        let old = source(&c);
        c.temp = c.base.join("source/deep");
        fs::create_dir_all(&c.temp).unwrap();
        fs::rename(old, source(&c)).unwrap();
        let mut r: Receipt = serde_json::from_slice(&fs::read(c.receipt_path()).unwrap()).unwrap();
        r.identity.root = c.temp.to_string_lossy().into_owned();
        r.identity.path = source(&c).to_string_lossy().into_owned();
        r.integrity = format!(
            "{:x}",
            Sha256::digest(serde_json::to_vec(&r.identity).unwrap())
        );
        fs::write(c.receipt_path(), serde_json::to_vec(&r).unwrap()).unwrap();
        c.project_alias_target = Some(
            c.base
                .parent()
                .unwrap()
                .join("Desktop/dev/ADJ/scoopa-wt/agent-todo-1242-3e3e30bb3a02/frontend"),
        );
        fs::create_dir_all(source(&c).join("frontend-runtime/node_modules/.pnpm/node_modules"))
            .unwrap();
        std::os::windows::fs::symlink_dir(
            scoopa::PROJECT_RAW,
            source(&c).join(scoopa::PROJECT_LINK),
        )
        .unwrap();
        c
    }
    #[test]
    fn scoopa_fixed_project_alias_is_not_followed_and_keeps_same_lexical_target() {
        let mut c = scoopa_project_fixture();
        let inspected = scoopa::inspect(&c, scoopa::ID).unwrap();
        assert_eq!(inspected["preservation"]["fixed_project_aliases"], 1);
        assert_eq!(inspected["fixed_project_aliases_present"], true);
        assert!(!c.ticket_path().exists());
        assert!(matches!(
            c.plan(scoopa::ID),
            Err("PROJECT_ALIAS_PRESERVATION_DISABLED")
        ));
        assert!(!c.ticket_path().exists());
        c.project_alias_preservation_enabled = true; // Re-enable preservation for this explicitly disabled fixture.
        c.plan(scoopa::ID).unwrap();
        move_scoopa_fixture(&c);
        c.project_alias_preservation_enabled = false;
        assert!(matches!(
            c.finish(scoopa::ID),
            Err("PROJECT_ALIAS_PRESERVATION_DISABLED")
        ));
        assert_eq!(c.load(scoopa::ID).unwrap().state, "owned");
        c.project_alias_preservation_enabled = true;
        assert_eq!(
            c.finish(scoopa::ID).unwrap()["preservation"]["fixed_project_aliases"],
            1
        );
        let c = scoopa_project_fixture();
        fs::remove_dir(source(&c).join(scoopa::PROJECT_LINK)).unwrap();
        std::os::windows::fs::symlink_dir(
            r"..\..\..\..\..\..\..\..\different",
            source(&c).join(scoopa::PROJECT_LINK),
        )
        .unwrap();
        assert!(matches!(c.plan(scoopa::ID), Err("EXTERNAL_LINK")));
        let mut c = scoopa_project_fixture();
        c.project_alias_preservation_enabled = true;
        c.quarantine = c.base.join("extra/deep/quarantine");
        assert!(matches!(
            c.plan(scoopa::ID),
            Err("PROJECT_ALIAS_TARGET_CHANGED")
        ));
        assert!(!c.ticket_path().exists());
    }
    #[test]
    fn scoopa_missing_pnpm_target_cannot_hide_case_or_short_name_alias() {
        for (target, expected) in [
            (
                r".pnpm\NODE_MODULES\scoopa-frontend\other",
                "LINK_TARGET_CASE_MISMATCH",
            ),
            (
                r".pnpm\node_modules\SCOOPA~1\other",
                "SHORT_NAME_MISSING_TARGET",
            ),
        ] {
            let c = scoopa_project_fixture();
            std::os::windows::fs::symlink_dir(
                target,
                source(&c).join("frontend-runtime/node_modules/bypass"),
            )
            .unwrap();
            assert_eq!(c.plan(scoopa::ID).unwrap_err(), expected);
            assert!(!c.ticket_path().exists());
        }
    }
    #[test]
    fn scoopa_production_alias_keeps_target_at_real_destination_depth() {
        let c = Config::production(scoopa::ID).unwrap();
        assert!(c.project_alias_preservation_enabled);
        assert!(
            !Config::production(ID)
                .unwrap()
                .project_alias_preservation_enabled
        );
        assert!(
            scoopa::project_alias(&c, &source(&c), scoopa::PROJECT_LINK, scoopa::PROJECT_RAW)
                .unwrap()
        );
        assert!(
            scoopa::project_alias(
                &c,
                &c.destination().join("files"),
                scoopa::PROJECT_LINK,
                scoopa::PROJECT_RAW
            )
            .unwrap()
        );
    }
    #[test]
    fn production_enables_fixed_project_preservation_only_for_scoopa() {
        for id in [
            ID,
            "76005ef45fe612479fd920447642497e",
            "38e3fbc1767b66eef9127f2a27567c8a",
            "438ee7d343a6c84558201e2778d2c890",
            "cfee3edefd3704d67c140ca6952dd7ce",
            "75cd71384cb094e47b19cf5d39b5b025",
            CARGO_ID,
            scoopa::ID,
        ] {
            assert_eq!(
                Config::production(id)
                    .unwrap()
                    .project_alias_preservation_enabled,
                id == scoopa::ID,
                "{id}"
            );
        }
        assert!(matches!(
            Config::production("00000000000000000000000000000000"),
            Err("SCOPE_NOT_AUTHORIZED")
        ));
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
    fn queue_cleanup_pins_original_identity_and_preserves_owned_clone() {
        let production = Config::production(QUEUE_ID).unwrap();
        assert_eq!(production.project, "cc-pc-tools-rs");
        assert_eq!(production.content, "queue-loop-tier");
        assert_eq!(
            production.pinned_identity,
            Some((2229119083, 23362423072822789))
        );
        assert_eq!(
            production.repos[0].head,
            "80c4bb370fce4e7645fafd8ac3eda1422168626b"
        );
        assert_eq!(
            production.repos[0].saved_ref,
            Some("refs/remotes/origin/feat/queue-loop-tier-1967")
        );
        let c = fixture_for(QUEUE_ID);
        let before = scan(&source(&c), &c.repos).unwrap();
        c.plan(c.id).unwrap();
        fs::rename(source(&c), c.destination().join("files")).unwrap();
        c.finish(c.id).unwrap();
        assert_eq!(
            before,
            scan(&c.destination().join("files"), &c.repos).unwrap()
        );
        assert!(!source(&c).exists());
        assert_eq!(c.load(c.id).unwrap().state, "quarantined");
        for field in ["project", "content", "path", "volume", "file_id"] {
            let c = fixture_for(QUEUE_ID);
            let mut r = c.load(c.id).unwrap();
            match field {
                "project" => r.identity.project = "another-task".into(),
                "content" => r.identity.content = "another-content".into(),
                "path" => r.identity.path.push_str("-other"),
                "volume" => r.identity.volume += 1,
                "file_id" => r.identity.file_id += 1,
                _ => unreachable!(),
            }
            r.integrity = format!(
                "{:x}",
                Sha256::digest(serde_json::to_vec(&r.identity).unwrap())
            );
            fs::write(c.receipt_path(), serde_json::to_vec(&r).unwrap()).unwrap();
            assert!(
                matches!(c.plan(c.id), Err("RECEIPT_SCOPE_MISMATCH")),
                "{field}"
            );
            assert!(!c.destination().exists());
        }
    }
    #[test]
    fn cargo_receipt_scope_and_fixed_identity_reject_recomputed_tampering() {
        let production = Config::production(CARGO_ID).unwrap();
        assert_eq!(
            production.pinned_identity,
            Some((2229119083, 2814749782464451))
        );
        for field in ["project", "content", "path", "volume", "file_id"] {
            let c = fixture_for(CARGO_ID);
            let mut r = c.load(c.id).unwrap();
            match field {
                "project" => r.identity.project = "different-task".into(),
                "content" => r.identity.content = "different-content".into(),
                "path" => r.identity.path.push_str("-other"),
                "volume" => r.identity.volume += 1,
                "file_id" => r.identity.file_id += 1,
                _ => unreachable!(),
            }
            r.integrity = format!(
                "{:x}",
                Sha256::digest(serde_json::to_vec(&r.identity).unwrap())
            );
            fs::write(c.receipt_path(), serde_json::to_vec(&r).unwrap()).unwrap();
            assert!(
                matches!(c.plan(c.id), Err("RECEIPT_SCOPE_MISMATCH")),
                "{field}"
            );
            assert!(!c.destination().exists());
        }
    }
    #[test]
    fn cargo_pruned_ref_requires_both_merge_reachability_checks() {
        for failure in [
            "none",
            "missing-main",
            "merge-not-in-main",
            "main-before-merge",
            "changed-ref",
        ] {
            let c = fixture_for(CARGO_ID);
            let path = source(&c).join("work");
            let repo = &c.repos[0];
            git(&path, &["update-ref", "-d", repo.saved_ref.unwrap()]).unwrap();
            match failure {
                "none" => (),
                "missing-main" => {
                    git(&path, &["update-ref", "-d", "refs/remotes/origin/main"]).unwrap();
                }
                "merge-not-in-main" => {
                    let tree = format!("{}^{{tree}}", repo.head);
                    let unrelated = git(
                        &path,
                        &[
                            "-c",
                            "user.name=Fixture",
                            "-c",
                            "user.email=fixture@example.invalid",
                            "commit-tree",
                            &tree,
                            "-m",
                            "unrelated main",
                        ],
                    )
                    .unwrap();
                    git(
                        &path,
                        &["update-ref", "refs/remotes/origin/main", &unrelated],
                    )
                    .unwrap();
                    // The fixed expected merge must not be an ancestor of this main.
                }
                "main-before-merge" => {
                    git(
                        &path,
                        &["update-ref", "refs/remotes/origin/main", &repo.head],
                    )
                    .unwrap();
                }
                "changed-ref" => {
                    git(
                        &path,
                        &[
                            "update-ref",
                            repo.saved_ref.unwrap(),
                            repo.saved_merge.as_deref().unwrap(),
                        ],
                    )
                    .unwrap();
                }
                _ => unreachable!(),
            }
            if failure == "none" {
                c.plan(c.id).unwrap();
            } else {
                assert!(
                    matches!(c.plan(c.id), Err("GIT_UNPUSHED_COMMITS")),
                    "{failure}"
                );
                assert!(!c.destination().exists());
            }
        }
        let mut c = fixture_for(CARGO_ID);
        let path = source(&c).join("work");
        let repo = &mut c.repos[0];
        git(&path, &["update-ref", "-d", repo.saved_ref.unwrap()]).unwrap();
        let tree = format!("{}^{{tree}}", repo.head);
        let unrelated = git(
            &path,
            &[
                "-c",
                "user.name=Fixture",
                "-c",
                "user.email=fixture@example.invalid",
                "commit-tree",
                &tree,
                "-m",
                "unrelated merge",
            ],
        )
        .unwrap();
        git(
            &path,
            &["update-ref", "refs/remotes/origin/main", &unrelated],
        )
        .unwrap();
        repo.saved_merge = Some(unrelated);
        assert!(matches!(c.plan(c.id), Err("GIT_UNPUSHED_COMMITS")));
    }
    #[test]
    fn cargo_head_origin_and_external_admin_are_rejected() {
        for failure in ["head", "origin", "admin", "common"] {
            let mut c = fixture_for(CARGO_ID);
            let path = source(&c).join("work");
            match failure {
                "head" => {
                    git(
                        &path,
                        &[
                            "-c",
                            "user.name=Fixture",
                            "-c",
                            "user.email=fixture@example.invalid",
                            "commit",
                            "--allow-empty",
                            "-m",
                            "unsaved commit",
                        ],
                    )
                    .unwrap();
                }
                "origin" => {
                    git(
                        &path,
                        &[
                            "remote",
                            "set-url",
                            "origin",
                            "https://example.invalid/repo.git",
                        ],
                    )
                    .unwrap();
                }
                "admin" | "common" => {
                    let replacement = c.temp.join("unexpected-admin");
                    fs::create_dir(&replacement).unwrap();
                    match &mut c.repos[0].kind {
                        RepoKind::Linked { admin, common } => {
                            if failure == "admin" {
                                *admin = replacement;
                            } else {
                                *common = replacement;
                            }
                        }
                        _ => unreachable!(),
                    }
                }
                _ => unreachable!(),
            }
            let expected = match failure {
                "head" => "GIT_HEAD_CHANGED",
                "origin" => "GIT_ORIGIN_CHANGED",
                _ => "EXTERNAL_GITDIR",
            };
            assert_eq!(c.plan(c.id).err(), Some(expected), "{failure}");
            assert!(!c.destination().exists());
        }
    }
    #[test]
    fn cargo_move_preserves_inventory_and_repairs_only_fixed_backlink() {
        let c = fixture_for(CARGO_ID);
        let common = match &c.repos[0].kind {
            RepoKind::Linked { common, .. } => common,
            _ => unreachable!(),
        };
        let sibling = common.join("worktrees/sibling");
        fs::create_dir(&sibling).unwrap();
        fs::write(sibling.join("gitdir"), b"fixture other worktree\n").unwrap();
        let refs = git(common.parent().unwrap(), &["show-ref"]).unwrap();
        let plan = c.plan(c.id).unwrap();
        assert!(
            plan["powershell_command"]
                .as_str()
                .unwrap()
                .starts_with("Move-Item -LiteralPath")
        );
        fs::rename(source(&c), c.destination().join("files")).unwrap();
        let result = c.finish(c.id).unwrap();
        assert_eq!(result["inventory_verified"], true);
        c.finish(c.id).unwrap();
        assert_eq!(c.load(c.id).unwrap().state, "quarantined");
        assert_eq!(git(common.parent().unwrap(), &["show-ref"]).unwrap(), refs);
        assert_eq!(
            fs::read(sibling.join("gitdir")).unwrap(),
            b"fixture other worktree\n"
        );
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
    fn cargo_replaced_destination_never_commits_receipt_or_backlink() {
        let c = fixture_for(CARGO_ID);
        c.plan(c.id).unwrap();
        let admin = match &c.repos[0].kind {
            RepoKind::Linked { admin, .. } => admin,
            _ => unreachable!(),
        };
        let original = fs::read(admin.join("gitdir")).unwrap();
        fs::rename(source(&c), c.temp.join("preserved-original")).unwrap();
        fs::create_dir(c.destination().join("files")).unwrap();
        assert!(matches!(c.finish(c.id), Err("TARGET_IDENTITY_CHANGED")));
        assert_eq!(c.load(c.id).unwrap().state, "owned");
        assert_eq!(fs::read(admin.join("gitdir")).unwrap(), original);
    }
    #[test]
    fn cargo_broken_ref_or_ref_backend_failure_is_not_absence() {
        for failure in ["missing-object", "invalid-ref", "invalid-packed-refs"] {
            let c = fixture_for(CARGO_ID);
            let path = source(&c).join("work");
            let repo = &c.repos[0];
            let common = match &repo.kind {
                RepoKind::Linked { common, .. } => common,
                _ => unreachable!(),
            };
            git(&path, &["update-ref", "-d", repo.saved_ref.unwrap()]).unwrap();
            if failure == "invalid-packed-refs" {
                fs::write(common.join("packed-refs"), b"invalid packed ref\n").unwrap();
            } else {
                fs::create_dir_all(common.join(repo.saved_ref.unwrap()).parent().unwrap()).unwrap();
                fs::write(
                    common.join(repo.saved_ref.unwrap()),
                    if failure == "missing-object" {
                        b"1111111111111111111111111111111111111111\n".as_slice()
                    } else {
                        b"invalid ref\n".as_slice()
                    },
                )
                .unwrap();
            }
            assert!(
                validate_saved_commit(&path, repo, repo.saved_ref.unwrap()).is_err(),
                "{failure}"
            );
            assert!(!c.destination().exists());
        }
    }
    #[test]
    fn cargo_replacement_history_and_grafts_cannot_fake_saved_merge() {
        let mut c = fixture_for(CARGO_ID);
        let path = source(&c).join("work");
        let repo = &mut c.repos[0];
        let head = repo.head.clone();
        let real_merge = repo.saved_merge.clone().unwrap();
        git(&path, &["update-ref", "-d", repo.saved_ref.unwrap()]).unwrap();
        let tree = format!("{head}^{{tree}}");
        let unrelated = git(
            &path,
            &[
                "-c",
                "user.name=Fixture",
                "-c",
                "user.email=fixture@example.invalid",
                "commit-tree",
                &tree,
                "-m",
                "unrelated replacement",
            ],
        )
        .unwrap();
        git(
            &path,
            &[
                "update-ref",
                &format!("refs/replace/{unrelated}"),
                &real_merge,
            ],
        )
        .unwrap();
        git(
            &path,
            &["update-ref", "refs/remotes/origin/main", &unrelated],
        )
        .unwrap();
        repo.saved_merge = Some(unrelated);
        assert!(matches!(c.plan(c.id), Err("GIT_UNPUSHED_COMMITS")));
        let c = fixture_for(CARGO_ID);
        let common = match &c.repos[0].kind {
            RepoKind::Linked { common, .. } => common,
            _ => unreachable!(),
        };
        fs::write(common.join("info/grafts"), b"fixture graft\n").unwrap();
        assert!(matches!(
            c.plan(c.id),
            Err("EXTERNAL_OR_SHARED_GIT_METADATA")
        ));
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
        let mut c = fixture_for(RESUME_ID);
        // Both a longer quarantine prefix and the content inventory must work.
        // The old helper failed in Git when its loose-object path crossed 260.
        c.quarantine = c.base.join("q");
        let initial = c
            .destination()
            .join("files/source")
            .as_os_str()
            .encode_wide()
            .count();
        assert!(initial < 240);
        c.quarantine = c.base.join("q".repeat(241 - initial));
        assert_eq!(
            c.destination()
                .join("files/source")
                .as_os_str()
                .encode_wide()
                .count(),
            240
        );
        assert!(
            c.destination()
                .join("files/source/.git/objects/aa/01234567890123456789012345678901234567")
                .as_os_str()
                .encode_wide()
                .count()
                > 260
        );
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
    fn git_startup_root_limit_is_rejected_before_preparing_a_move() {
        let mut c = fixture_for(RESUME_ID);
        c.quarantine = c.base.join("q");
        let initial = c
            .destination()
            .join("files/source")
            .as_os_str()
            .encode_wide()
            .count();
        assert!(initial < 241);
        c.quarantine = c.base.join("q".repeat(242 - initial));
        assert_eq!(
            c.destination()
                .join("files/source")
                .as_os_str()
                .encode_wide()
                .count(),
            241
        );
        assert!(matches!(c.plan(c.id), Err("GIT_DESTINATION_ROOT_TOO_LONG")));
        assert!(!c.ticket_path().exists());
        assert!(!c.destination().exists());
        assert!(source(&c).exists());
        assert_eq!(c.load(c.id).unwrap().state, "owned");
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
    fn claude_routing_profile_preserves_only_its_runtime_registry_and_backlink() {
        let c = fixture_for("75cd71384cb094e47b19cf5d39b5b025");
        let work = source(&c).join("worktree");
        for relative in c.repos[0].allowed_untracked {
            let path = work.join(relative);
            fs::create_dir_all(path.parent().unwrap()).unwrap();
            fs::write(path, b"runtime fixture\n").unwrap();
        }
        let admin = match &c.repos[0].kind {
            RepoKind::Linked { admin, .. } => admin,
            _ => unreachable!(),
        };
        let before = bounded_text(&admin.join("gitdir")).unwrap();
        c.plan(c.id).unwrap();
        assert_eq!(bounded_text(&admin.join("gitdir")).unwrap(), before);
        fs::rename(source(&c), c.destination().join("files")).unwrap();
        c.finish(c.id).unwrap();
        assert_eq!(c.load(c.id).unwrap().state, "quarantined");
        let moved = c.destination().join("files/worktree");
        assert!(same(
            Path::new(&bounded_text(&admin.join("gitdir")).unwrap()),
            &moved.join(".git")
        ));
        for relative in c.repos[0].allowed_untracked {
            assert_eq!(
                fs::read(moved.join(relative)).unwrap(),
                b"runtime fixture\n"
            );
        }
    }
    #[test]
    fn claude_routing_profile_rejects_other_untracked_or_modified_source_before_planning() {
        for (name, tracked) in [("unpublished.rs", false), ("source.txt", true)] {
            let c = fixture_for("75cd71384cb094e47b19cf5d39b5b025");
            fs::write(source(&c).join("worktree").join(name), b"unsaved work\n").unwrap();
            assert!(
                matches!(c.plan(c.id), Err("GIT_TRACKED_CHANGES")),
                "tracked={tracked}"
            );
            assert!(!c.ticket_path().exists());
            assert!(!c.destination().exists());
        }
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
