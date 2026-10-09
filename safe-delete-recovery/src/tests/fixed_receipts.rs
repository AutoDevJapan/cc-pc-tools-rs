//! Isolated fixtures for the three fixed receipts. No real receipt or root is touched.
use super::{Fixture, fixture_for, source};
use crate::seller2140 as seller;
use crate::*;

const ORIGIN: &str = "https://github.com/AutoDevJapan/GoDD-Matrix.git";

fn commit(path: &Path) -> String {
    git(path, &["init", "-b", "main"]).unwrap();
    fs::write(path.join("source.txt"), b"public fixture\n").unwrap();
    fs::write(path.join(".gitignore"), b"node_modules/\n").unwrap();
    fs::create_dir(path.join(".cursor")).unwrap();
    fs::write(path.join(".cursor/rule.txt"), b"tracked\n").unwrap();
    git(path, &["add", "."]).unwrap();
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
    let head = git(path, &["rev-parse", "HEAD"]).unwrap();
    git(path, &["remote", "add", "origin", ORIGIN]).unwrap();
    git(path, &["update-ref", "refs/remotes/origin/main", &head]).unwrap();
    head
}
fn exclude(repo: &Path, names: &str) {
    fs::write(repo.join(".git/info/exclude"), names).unwrap();
}
fn clone_fixture() -> Fixture {
    let mut c = fixture_for(seller::MATRIX);
    let root = source(&c);
    let head = commit(&root);
    exclude(&root, "escape\nnew-data\n");
    c.seller = vec![seller::RepoSpec {
        dir: "",
        origin: ORIGIN,
        head,
        untracked: &[],
        link: None,
    }];
    c
}
fn outside(c: &Fixture, name: &str) -> PathBuf {
    let dir = c.base.join(name);
    fs::create_dir_all(dir.join(".git")).unwrap();
    fs::write(dir.join("data"), b"outside").unwrap();
    dir
}
// A dependency link whose target holds a `.git` that a traversal would reject.
fn link_fixture() -> (Fixture, PathBuf, PathBuf) {
    let c = clone_fixture();
    let target = outside(&c, "outside");
    let modules = source(&c).join("node_modules");
    fs::create_dir(&modules).unwrap();
    let link = modules.join("dep");
    std::os::windows::fs::symlink_dir(&target, &link).unwrap();
    (c, link, target)
}
fn move_root(c: &Config) {
    // The caller's ordinary directory rename; never a production API.
    fs::rename(source(c), c.destination().join("files")).unwrap();
}

#[test]
fn only_three_fixed_ids_and_exact_receipt_constants_are_accepted() {
    assert!(matches!(
        Config::production("00000000000000000000000000000000"),
        Err("SCOPE_NOT_AUTHORIZED")
    ));
    for id in [seller::MATRIX, seller::GENESIS, seller::AUDIT] {
        let c = Config::production(id).unwrap();
        let f = seller::fixed(id).unwrap();
        assert!(c.repos.is_empty() && !c.seller.is_empty());
        let (volume, file_id) = c.pinned_identity.unwrap();
        let identity = Identity {
            version: 2,
            id: id.into(),
            root: c.temp.to_string_lossy().into_owned(),
            path: source(&c).to_string_lossy().into_owned(),
            volume,
            file_id,
            created_at: f.created_at.into(),
            project: c.project.into(),
            content: c.content.into(),
        };
        assert_eq!(
            format!(
                "{:x}",
                Sha256::digest(serde_json::to_vec(&identity).unwrap())
            ),
            f.integrity
        );
        assert!(matches!(c.plan(scoopa::ID), Err("SCOPE_NOT_AUTHORIZED")));
    }
    assert!(Config::production(scoopa::ID).unwrap().seller.is_empty());
    assert!(Config::production(ID).unwrap().seller.is_empty());
}

#[test]
fn dependency_link_is_preserved_raw_and_never_entered() {
    let (c, link, target) = link_fixture();
    let raw = seller::reparse_data(&link).unwrap();
    let before = node_no_follow(&link).unwrap();
    let inspected = seller::inspect(&c, c.id).unwrap();
    assert_eq!(inspected["links"], 1);
    assert!(!c.ticket_path().exists());
    let plan = c.plan(c.id).unwrap();
    assert!(
        plan["powershell_command"]
            .as_str()
            .unwrap()
            .starts_with("Move-Item -LiteralPath")
    );
    assert!(matches!(c.finish(c.id), Err("TARGET_STILL_EXISTS")));
    move_root(&c);
    let moved = c.destination().join("files/node_modules/dep");
    let result = c.finish(c.id).unwrap();
    assert_eq!(result["inventory_verified"], true);
    assert_eq!(c.load(c.id).unwrap().state, "quarantined");
    assert_eq!(seller::reparse_data(&moved).unwrap(), raw);
    assert_eq!(node_no_follow(&moved).unwrap(), before);
    assert_eq!(fs::read(target.join("data")).unwrap(), b"outside");
    assert_eq!(c.finish(c.id).unwrap(), result);
}

