//! Independent source copies for projects without Git; runtime data stays in the original.
//! In addition to the source allowlist, copy literal include_str!/include_bytes!/include!
//! inputs (including raw/escaped strings) admitted by the same directory/file exclusions.
//! New assets are limited to source/fixture/asset directories and known build formats;
//! a literal include is not permission to copy an arbitrary file.
//! Comments and string contents are not macro calls. Computed paths remain outside this
//! static discovery policy. Existing snapshots are reused without overwriting edits.
use serde_json::{Value, json};
use std::{
    collections::{BTreeMap, BTreeSet},
    fs, io,
    io::Read,
    path::{Component, Path, PathBuf},
    time::{SystemTime, UNIX_EPOCH},
};
struct Staging(PathBuf);
impl Drop for Staging {
    fn drop(&mut self) {
        if self.0.exists() {
            let _ = fs::remove_dir_all(&self.0);
        }
    }
}
fn source_file(path: &Path) -> bool {
    let name = path.file_name().and_then(|n| n.to_str()).unwrap_or("");
    let extension = path.extension().and_then(|n| n.to_str()).unwrap_or("");
    matches!(
        extension,
        "rs" | "go"
            | "c"
            | "h"
            | "cpp"
            | "md"
            | "toml"
            | "ps1"
            | "vbs"
            | "cmd"
            | "bat"
            | "ts"
            | "tsx"
            | "js"
            | "jsx"
            | "html"
            | "css"
            | "sql"
            | "yaml"
            | "yml"
    ) && !matches!(name, "credentials.toml" | "config.toml")
        || matches!(
            name,
            "Cargo.lock"
                | "go.mod"
                | "go.sum"
                | "Makefile"
                | "GNUmakefile"
                | "CMakeLists.txt"
                | "package.json"
                | "package-lock.json"
                | "Dockerfile"
                | "client-projects.txt"
        )
}
// Apply these exclusions before discovering embedded assets as well as before copying.
// An include macro must never turn a runtime credential or client directory into source.
fn excluded_file(path: &Path) -> bool {
    let name = path
        .file_name()
        .unwrap_or_default()
        .to_string_lossy()
        .to_ascii_lowercase();
    let stem = path
        .file_stem()
        .unwrap_or_default()
        .to_string_lossy()
        .to_ascii_lowercase();
    let extension = path
        .extension()
        .unwrap_or_default()
        .to_string_lossy()
        .to_ascii_lowercase();
    name.starts_with('.')
        || matches!(
            name.as_str(),
            "id_rsa" | "id_dsa" | "id_ecdsa" | "id_ed25519" | "authorized_keys" | "known_hosts"
        )
        || (matches!(
            extension.as_str(),
            "txt" | "json" | "jsonl" | "toml" | "yaml" | "yml" | ""
        ) && [
            "service-account",
            "service_account",
            "client-secret",
            "client_secret",
            "credential",
            "secret",
            "token",
        ]
        .iter()
        .any(|part| stem.contains(part)))
        || matches!(name.as_str(), "config.toml" | "credentials.toml")
        || (matches!(
            stem.as_str(),
            "config" | "credentials" | "auth" | "secrets" | "secret" | "tokens" | "token"
        ) && matches!(
            path.extension()
                .unwrap_or_default()
                .to_string_lossy()
                .to_ascii_lowercase()
                .as_str(),
            "txt" | "json" | "jsonl" | "toml" | "yaml" | "yml"
        ))
        || matches!(
            path.extension()
                .unwrap_or_default()
                .to_string_lossy()
                .to_ascii_lowercase()
                .as_str(),
            "pem" | "key" | "p12" | "pfx"
        )
}
fn runtime_directory(path: &Path) -> bool {
    path.parent()
        .into_iter()
        .flat_map(Path::components)
        .any(|part| {
            matches!(
                part.as_os_str()
                    .to_string_lossy()
                    .to_ascii_lowercase()
                    .as_str(),
                "auth" | "credentials" | "secrets" | "runtime" | "logs" | "jobs" | "data"
            )
        })
}
fn admitted_file(path: &Path, embedded: bool) -> bool {
    // One policy for initial selection and include-discovered additions.
    // Keep Rust modules in src/auth or src/data without admitting runtime settings.
    !excluded_file(path)
        && ((source_file(path)
            && (!runtime_directory(path) || path.extension().is_some_and(|ext| ext == "rs")))
            || (embedded && build_asset_file(path)))
}
fn build_asset_file(path: &Path) -> bool {
    let parts: Vec<_> = path
        .components()
        .filter_map(|part| match part {
            Component::Normal(name) => Some(name.to_string_lossy().to_ascii_lowercase()),
            _ => None,
        })
        .collect();
    let directories = &parts[..parts.len().saturating_sub(1)];
    if runtime_directory(path) {
        return false;
    }
    let source = directories.iter().any(|name| name == "src");
    let fixtures = directories
        .windows(2)
        .any(|parts| parts == ["tests", "fixtures"]);
    let assets = directories.iter().any(|name| name == "assets");
    let extension = path
        .extension()
        .unwrap_or_default()
        .to_string_lossy()
        .to_ascii_lowercase();
    (source || fixtures || assets)
        && matches!(
            extension.as_str(),
            "txt"
                | "json"
                | "jsonl"
                | "bin"
                | "inc"
                | "png"
                | "jpg"
                | "jpeg"
                | "svg"
                | "webp"
                | "ico"
                | "woff"
                | "woff2"
        )
}
fn client_directories(origin: &Path) -> io::Result<BTreeSet<String>> {
    let mut names = BTreeSet::new();
    for registry in [
        PathBuf::from("D:/CC/Tools/client-projects.txt"),
        origin.join("client-projects.txt"),
    ] {
        match fs::read_to_string(registry) {
            Ok(text) => names.extend(
                text.lines()
                    .map(str::trim)
                    .filter(|line| !line.is_empty() && !line.starts_with('#'))
                    .map(str::to_ascii_lowercase),
            ),
            Err(e) if e.kind() == io::ErrorKind::NotFound => (),
            Err(e) => return Err(e),
        }
    }
    Ok(names)
}
fn inventory_tree(
    origin: &Path,
    relative: &Path,
    clients: &BTreeSet<String>,
    inventory: &mut BTreeMap<PathBuf, u64>,
) -> io::Result<()> {
    for entry in fs::read_dir(origin.join(relative))? {
        let entry = entry?;
        let name = entry.file_name();
        let name_text = name.to_string_lossy();
        // Reject registered client names before metadata or Cargo.toml probes.
        if clients.contains(&name_text.to_ascii_lowercase()) {
            continue;
        }
        let metadata = fs::symlink_metadata(entry.path())?;
        #[cfg(windows)]
        {
            use std::os::windows::fs::MetadataExt;
            if metadata.file_attributes() & 0x400 != 0 {
                continue;
            }
        }
        if metadata.file_type().is_symlink() {
            continue;
        }
        let next = relative.join(&name);
        if metadata.is_dir() {
            // The PC tools directory also contains installed vendor SDKs and data.
            // Snapshot its source crates, rather than traversing those installations.
            let pc_tools = origin
                .to_string_lossy()
                .trim_start_matches(r"\\?\")
                .replace('\\', "/")
                .trim_end_matches('/')
                .eq_ignore_ascii_case("D:/CC/Tools");
            if relative.as_os_str().is_empty()
                && pc_tools
                && name_text != "rs"
                && !entry.path().join("Cargo.toml").is_file()
            {
                continue;
            }
            if pc_tools && relative == Path::new("rs") && name_text == "verification" {
                continue;
            }
            let lower_name = name_text.to_ascii_lowercase();
            let source_directory = relative.components().any(|part| part.as_os_str() == "src");
            if clients.contains(&lower_name)
                || name_text.starts_with('.')
                || matches!(
                    lower_name.as_str(),
                    "target" | "node_modules" | "agent-workspaces"
                )
                || (!source_directory
                    && matches!(
                        lower_name.as_str(),
                        "data" | "logs" | "jobs" | "runtime" | "credentials" | "secrets" | "auth"
                    ))
                || (relative.as_os_str().is_empty()
                    && matches!(
                        name_text.as_ref(),
                        "jobs"
                            | "logs"
                            | "verification"
                            | "cleanup"
                            | "models"
                            | "agent-workspaces"
                            | "codex-jobs"
                            | "discord-ask"
                            | "todo"
                            | "scheduler"
                    ))
            {
                continue;
            }
            inventory_tree(origin, &next, clients, inventory)?;
        } else if metadata.is_file() && !excluded_file(&next) {
            inventory.insert(next, metadata.len());
        }
    }
    Ok(())
}

fn embedded_paths(tokens: proc_macro2::TokenStream, paths: &mut Vec<(String, bool)>) {
    use proc_macro2::TokenTree;
    use syn::parse::Parser;
    let tokens: Vec<_> = tokens.into_iter().collect();
    let mut inert_groups = BTreeSet::new();
    for (i, token) in tokens.iter().enumerate() {
        if let TokenTree::Ident(name) = token {
            if matches!(name.to_string().as_str(), "stringify" | "quote")
                && matches!(tokens.get(i + 1), Some(TokenTree::Punct(p)) if p.as_char() == '!')
            {
                inert_groups.insert(i + 2);
            }
            if matches!(
                name.to_string().as_str(),
                "include_str" | "include_bytes" | "include"
            ) && matches!(tokens.get(i + 1), Some(TokenTree::Punct(p)) if p.as_char() == '!')
            {
                if let Some(TokenTree::Group(group)) = tokens.get(i + 2) {
                    let literal = |input: syn::parse::ParseStream<'_>| {
                        let path: syn::LitStr = input.parse()?;
                        if input.peek(syn::Token![,]) {
                            input.parse::<syn::Token![,]>()?;
                        }
                        if !input.is_empty() {
                            return Err(input.error("expected a literal include path"));
                        }
                        Ok(path.value())
                    };
                    if let Ok(path) = literal.parse2(group.stream()) {
                        paths.push((path, name == "include"));
                    }
                }
            }
        }
        if let TokenTree::Group(group) = token {
            if inert_groups.contains(&i) {
                continue;
            }
            embedded_paths(group.stream(), paths);
        }
    }
}
fn relative_include(source: &Path, included: &str) -> Option<PathBuf> {
    // Normalize lexically: never follow a link or inspect a path outside the inventory.
    // Reject Windows absolute/drive/stream paths even when tests run on another OS.
    if included.contains(':') || included.starts_with(['/', '\\']) {
        return None;
    }
    let mut result = source.parent()?.to_path_buf();
    for part in Path::new(&included.replace('\\', "/")).components() {
        match part {
            Component::Normal(name) => result.push(name),
            Component::CurDir => (),
            Component::ParentDir if result.pop() => (),
            _ => return None,
        }
    }
    Some(result)
}
fn copy_tree(origin: &Path, work: &Path, baseline: &Path) -> io::Result<(Vec<String>, u64)> {
    let mut inventory = BTreeMap::new();
    inventory_tree(
        origin,
        Path::new(""),
        &client_directories(origin)?,
        &mut inventory,
    )?;
    let mut selected: BTreeSet<PathBuf> = inventory
        .keys()
        .filter(|path| admitted_file(path, false))
        .cloned()
        .collect();
    let mut selected_bytes = selected.iter().try_fold(0_u64, |bytes, path| {
        bytes
            .checked_add(inventory[path])
            .ok_or_else(|| io::Error::other("source size overflow"))
    })?;
    check_size(selected_bytes, selected.len())?;
    let mut pending: Vec<PathBuf> = selected
        .iter()
        .filter(|path| path.extension().is_some_and(|ext| ext == "rs"))
        .cloned()
        .collect();
    let mut inspected = BTreeSet::new();
    while let Some(source) = pending.pop() {
        if !inspected.insert(source.clone()) {
            continue;
        }
        if inventory[&source] > 64 * 1024 * 1024 {
            return Err(io::Error::other("Rust build input exceeds 64 MiB"));
        }
        let text = String::from_utf8(read_build_input(&origin.join(&source))?)
            .map_err(io::Error::other)?;
        let tokens = text.parse::<proc_macro2::TokenStream>().map_err(|e| {
            io::Error::other(format!(
                "cannot inspect Rust source {}: {e}",
                source.display()
            ))
        })?;
        let mut includes = vec![];
        embedded_paths(tokens, &mut includes);
        for (included, rust_source) in includes {
            if let Some(path) = relative_include(&source, &included) {
                // Missing/filtered paths can occur in unused backups or fixtures. Do not
                // fetch them from outside the admitted tree or override its exclusions.
                if inventory.contains_key(&path) && admitted_file(&path, true) {
                    if selected.insert(path.clone()) {
                        selected_bytes = selected_bytes
                            .checked_add(inventory[&path])
                            .ok_or_else(|| io::Error::other("source size overflow"))?;
                        check_size(selected_bytes, selected.len())?;
                    }
                    if rust_source || path.extension().is_some_and(|ext| ext == "rs") {
                        pending.push(path);
                    }
                }
            }
        }
    }
    let mut files = vec![];
    let mut bytes = 0_u64;
    for next in selected {
        let content = read_build_input(&origin.join(&next))?;
        bytes = bytes
            .checked_add(content.len() as u64)
            .ok_or_else(|| io::Error::other("source size overflow"))?;
        check_size(bytes, files.len() + 1)?;
        for root in [work, baseline] {
            let target = root.join(&next);
            fs::create_dir_all(target.parent().unwrap())?;
            fs::write(target, &content)?;
        }
        files.push(next.to_string_lossy().into());
    }
    Ok((files, bytes))
}
fn read_build_input(path: &Path) -> io::Result<Vec<u8>> {
    let mut content = Vec::new();
    fs::File::open(path)?
        .take(64 * 1024 * 1024 + 1)
        .read_to_end(&mut content)?;
    check_size(content.len() as u64, 1)?;
    Ok(content)
}
fn check_size(bytes: u64, count: usize) -> io::Result<()> {
    if bytes > 64 * 1024 * 1024 || count > 20000 {
        return Err(io::Error::other(
            "source copy exceeds 64 MiB or 20000 files; split the task into its source module",
        ));
    }
    Ok(())
}
pub fn prepare(origin: &Path, destination: &Path, todo: u64) -> io::Result<String> {
    let marker = destination.join("work/.cc-agent-origin.json");
    if destination.exists() {
        let previous: Value =
            serde_json::from_slice(&fs::read(&marker)?).map_err(io::Error::other)?;
        if previous["origin"] != origin.to_string_lossy().as_ref() || previous["todo_id"] != todo {
            return Err(io::Error::other(
                "existing source copy belongs to another task",
            ));
        }
        return Ok(destination.join("work").to_string_lossy().into());
    }
    let stage = destination.with_extension(format!(
        "{}-{}-staging",
        std::process::id(),
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos()
    ));
    let parent = destination
        .parent()
        .ok_or_else(|| io::Error::other("source copy has no container"))?;
    fs::create_dir_all(parent)?;
    fs::create_dir(&stage)?;
    let cleanup = Staging(stage.clone());
    let work = stage.join("work");
    let baseline = stage.join("baseline");
    fs::create_dir(&work)?;
    fs::create_dir(&baseline)?;
    let (files, bytes) = copy_tree(origin, &work, &baseline)?;
    fs::write(work.join(".cc-agent-origin.json"),json!({"origin":origin,"todo_id":todo,"baseline":"../baseline","files":files,"source_bytes":bytes}).to_string())?;
    fs::write(
        work.join("CC-SOURCE-WORKSPACE.md"),
        format!(
            "この作業の原本は {} です。コードはこのコピーで変更・検証してください。原本の実行用データ・認証設定は複製していません。原本への反映時は ../baseline の元ファイルと現在の原本を比較し、他担当の更新を上書きせず差分を統合してください。共有ToDoと常駐ツールは元の絶対パスから操作します。\n",
            origin.display()
        ),
    )?;
    assert_eq!(stage.parent(), destination.parent());
    fs::rename(stage, destination)?;
    drop(cleanup);
    Ok(destination.join("work").to_string_lossy().into())
}

#[cfg(test)]
mod tests {
    use super::*;
    struct Fixture(PathBuf);
    impl Fixture {
        fn new() -> Self {
            let root = std::env::temp_dir().join(format!(
                "cc-workspace-1768-{}-{}",
                std::process::id(),
                SystemTime::now()
                    .duration_since(UNIX_EPOCH)
                    .unwrap()
                    .as_nanos()
            ));
            fs::create_dir_all(root.join("origin")).unwrap();
            Self(root)
        }
        fn write(&self, path: &str, content: impl AsRef<[u8]>) {
            let path = self.0.join("origin").join(path);
            fs::create_dir_all(path.parent().unwrap()).unwrap();
            fs::write(path, content).unwrap();
        }
        fn prepare(&self) -> PathBuf {
            PathBuf::from(prepare(&self.0.join("origin"), &self.0.join("snapshot"), 1768).unwrap())
        }
    }
    impl Drop for Fixture {
        fn drop(&mut self) {
            fs::remove_dir_all(&self.0).unwrap();
        }
    }
    #[test]
    fn embedded_assets_are_copied_to_work_and_baseline_and_compile() {
        let fixture = Fixture::new();
        let source = r###"
            pub mod auth;
            pub mod data;
            pub const REQUEST: &str = include_str!("request_preamble.txt");
            pub const RESUME: &str = include_str!(r#"resume_preamble.txt"#,);
            pub const JSON: &str = include_str!("../tests/fixtures/job.json");
            pub const JSONL: &str = include_str!("../tests/fixtures/events.jsonl");
            pub const BYTES: &[u8] = include_bytes!("payload.bin");
            include!("extra.inc");
        "###;
        fixture.write("src/lib.rs", source);
        fixture.write("src/auth/mod.rs", "pub fn authenticate() {}");
        fixture.write("src/data/mod.rs", "pub fn load() {}");
        fixture.write("src/request_preamble.txt", "依頼の本文\n");
        fixture.write("src/resume_preamble.txt", "再開の本文\n");
        fixture.write("tests/fixtures/job.json", "{\"id\":1768}");
        fixture.write("tests/fixtures/events.jsonl", "{\"ok\":true}\n");
        fixture.write("src/payload.bin", [0, 255, 128]);
        fixture.write(
            "src/extra.inc",
            "pub const EXTRA: &str = include_str!(\"nested.txt\");",
        );
        fixture.write("src/nested.txt", "nested build input");
        let work = fixture.prepare();
        let marker: Value =
            serde_json::from_slice(&fs::read(work.join(".cc-agent-origin.json")).unwrap()).unwrap();
        let expected = [
            "src/auth/mod.rs",
            "src/data/mod.rs",
            "src/extra.inc",
            "src/lib.rs",
            "src/nested.txt",
            "src/payload.bin",
            "src/request_preamble.txt",
            "src/resume_preamble.txt",
            "tests/fixtures/events.jsonl",
            "tests/fixtures/job.json",
        ];
        let actual: BTreeSet<_> = marker["files"]
            .as_array()
            .unwrap()
            .iter()
            .map(|v| v.as_str().unwrap().replace('\\', "/"))
            .collect();
        assert_eq!(actual, expected.iter().map(|p| p.to_string()).collect());
        let mut bytes = 0_u64;
        for path in expected {
            let original = fs::read(fixture.0.join("origin").join(path)).unwrap();
            bytes += original.len() as u64;
            assert_eq!(fs::read(work.join(path)).unwrap(), original);
            assert_eq!(
                fs::read(fixture.0.join("snapshot/baseline").join(path)).unwrap(),
                original
            );
        }
        assert_eq!(marker["source_bytes"], bytes);
        let mut compiler = std::process::Command::new("rustc");
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            compiler.creation_flags(0x08000000);
        }
        let output = compiler
            .args(["--crate-type=lib", "--emit=metadata"])
            .arg(work.join("src/lib.rs"))
            .arg("-o")
            .arg(fixture.0.join("compiled.rmeta"))
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
    }
    #[test]
    fn comments_and_strings_do_not_admit_unreferenced_data() {
        let fixture = Fixture::new();
        fixture.write(
            "src/lib.rs",
            r##"
            // include_str!("unused.txt")
            /* include_bytes!("unused.json") */
            pub const TEXT: &str = "include_str!(\"unused.txt\")";
            pub const RAW: &str = r#"include_str!("unused.json")"#;
            pub const STRINGIFIED: &str = stringify!(include_str!("unused.json"));
        "##,
        );
        fixture.write("src/unused.txt", "not a build input");
        fixture.write("src/unused.json", "{}");
        let work = fixture.prepare();
        for path in ["src/unused.txt", "src/unused.json"] {
            assert!(!work.join(path).exists());
            assert!(!fixture.0.join("snapshot/baseline").join(path).exists());
        }
    }
    #[test]
    fn includes_never_override_auth_runtime_client_or_hidden_exclusions() {
        let fixture = Fixture::new();
        fixture.write("client-projects.txt", "example-client\n");
        let paths = [
            "src/credentials.toml",
            "src/CONFIG.TOML",
            "src/Auth.json",
            "src/secret.txt",
            "src/key.pem",
            "src/service-account.json",
            "src/client_secret.json",
            "src/id_rsa",
            "src/auth/service-account.json",
            "src/auth/account.toml",
            "src/runtime/session.yaml",
            "src/unclassified.db",
            "src/.env",
            "src/.settings/x.txt",
            "src/logs/x.txt",
            "src/data/x.txt",
            "src/runtime/x.txt",
            "example-client/input.txt",
            "example-client/private.rs",
        ];
        let source: String = paths
            .iter()
            .map(|path| {
                format!(
                    "const _: &str = include_str!({:?});\n",
                    format!("../{path}")
                )
            })
            .collect();
        fixture.write("src/lib.rs", source);
        for path in paths {
            fixture.write(path, "placeholder");
        }
        let work = fixture.prepare();
        for path in paths {
            assert!(!work.join(path).exists(), "copied excluded path {path}");
            assert!(!fixture.0.join("snapshot/baseline").join(path).exists());
        }
    }
    #[test]
    fn include_paths_are_relative_and_cannot_escape() {
        assert_eq!(
            relative_include(Path::new("crate/src/lib.rs"), "../tests/data.bin"),
            Some(PathBuf::from("crate/tests/data.bin"))
        );
        assert_eq!(
            relative_include(Path::new("src/lib.rs"), "..\\assets\\data.bin"),
            Some(PathBuf::from("assets/data.bin"))
        );
        for path in [
            "../../outside.txt",
            "C:/credentials.txt",
            "C:credentials.txt",
            "/absolute.txt",
            "\\\\server\\file",
            "payload.txt:secret",
        ] {
            assert!(
                relative_include(Path::new("src/lib.rs"), path).is_none(),
                "accepted {path}"
            );
        }
        let fixture = Fixture::new();
        fixture.write(
            "src/lib.rs",
            "const _: &str = include_str!(\"../../outside.txt\");",
        );
        fs::write(fixture.0.join("outside.txt"), "outside").unwrap();
        let work = fixture.prepare();
        assert!(!work.join("outside.txt").exists());
    }
    #[test]
    fn nested_macros_and_escaped_literals_are_discovered() {
        let tokens = r###"outer!(include_bytes!("asset\u{002e}bin"), { include_str![r##"本文.txt"##]; });"###.parse().unwrap();
        let mut paths = vec![];
        embedded_paths(tokens, &mut paths);
        assert_eq!(
            paths,
            [("asset.bin".into(), false), ("本文.txt".into(), false)]
        );
    }
    #[test]
    fn retry_preserves_edits_and_task_ownership() {
        let fixture = Fixture::new();
        fixture.write("src/lib.rs", "const _: &str = include_str!(\"input.txt\");");
        fixture.write("src/input.txt", "original");
        let work = fixture.prepare();
        fs::write(work.join("src/input.txt"), "edited").unwrap();
        fixture.write("src/input.txt", "changed in original");
        assert_eq!(fixture.prepare(), work);
        assert_eq!(
            fs::read_to_string(work.join("src/input.txt")).unwrap(),
            "edited"
        );
        assert_eq!(
            fs::read_to_string(fixture.0.join("snapshot/baseline/src/input.txt")).unwrap(),
            "original"
        );
        assert!(prepare(&fixture.0.join("origin"), &fixture.0.join("snapshot"), 1095).is_err());
    }
    #[test]
    fn embedded_assets_obey_size_limit_and_failed_staging_is_removed() {
        let fixture = Fixture::new();
        fixture.write(
            "src/lib.rs",
            "const _: &[u8] = include_bytes!(\"large.bin\");",
        );
        let file = fs::File::create(fixture.0.join("origin/src/large.bin")).unwrap();
        file.set_len(64 * 1024 * 1024 + 1).unwrap();
        drop(file);
        assert!(prepare(&fixture.0.join("origin"), &fixture.0.join("snapshot"), 1768).is_err());
        assert!(!fixture.0.join("snapshot").exists());
        assert_eq!(fs::read_dir(&fixture.0).unwrap().count(), 1);
    }
    #[test]
    fn build_input_formats_are_bounded_and_limits_keep_their_boundary() {
        assert!(build_asset_file(Path::new(
            "crate/src/request_preamble.txt"
        )));
        assert!(build_asset_file(Path::new(
            "crate/tests/fixtures/input.jsonl"
        )));
        assert!(!build_asset_file(Path::new("crate/account.json")));
        assert!(!build_asset_file(Path::new("crate/src/database.db")));
        assert!(!build_asset_file(Path::new("crate/src/auth/account.json")));
        assert!(check_size(64 * 1024 * 1024, 20000).is_ok());
        assert!(check_size(64 * 1024 * 1024 + 1, 1).is_err());
        assert!(check_size(0, 20001).is_err());
    }
}