#[test]
fn changed_link_data_inventory_and_destination_are_rejected() {
    let (c, link, _) = link_fixture();
    c.plan(c.id).unwrap();
    move_root(&c);
    let moved = c.destination().join("files/node_modules/dep");
    assert!(link.symlink_metadata().is_err());
    fs::remove_dir(&moved).unwrap();
    std::os::windows::fs::symlink_dir(outside(&c, "other-target"), &moved).unwrap();
    assert!(matches!(c.finish(c.id), Err("CONTENT_CHANGED_AFTER_PLAN")));
    assert_eq!(c.load(c.id).unwrap().state, "owned");

    let c = clone_fixture();
    c.plan(c.id).unwrap();
    move_root(&c);
    fs::write(c.destination().join("files/new-data"), b"unexpected").unwrap();
    assert!(matches!(c.finish(c.id), Err("CONTENT_CHANGED_AFTER_PLAN")));
    assert_eq!(c.load(c.id).unwrap().state, "owned");

    let c = clone_fixture();
    c.plan(c.id).unwrap();
    move_root(&c);
    let files = c.destination().join("files");
    fs::rename(&files, c.base.join("original")).unwrap();
    fs::create_dir(&files).unwrap();
    assert!(matches!(c.finish(c.id), Err("TARGET_IDENTITY_CHANGED")));
}

#[test]
fn links_outside_dependencies_nested_git_and_external_hardlinks_are_refused() {
    let c = clone_fixture();
    let target = outside(&c, "outside");
    std::os::windows::fs::symlink_dir(&target, source(&c).join("escape")).unwrap();
    assert!(matches!(c.plan(c.id), Err("LINK_OUTSIDE_DEPENDENCIES")));

    let c = clone_fixture();
    let nested = source(&c).join("node_modules/pkg/.git");
    fs::create_dir_all(nested).unwrap();
    assert!(matches!(c.plan(c.id), Err("UNDECLARED_GIT_METADATA")));

    let c = clone_fixture();
    fs::hard_link(
        source(&c).join("source.txt"),
        c.base.join("external-name.txt"),
    )
    .unwrap();
    assert!(matches!(c.plan(c.id), Err("HARD_LINK")));
    assert!(!c.ticket_path().exists() && !c.destination().exists());
}

#[test]
fn unexpected_untracked_and_tracked_changes_are_refused_declared_ones_kept() {
    let c = clone_fixture();
    fs::write(source(&c).join("stray.txt"), b"x").unwrap();
    assert!(matches!(c.plan(c.id), Err("GIT_TRACKED_CHANGES")));
    let c = clone_fixture();
    fs::write(source(&c).join("source.txt"), b"unsaved").unwrap();
    assert!(matches!(c.plan(c.id), Err("GIT_TRACKED_CHANGES")));
    assert!(!c.ticket_path().exists() && !c.destination().exists());

    let mut c = clone_fixture();
    let root = source(&c);
    fs::create_dir(root.join("target-seller-contact-2132")).unwrap();
    fs::write(root.join("target-seller-contact-2132/build"), b"x").unwrap();
    fs::create_dir(root.join(".cursor/state")).unwrap();
    fs::write(root.join(".cursor/state/session"), b"x").unwrap();
    assert!(matches!(c.plan(c.id), Err("GIT_TRACKED_CHANGES")));
    c.seller[0].untracked = &["target-seller-contact-2132/", ".cursor/state/"];
    c.plan(c.id).unwrap();
    move_root(&c);
    c.finish(c.id).unwrap();
    assert!(
        c.destination()
            .join("files/.cursor/state/session")
            .is_file()
    );
}

#[test]
fn local_history_stash_and_wrong_head_or_origin_are_refused() {
    let c = clone_fixture();
    let root = source(&c);
    git(&root, &["update-ref", "refs/stash", "HEAD"]).unwrap();
    assert!(matches!(c.plan(c.id), Err("GIT_STASH")));
    let mut c = clone_fixture();
    c.seller[0].head = "0".repeat(40);
    assert!(matches!(c.plan(c.id), Err("GIT_HEAD_CHANGED")));
    let mut c = clone_fixture();
    c.seller[0].origin = "https://github.com/AutoDevJapan/GoDD-Genesis.git";
    assert!(matches!(c.plan(c.id), Err("GIT_ORIGIN_CHANGED")));
}

const NAMES: [&str; 3] = ["QaJaRuEn", "oss-mcp", "alt-line"];
fn linked_fixture() -> (Fixture, PathBuf) {
    let mut c = fixture_for(seller::AUDIT);
    let root = source(&c);
    let common = c.base.join("common");
    fs::create_dir(&common).unwrap();
    let head = commit(&common);
    let mut repos = Vec::new();
    for (i, name) in NAMES.into_iter().enumerate() {
        let path = root.join(name);
        git(
            &common,
            &[
                "worktree",
                "add",
                "-b",
                &format!("wt-{i}"),
                path.to_str().unwrap(),
            ],
        )
        .unwrap();
        repos.push(seller::RepoSpec {
            dir: name,
            origin: ORIGIN,
            head: head.clone(),
            untracked: &[],
            link: Some((
                common.join(".git/worktrees").join(name),
                common.join(".git"),
            )),
        });
    }
    // Another owner's worktree of the same repository must stay untouched.
    let other = c.base.join("other");
    git(
        &common,
        &["worktree", "add", "-b", "other", other.to_str().unwrap()],
    )
    .unwrap();
    c.seller = repos;
    (c, common)
}
fn admin_gitdir(c: &Config, i: usize) -> PathBuf {
    c.seller[i].link.as_ref().unwrap().0.join("gitdir")
}

#[test]
fn three_backlinks_resume_after_partial_repair_and_other_worktrees_stay() {
    let (c, common) = linked_fixture();
    let other_gitdir = common.join(".git/worktrees/other/gitdir");
    let other_before = (
        fs::read(&other_gitdir).unwrap(),
        node(&other_gitdir).unwrap(),
    );
    let refs_before = git(&common, &["for-each-ref"]).unwrap();
    let plan = c.plan(c.id).unwrap();
    assert_eq!(plan["worktree_backlinks"], 3);
    assert_eq!(c.plan(c.id).unwrap(), plan);
    move_root(&c);
    // Interrupted after the first exchange: the saved replacement is already in place.
    let admin = c.seller[0].link.as_ref().unwrap().0.clone();
    fs::rename(admin.join("gitdir.todo2140-repair"), admin.join("gitdir")).unwrap();
    let result = c.finish(c.id).unwrap();
    assert_eq!(result["worktree_backlinks_repaired"], 3);
    let files = c.destination().join("files");
    for (i, name) in NAMES.into_iter().enumerate() {
        let text = bounded_text(&admin_gitdir(&c, i)).unwrap();
        assert!(same(Path::new(&text), &files.join(name).join(".git")));
        let admin = c.seller[i].link.as_ref().unwrap().0.clone();
        assert!(!admin.join("gitdir.todo2140-repair").exists());
    }
    assert_eq!(
        (
            fs::read(&other_gitdir).unwrap(),
            node(&other_gitdir).unwrap()
        ),
        other_before
    );
    assert_eq!(git(&common, &["for-each-ref"]).unwrap(), refs_before);
    assert_eq!(c.finish(c.id).unwrap(), result);
    assert_eq!(c.load(c.id).unwrap().state, "quarantined");
}

#[test]
fn unexpected_backlink_blocks_success() {
    let (c, _) = linked_fixture();
    c.plan(c.id).unwrap();
    move_root(&c);
    fs::write(admin_gitdir(&c, 1), b"C:/elsewhere/.git\n").unwrap();
    assert!(matches!(c.finish(c.id), Err("WORKTREE_BACKLINK_CHANGED")));
    assert_eq!(c.load(c.id).unwrap().state, "owned");
    let (mut c, _) = linked_fixture();
    let mut spoofed = c.seller[0].link.clone().unwrap();
    spoofed.1 = c.base.join("elsewhere/.git");
    c.seller[0].link = Some(spoofed);
    assert!(c.plan(c.id).is_err());
    assert!(!c.ticket_path().exists());
}

#[test]
fn outside_dependency_alias_is_preserved_without_changing_the_outside_name() {
    let c = clone_fixture();
    let root = source(&c);
    fs::create_dir(root.join("node_modules")).unwrap();
    let outside = c.base.join("store-file");
    fs::write(&outside, b"immutable fixture dependency").unwrap();
    let alias = root.join("node_modules/dependency");
    fs::hard_link(&outside, &alias).unwrap();
    let before = node(&outside).unwrap();
    assert_eq!(
        seller::inspect(&c, c.id).unwrap()["outside_dependency_aliases"],
        1
    );
    c.plan(c.id).unwrap();
    move_root(&c);
    let result = c.finish(c.id).unwrap();
    assert_eq!(result["outside_dependency_aliases"], 1);
    assert_eq!(node(&outside).unwrap(), before);
    assert_eq!(
        node(&c.destination().join("files/node_modules/dependency")).unwrap(),
        before
    );
}
