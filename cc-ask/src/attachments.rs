//! Validate and snapshot attachments before any Discord mutation. Transcoding uses hidden ffmpeg.
use crate::core::*;
use serde_json::{json, Value};
use std::{
    fs,
    io::{Read, Seek, SeekFrom, Write},
    path::{Path, PathBuf},
};

pub const BASE_LIMIT: u64 = 10 * 1024 * 1024;
const MESSAGE_BYTES: u64 = 180 * 1024 * 1024;

pub fn tier_limit(tier: u64) -> u64 {
    match tier {
        2 => 50 * 1024 * 1024,
        3 => 100 * 1024 * 1024,
        _ => BASE_LIMIT,
    }
}

pub struct File {
    pub path: PathBuf,
    pub name: String,
    pub mime: &'static str,
    pub size: u64,
}
pub struct Prepared {
    _temp: tempfile::TempDir,
    pub files: Vec<File>,
    pub limit: u64,
}

pub fn fingerprints(prepared: Option<&Prepared>) -> Result<Vec<String>> {
    use sha2::{Digest, Sha256};
    let mut hashes = Vec::new();
    for file in prepared.into_iter().flat_map(|p| &p.files) {
        let mut input = fs::File::open(&file.path).map_err(|e| e.to_string())?;
        let mut digest = Sha256::new();
        let mut buffer = [0u8; 65536];
        loop {
            let n = input.read(&mut buffer).map_err(|e| e.to_string())?;
            if n == 0 {
                break;
            }
            digest.update(&buffer[..n]);
        }
        hashes.push(format!("{:x}", digest.finalize()));
    }
    hashes.sort();
    Ok(hashes)
}

struct Policy {
    clients: Vec<String>,
    secrets: Vec<String>,
    pattern: regex::Regex,
}
// SQLite's public printf diagnostics contain placeholders, not credential values.
// Match only the exact parser diagnostic; never ignore another assignment or known secret.
fn sqlite_diagnostic_ranges(data: &[u8]) -> Vec<std::ops::Range<usize>> {
    let mut ranges = Vec::new();
    for literal in ["unrecognized token: \"%s\"", "unrecognized token: \"%T\""] {
        let encodings = [
            literal.as_bytes().to_vec(),
            literal.encode_utf16().flat_map(u16::to_le_bytes).collect(),
            literal.encode_utf16().flat_map(u16::to_be_bytes).collect(),
        ];
        for bytes in encodings {
            for (start, chunk) in data.windows(bytes.len()).enumerate() {
                if chunk == bytes {
                    ranges.push(start..start + bytes.len());
                }
            }
        }
    }
    ranges.sort_by_key(|range| range.start);
    ranges
}
fn decoded_views(data: &[u8]) -> Vec<String> {
    let mut views = vec![String::from_utf8_lossy(data).replace('\0', "")];
    for offset in 0..data.len().min(2) {
        for little in [true, false] {
            let words: Vec<u16> = data[offset..]
                .as_chunks::<2>()
                .0
                .iter()
                .map(|b| {
                    if little {
                        u16::from_le_bytes([b[0], b[1]])
                    } else {
                        u16::from_be_bytes([b[0], b[1]])
                    }
                })
                .collect();
            // Normalize dispersed NUL code units after decoding as well as in
            // the byte view. Real UTF-16 passwords must survive C0 boundaries
            // in unrelated PE metadata without losing this encoding coverage.
            views.push(String::from_utf16_lossy(&words).replace('\0', ""));
        }
    }
    views
}
fn production_pattern() -> regex::Regex {
    regex::Regex::new(concat!(
            r#"(?i)(?:authorization["']?\s*[:=]\s*["']?(?:Bearer|Bot)\s+\S+|"#,
            r#"(?:api[_-]?key|access[_-]?token|refresh[_-]?token|password|passwd|pwd|secret|bot[_-]?token|token|secret[_-]?access[_-]?key|private[_-]?key|connection[_-]?string|client-key-data|_auth)["']?\s*[:=]\s*["']?[^\s"',}]+|"#,
            // Password-bearing URI userinfo is secret regardless of the configuration key.
            // Unescaped controls terminate userinfo: PE length/address bytes after a
            // public format fragment are not a password. Keep NUL-normalized views
            // so actual NUL-dispersed credentials remain detectable on every host.
            r#"[a-z][a-z0-9+.-]*://[^\s\p{Cc}/"'<>:@]+:[^\s\p{Cc}/"'<>@]+@|"#,
            r#"[a-z][a-z0-9+.-]*://:[^\s\p{Cc}/"'<>@]+@|"#,
            r"-----BEGIN (?:[A-Z ]+ )?PRIVATE KEY(?: BLOCK)?-----|",
            r"https://(?:discord\.com|discordapp\.com)/api/webhooks/|",
            r"\b(?:sk-[A-Za-z0-9_-]{16,}|gh[pousr]_[A-Za-z0-9]{20,}|AKIA[A-Z0-9]{16}|mfa\.[A-Za-z0-9_-]{40,})\b)"
        )).unwrap()
}
impl Policy {
    fn load(ctx: &Context) -> Result<Self> {
        let mut clients: Vec<String> = read_text(&ctx.client_projects)?
            .trim_start_matches('\u{feff}')
            .lines()
            .map(str::trim)
            .filter(|s| !s.is_empty() && !s.starts_with('#'))
            .map(|s| s.replace('\\', "/").to_lowercase())
            .collect();
        // Protect the real location too when the deny list names a junction/symlink.
        for entry in clients.clone() {
            if let Ok(resolved) = fs::canonicalize(&entry) {
                clients.push(resolved.to_string_lossy().replace('\\', "/").to_lowercase());
            }
        }
        let names = regex::Regex::new("(?i)TOKEN|PASSWORD|SECRET|API_KEY|WEBHOOK").unwrap();
        let mut secrets: Vec<String> = std::env::vars()
            .filter(|(k, v)| names.is_match(k) && v.len() >= 8)
            .map(|(_, v)| v)
            .collect();
        if let Ok(value) = token() {
            secrets.push(value);
        }
        let pattern = production_pattern();
        Ok(Self {
            clients,
            secrets,
            pattern,
        })
    }
    fn path(&self, path: &Path) -> Result<()> {
        let value = path.to_string_lossy().replace('\\', "/").to_lowercase();
        if self.clients.iter().any(|entry| value.contains(entry)) {
            return Err("顧客案件のファイルは添付できません".into());
        }
        for part in value.split('/') {
            if part == ".env"
                || part.starts_with(".env.")
                || [
                    ".ssh",
                    ".aws",
                    ".azure",
                    ".gnupg",
                    ".pgpass",
                    ".netrc",
                    ".kube",
                    ".npmrc",
                    "credentials",
                    "secrets",
                    "auth.json",
                    "credentials.json",
                    "credentials.toml",
                    "id_rsa",
                    "id_ed25519",
                ]
                .contains(&part)
                || part.ends_with(".pem")
                || part.ends_with(".key")
                || part.ends_with(".p12")
                || part.ends_with(".pfx")
                || part.contains("secret")
                || part.contains("password")
                || part.contains("token")
            {
                return Err("秘密情報を含む場所のファイルは添付できません".into());
            }
        }
        Ok(())
    }
    fn bytes(&self, data: &[u8]) -> Result<()> {
        // Known values always inspect unmodified input; masking never affects snapshots.
        if !self.secrets.is_empty()
            && decoded_views(data).iter().any(|text| {
                self.secrets
                    .iter()
                    .any(|s| !s.is_empty() && text.contains(s))
            })
        {
            return Err("秘密情報を検出したため添付を中止しました".into());
        }
        let mut regex_view = data.to_vec();
        for range in sqlite_diagnostic_ranges(data) {
            // Keep "unrecognized " so an enclosing assignment such as
            // password=unrecognized token: "%s" is still rejected.
            let width = range.len() / 24;
            regex_view[range.start + 13 * width..range.end].fill(0);
        }
        if decoded_views(&regex_view)
            .iter()
            .any(|text| self.pattern.is_match(text))
        {
            return Err("秘密情報を検出したため添付を中止しました".into());
        }
        Ok(())
    }
    fn scan(&self, mut input: impl Read, mut output: Option<&mut fs::File>) -> Result<()> {
        let mut buf = [0u8; 65536];
        let mut overlap = Vec::new();
        let keep = self
            .secrets
            .iter()
            .map(String::len)
            .max()
            .unwrap_or(0)
            .saturating_mul(2)
            .saturating_add(64)
            .max(4096);
        loop {
            let count = input
                .read(&mut buf)
                .map_err(|_| "添付ファイルを読み取れません")?;
            if count == 0 {
                self.bytes(&overlap)?;
                break;
            }
            overlap.extend_from_slice(&buf[..count]);
            // Defer a possible incomplete ASCII/UTF-16 diagnostic until another
            // read or EOF. Retain original overlap, including known-value bytes.
            let mut end = overlap.len().saturating_sub(48);
            for range in sqlite_diagnostic_ranges(&overlap) {
                if range.start < end && end < range.end {
                    end = end.min(range.start);
                }
            }
            self.bytes(&overlap[..end])?;
            if let Some(out) = output.as_deref_mut() {
                out.write_all(&buf[..count])
                    .map_err(|_| "添付ファイルを保存できません")?;
            }
            if overlap.len() > keep {
                let mut cut = overlap.len() - keep;
                for range in sqlite_diagnostic_ranges(&overlap) {
                    if range.start < cut && cut < range.end {
                        cut = cut.min(range.start);
                    }
                }
                overlap.drain(..cut);
            }
        }
        Ok(())
    }
    fn archive(&self, path: &Path, depth: usize) -> Result<()> {
        let mut input = fs::File::open(path).map_err(|_| "添付ファイルを読み取れません")?;
        let mut header = [0u8; 4];
        let count = input
            .read(&mut header)
            .map_err(|_| "添付ファイルを読み取れません")?;
        let size = input
            .metadata()
            .map_err(|_| "添付ファイルを読み取れません")?
            .len();
        input
            .seek(SeekFrom::Start(size.saturating_sub(65557)))
            .map_err(|_| "添付ファイルを読み取れません")?;
        let mut tail = Vec::new();
        input
            .read_to_end(&mut tail)
            .map_err(|_| "添付ファイルを読み取れません")?;
        // The central-directory footer also identifies self-extracting/prepended ZIPs.
        if (count == 4 && header.starts_with(b"PK"))
            || zip_footer(&tail)
            || extension(path) == "zip"
        {
            self.zip(
                fs::File::open(path).map_err(|_| "添付ファイルを読み取れません")?,
                depth,
            )?;
        } else if header.starts_with(b"\x1f\x8b")
            || header.starts_with(b"7z")
            || header.starts_with(b"Rar!")
        {
            return Err(
                "この圧縮形式は秘密情報を検査できません。展開したファイルを添付してください".into(),
            );
        }
        Ok(())
    }
    fn zip(&self, reader: impl Read + std::io::Seek, depth: usize) -> Result<()> {
        if depth > 4 {
            return Err("圧縮ファイルの入れ子が深いため検査できません".into());
        }
        let mut archive =
            zip::ZipArchive::new(reader).map_err(|_| "圧縮ファイルを検査できません")?;
        let mut total = 0u64;
        for i in 0..archive.len() {
            let mut entry = archive
                .by_index(i)
                .map_err(|_| "暗号化または未対応の圧縮ファイルは添付できません")?;
            self.path(Path::new(entry.name()))?;
            total = total.saturating_add(entry.size());
            if total > 512 * 1024 * 1024 {
                return Err("圧縮ファイルの展開量が検査上限を超えます".into());
            }
            let mut data = Vec::new();
            entry
                .by_ref()
                .take(512 * 1024 * 1024 + 1)
                .read_to_end(&mut data)
                .map_err(|_| "圧縮ファイルを検査できません")?;
            if data.len() > 512 * 1024 * 1024 {
                return Err("圧縮ファイルの展開量が検査上限を超えます".into());
            }
            self.scan(&data[..], None)?;
            if data.starts_with(b"PK")
                || zip_footer(&data)
                || entry.name().to_lowercase().ends_with(".zip")
            {
                self.zip(std::io::Cursor::new(data), depth + 1)?;
            } else if data.starts_with(b"\x1f\x8b")
                || data.starts_with(b"7z")
                || data.starts_with(b"Rar!")
            {
                return Err("圧縮ファイル内に検査できない圧縮形式があります".into());
            }
        }
        Ok(())
    }
}

fn zip_footer(data: &[u8]) -> bool {
    data[data.len().saturating_sub(65557)..]
        .windows(4)
        .any(|b| b == b"PK\x05\x06")
}

fn filename(path: &Path) -> String {
    path.file_name()
        .unwrap_or_default()
        .to_string_lossy()
        .chars()
        .take(160)
        .map(|c| {
            if c.is_control() || ['"', '\\', '/', ';'].contains(&c) {
                '_'
            } else {
                c
            }
        })
        .collect()
}
fn extension(path: &Path) -> String {
    path.extension()
        .unwrap_or_default()
        .to_string_lossy()
        .to_lowercase()
}
fn mime(path: &Path) -> &'static str {
    match extension(path).as_str() {
        "png" => "image/png",
        "jpg" | "jpeg" => "image/jpeg",
        "webp" => "image/webp",
        "gif" => "image/gif",
        "mp4" => "video/mp4",
        "webm" => "video/webm",
        "pdf" => "application/pdf",
        "txt" | "md" => "text/plain",
        _ => "application/octet-stream",
    }
}
fn file(path: PathBuf, name: String, limit: u64) -> Result<File> {
    let size = fs::metadata(&path)
        .map_err(|_| "変換結果を読み取れません")?
        .len();
    if size > limit {
        return Err("添付ファイルが容量上限を超えています".into());
    }
    if size == 0 {
        return Err("空の添付ファイルは送信できません".into());
    }
    Ok(File {
        mime: mime(Path::new(&name)),
        path,
        name,
        size,
    })
}
fn media(ctx: &mut Context, argv: Vec<String>) -> Result<Output> {
    let out = ctx.run(&argv, 1800)?;
    if out.code != 0 {
        return Err("ffmpegによる添付ファイルの変換に失敗しました".into());
    }
    Ok(out)
}
fn ffmpeg(input: &Path) -> Vec<String> {
    vec!["ffmpeg".into(), "-nostdin".into(), "-hide_banner".into(), "-loglevel".into(), "error".into(), "-y".into(),
        "-protocol_whitelist".into(), "file,pipe".into(), "-format_whitelist".into(),
        "image2,png_pipe,jpeg_pipe,webp_pipe,bmp_pipe,tiff_pipe,gif,mov,matroska,webm,avi,mpegts,mpeg,flv,ogg".into(),
        "-i".into(), input.to_string_lossy().into_owned(), "-map_metadata".into(), "-1".into(), "-map_chapters".into(), "-1".into()]
}
fn image(ctx: &mut Context, input: &Path, dir: &Path, stem: &str, limit: u64) -> Result<Vec<File>> {
    let out = dir.join(format!("{}.webp", uuid::Uuid::new_v4()));
    for width in [4096, 2048, 1024, 512, 256, 128] {
        for quality in [80, 60, 40] {
            let mut argv = ffmpeg(input);
            argv.extend([
                "-frames:v".into(),
                "1".into(),
                "-vf".into(),
                format!("scale=min({width}\\,iw):-1"),
                "-c:v".into(),
                "libwebp".into(),
                "-quality".into(),
                quality.to_string(),
                "-threads".into(),
                "1".into(),
                out.to_string_lossy().into_owned(),
            ]);
            media(ctx, argv)?;
            if fs::metadata(&out)
                .map_err(|_| "画像の変換結果がありません")?
                .len()
                <= limit
            {
                return Ok(vec![file(out, format!("{stem}.webp"), limit)?]);
            }
        }
    }
    Err("画像を容量上限まで縮小できませんでした".into())
}
fn encode_video(
    ctx: &mut Context,
    input: &Path,
    output: &Path,
    start: f64,
    duration: f64,
    bitrate: u64,
    width: u32,
) -> Result<()> {
    let mut argv = ffmpeg(input);
    argv.extend([
        "-ss".into(),
        format!("{start:.6}"),
        "-t".into(),
        format!("{duration:.6}"),
        "-map".into(),
        "0:v:0".into(),
        "-map".into(),
        "0:a:0?".into(),
        "-vf".into(),
        format!("scale=trunc(min({width}\\,iw)/2)*2:-2"),
        "-c:v".into(),
        "libx264".into(),
        "-preset".into(),
        "fast".into(),
        "-b:v".into(),
        bitrate.to_string(),
        "-maxrate".into(),
        bitrate.to_string(),
        "-bufsize".into(),
        (bitrate * 2).to_string(),
        "-pix_fmt".into(),
        "yuv420p".into(),
        "-c:a".into(),
        "aac".into(),
        "-b:a".into(),
        "48000".into(),
        "-ac".into(),
        "2".into(),
        "-movflags".into(),
        "+faststart".into(),
        "-threads".into(),
        "2".into(),
        output.to_string_lossy().into_owned(),
    ]);
    media(ctx, argv)?;
    Ok(())
}
struct Video<'a> {
    input: &'a Path,
    dir: &'a Path,
    stem: &'a str,
    limit: u64,
}
impl Video<'_> {
    fn part(
        &self,
        ctx: &mut Context,
        start: f64,
        duration: f64,
        depth: u32,
        files: &mut Vec<File>,
    ) -> Result<()> {
        if depth > 12 || duration < 0.05 {
            return Err("動画を容量上限まで分割できませんでした".into());
        }
        let out = self.dir.join(format!("{}.mp4", uuid::Uuid::new_v4()));
        let budget = (self.limit as f64 * 0.80 * 8. / duration) as u64;
        encode_video(
            ctx,
            self.input,
            &out,
            start,
            duration,
            budget.saturating_sub(48000).clamp(120000, 2_000_000),
            854,
        )?;
        if fs::metadata(&out)
            .map_err(|_| "動画の変換結果がありません")?
            .len()
            <= self.limit
        {
            files.push(file(
                out,
                format!("{}-part{:03}.mp4", self.stem, files.len() + 1),
                self.limit,
            )?);
        } else {
            fs::remove_file(&out).map_err(|_| "変換途中の動画を削除できません")?;
            self.part(ctx, start, duration / 2., depth + 1, files)?;
            self.part(ctx, start + duration / 2., duration / 2., depth + 1, files)?;
        }
        Ok(())
    }
}
fn video(ctx: &mut Context, input: &Path, dir: &Path, stem: &str, limit: u64) -> Result<Vec<File>> {
    let probe = media(
        ctx,
        vec![
            "ffprobe".into(),
            "-v".into(),
            "error".into(),
            "-protocol_whitelist".into(),
            "file,pipe".into(),
            "-format_whitelist".into(),
            "mov,matroska,webm,avi,mpegts,mpeg,flv,ogg".into(),
            "-show_entries".into(),
            "format=duration".into(),
            "-of".into(),
            "json".into(),
            input.to_string_lossy().into_owned(),
        ],
    )?;
    let value: Value =
        serde_json::from_str(&probe.stdout).map_err(|_| "動画の長さを取得できません")?;
    let duration: f64 = s(&value["format"]["duration"])
        .parse()
        .map_err(|_| "動画の長さを取得できません")?;
    if !duration.is_finite() || duration <= 0. {
        return Err("動画の長さが不正です".into());
    }
    let out = dir.join(format!("{}.mp4", uuid::Uuid::new_v4()));
    let budget = (limit as f64 * 0.80 * 8. / duration) as u64;
    for (width, divisor) in [(1280, 1), (854, 2)] {
        encode_video(
            ctx,
            input,
            &out,
            0.,
            duration,
            (budget.saturating_sub(48000) / divisor).clamp(120000, 4_000_000),
            width,
        )?;
        if fs::metadata(&out)
            .map_err(|_| "動画の変換結果がありません")?
            .len()
            <= limit
        {
            return Ok(vec![file(out, format!("{stem}.mp4"), limit)?]);
        }
    }
    fs::remove_file(&out).map_err(|_| "変換途中の動画を削除できません")?;
    let plan = Video {
        input,
        dir,
        stem,
        limit,
    };
    let mut files = Vec::new();
    let seconds = (limit as f64 * 0.80 * 8. / 348000.).clamp(0.1, 120.);
    let mut start = 0.;
    while start < duration {
        let length = seconds.min(duration - start);
        plan.part(ctx, start, length, 0, &mut files)?;
        start += length;
    }
    Ok(files)
}

pub fn prepare(ctx: &mut Context, paths: &[PathBuf]) -> Result<Option<Prepared>> {
    if paths.is_empty() {
        return Ok(None);
    }
    let policy = Policy::load(ctx)?;
    let temp = tempfile::tempdir().map_err(|_| "添付用の一時フォルダを作成できません")?;
    let mut snapshots = Vec::new();
    for path in paths {
        policy.path(path)?;
        let resolved = fs::canonicalize(path).map_err(|_| "添付ファイルが見つかりません")?;
        policy.path(&resolved)?;
        let input = fs::File::open(&resolved).map_err(|_| "添付ファイルを読み取れません")?;
        if !input
            .metadata()
            .map_err(|_| "添付ファイルを読み取れません")?
            .is_file()
        {
            return Err("添付にはファイルを指定してください".into());
        }
        let snapshot = temp
            .path()
            .join(format!("{}.{}", uuid::Uuid::new_v4(), extension(path)));
        let mut output =
            fs::File::create(&snapshot).map_err(|_| "添付用のコピーを作成できません")?;
        policy.scan(input, Some(&mut output))?;
        drop(output);
        policy.archive(&snapshot, 0)?;
        snapshots.push((snapshot, filename(path)));
    }
    let guild = s(&ctx.cfg["guild_id"]).to_owned();
    // If tier lookup fails, use the conservative limit requested by the owner.
    let tier = ctx
        .call("GET", &format!("/guilds/{guild}"), None)
        .ok()
        .map(|g| n(&g["premium_tier"]))
        .unwrap_or(0);
    let limit = tier_limit(tier);
    let mut files = Vec::new();
    for (snapshot, name) in snapshots {
        let size = fs::metadata(&snapshot)
            .map_err(|_| "添付用のコピーを読み取れません")?
            .len();
        if size <= limit {
            files.push(file(snapshot, name, limit)?);
            continue;
        }
        let stem = Path::new(&name)
            .file_stem()
            .unwrap_or_default()
            .to_string_lossy();
        let converted = match extension(&snapshot).as_str() {
            "png" | "jpg" | "jpeg" | "webp" | "gif" | "bmp" | "tif" | "tiff" => {
                image(ctx, &snapshot, temp.path(), &stem, limit)?
            }
            "mp4" | "mov" | "mkv" | "webm" | "avi" | "m4v" | "mpeg" | "mpg" | "ts" | "flv" => {
                video(ctx, &snapshot, temp.path(), &stem, limit)?
            }
            _ => {
                return Err(
                    "画像・動画以外の添付が容量上限を超えています。ファイルを分けてください".into(),
                )
            }
        };
        for f in &converted {
            policy.scan(
                fs::File::open(&f.path).map_err(|_| "変換結果を読み取れません")?,
                None,
            )?;
        }
        files.extend(converted);
    }
    Ok(Some(Prepared {
        _temp: temp,
        files,
        limit,
    }))
}

/// At most ten attachments; also stay below Discord's total request body ceiling.
pub fn batches(files: &[File]) -> Vec<&[File]> {
    let mut parts = Vec::new();
    let mut start = 0;
    let mut size = 0;
    for (i, f) in files.iter().enumerate() {
        if i > start && (i - start == 10 || size + f.size > MESSAGE_BYTES) {
            parts.push(&files[start..i]);
            start = i;
            size = 0;
        }
        size += f.size;
    }
    if start < files.len() {
        parts.push(&files[start..]);
    }
    parts
}

/// Forum creation nests attachment metadata in message; regular posts put it at the root.
pub fn send(
    ctx: &mut Context,
    route: &str,
    payload: &Value,
    files: &[File],
    limit: u64,
) -> Result<Value> {
    if files.is_empty() {
        return ctx.call("POST", route, Some(payload));
    }
    if files.len() > 10 || files.iter().any(|f| f.size > limit) {
        return Err("添付数または容量が上限を超えています".into());
    }
    let boundary = format!("cc-ask-{}", uuid::Uuid::new_v4().simple());
    let mut payload = payload.clone();
    let metadata: Vec<Value> = files
        .iter()
        .enumerate()
        .map(|(i, f)| json!({"id":i,"filename":f.name}))
        .collect();
    if payload.get("message").is_some() {
        payload["message"]["attachments"] = json!(metadata);
    } else {
        payload["attachments"] = json!(metadata);
    }
    let mut body = format!("--{boundary}\r\nContent-Disposition: form-data; name=\"payload_json\"\r\nContent-Type: application/json\r\n\r\n{payload}\r\n").into_bytes();
    for (i, f) in files.iter().enumerate() {
        body.extend_from_slice(format!("--{boundary}\r\nContent-Disposition: form-data; name=\"files[{i}]\"; filename=\"{}\"\r\nContent-Type: {}\r\n\r\n", f.name, f.mime).as_bytes());
        let data = fs::read(&f.path).map_err(|_| "添付用のコピーを読み取れません")?;
        if data.len() as u64 != f.size || data.len() as u64 > limit {
            return Err("添付用のコピーの容量が変わりました".into());
        }
        body.extend_from_slice(&data);
        body.extend_from_slice(b"\r\n");
    }
    body.extend_from_slice(format!("--{boundary}--\r\n").as_bytes());
    crate::state_work::check(ctx)?;
    ctx.effects.multipart(
        &format!("https://discord.com/api/v10{route}"),
        &format!("multipart/form-data; boundary={boundary}"),
        &body,
    )
}

pub fn post(
    ctx: &mut Context,
    thread: &str,
    body: &str,
    prepared: Option<&Prepared>,
    notify: bool,
) -> Result<Vec<String>> {
    post_tracked(ctx, thread, body, prepared, notify, None)
}

/// Register immediately after each successful batch. Later upload failures leave the
/// first delivered question tracked, and answers between batches retain their cursor.
pub fn post_tracked(
    ctx: &mut Context,
    thread: &str,
    body: &str,
    prepared: Option<&Prepared>,
    notify: bool,
    question: Option<&Value>,
) -> Result<Vec<String>> {
    let _guard = if question.is_some() {
        Some(question_lock(ctx, thread)?)
    } else {
        None
    };
    post_tracked_locked(ctx, thread, body, prepared, notify, question)
}

pub(crate) fn question_lock(ctx: &Context, thread: &str) -> Result<ByteLock> {
    question_lock_for(ctx, thread, std::time::Duration::from_secs(60))
}
pub(crate) fn question_lock_for(
    ctx: &Context,
    thread: &str,
    timeout: std::time::Duration,
) -> Result<ByteLock> {
    if thread.is_empty() || !thread.bytes().all(|c| c.is_ascii_digit()) {
        return Err("invalid question thread".into());
    }
    ByteLock::acquire(
        &ctx.here.join(format!("question-post-{thread}.lock")),
        timeout,
    )
}

/// Caller holds only the per-thread delivery lock. Network waits never hold state.lock.
pub(crate) fn post_tracked_locked(
    ctx: &mut Context,
    thread: &str,
    body: &str,
    prepared: Option<&Prepared>,
    notify: bool,
    question: Option<&Value>,
) -> Result<Vec<String>> {
    post_tracked_locked_with_context(ctx, thread, body, prepared, notify, question, None)
}

pub(crate) fn post_tracked_locked_with_context(
    ctx: &mut Context,
    thread: &str,
    body: &str,
    prepared: Option<&Prepared>,
    notify: bool,
    question: Option<&Value>,
    context: Option<&Value>,
) -> Result<Vec<String>> {
    deliver_question(
        ctx,
        thread,
        body,
        prepared,
        (notify, false),
        question,
        context,
    )
}

/// Recover only already delivered parts before a newer owner answer suppresses
/// a retry. This never sends another message or finishes remaining text chunks.
pub(crate) fn recover_posted(
    ctx: &mut Context,
    thread: &str,
    body: &str,
    prepared: Option<&Prepared>,
    notify: bool,
    question: &Value,
    context: Option<&Value>,
) -> Result<Vec<String>> {
    deliver_question(
        ctx,
        thread,
        body,
        prepared,
        (notify, true),
        Some(question),
        context,
    )
}

fn deliver_question(
    ctx: &mut Context,
    thread: &str,
    body: &str,
    prepared: Option<&Prepared>,
    mode: (bool, bool),
    question: Option<&Value>,
    context: Option<&Value>,
) -> Result<Vec<String>> {
    let (notify, recover_only) = mode;
    let fingerprints = if question.is_some() {
        fingerprints(prepared)?
    } else {
        vec![]
    };
    let files = prepared.map(|p| batches(&p.files)).unwrap_or_default();
    let parts = chunks(body, MAX_CONTENT);
    let count = parts.len().max(files.len());
    if count == 0 {
        return Err("本文または添付ファイルを指定してください".into());
    }
    // A retry after response loss or a failed registration recovers the same
    // delivered messages, even after Discord's nonce retention window expires.
    let ledger_path = if question.is_some() {
        use sha2::{Digest, Sha256};
        let digest = format!(
            "{:x}",
            Sha256::digest(json!([thread, body, fingerprints, notify]).to_string())
        );
        Some(ctx.here.join(format!("question-post-{digest}.json")))
    } else {
        None
    };
    let existing_ledger = ledger_path.as_ref().is_some_and(|p| p.is_file());
    let mut ledger = match ledger_path.as_ref().filter(|p| p.is_file()) {
        Some(p) => read_json(p)?,
        None if recover_only => return Ok(vec![]),
        None => json!({"parts":{}}),
    };
    // Keep the original sender's context in the pre-send ledger. Response-loss
    // recovery and later chunks register the same owner, even from a new process.
    if question.is_some() && ledger["question_context"].is_null() {
        let mut original = json!({"session_id":null,"posting_job":null});
        if !existing_ledger {
            if let Some(context) = context {
                original = context.clone();
            }
            original["session_id"] = json!(std::env::var("CODEX_THREAD_ID")
                .ok()
                .filter(|id| !id.is_empty()));
            original["posting_job"] = json!(crate::questions::posting_job(ctx)?);
        } else {
            // Legacy ledgers can only recover an already recorded origin. The
            // current process is a recovery reader, not evidence of authorship.
            let state = ctx.load_fresh()?;
            if let Some(q) = state["questions"]
                .as_array()
                .into_iter()
                .flatten()
                .find(|q| {
                    s(&q["thread_id"]) == thread
                        && q["question_message"] == ledger["parts"]["0"]["id"]
                        && !s(&q["question_message"]).is_empty()
                })
            {
                for key in [
                    "agent",
                    "cwd",
                    "project",
                    "title",
                    "session_id",
                    "posting_job",
                    "source",
                    "scheduler_item",
                    "scheduler_job",
                    "owner_only",
                ] {
                    if let Some(value) = q.get(key) {
                        original[key] = value.clone();
                    }
                }
            }
        }
        ledger["question_context"] = original;
    }
    // A main's first POST may succeed while its response is lost. Publish the
    // inbox-only ownership before that effect, so intake cannot adopt its reply
    // while the per-message question record is still being recovered.
    if question.is_some() && crate::routing::question_inbox_only(ctx, &ledger["question_context"]) {
        let _state = ByteLock::acquire(
            &ctx.here.join("state.lock"),
            std::time::Duration::from_secs(60),
        )?;
        let mut state = ctx.load_fresh()?;
        state["inbox_only_post_contexts"][thread] = ledger["question_context"].clone();
        atomic_json(&ctx.here.join("state.json"), &state, 2)?;
    }
    if ledger_path.is_some() && s(&ledger["sender"]).is_empty() {
        let me = ctx.call("GET", "/users/@me", None)?;
        if s(&me["id"]).is_empty() || !truth(&me["bot"]) {
            return Err("question sender unavailable".into());
        }
        ledger["sender"] = me["id"].clone();
    }
    let mut ids = Vec::new();
    for i in 0..count {
        let mentions = if question.is_some() && notify && i == 0 {
            json!({"parse":[],"users":[ctx.owner()],"replied_user":false})
        } else {
            json!({"parse":[]})
        };
        let mut payload = json!({"content":parts.get(i).map(String::as_str).unwrap_or(""),"flags":if notify {0} else {SILENT},"allowed_mentions":mentions});
        let part = i.to_string();
        let response = if let Some(path) = &ledger_path {
            if !ledger["parts"][&part].is_object() {
                if recover_only {
                    break;
                }
                let cursor = thread_history(ctx, thread)?
                    .iter()
                    .map(|m| n(&m["id"]))
                    .max()
                    .unwrap_or(0)
                    .to_string();
                ledger["parts"][&part] =
                    json!({"nonce":uuid::Uuid::new_v4().simple().to_string()[..24],"after":cursor});
                atomic_json(path, &ledger, 2)?;
            }
            let entry = &ledger["parts"][&part];
            let recovered = if truth(&entry["id"]) {
                Some(json!({"id":entry["id"]}))
            } else {
                thread_history(ctx, thread)?.into_iter().find(|m| {
                    m["author"]["id"] == ledger["sender"]
                        && n(&m["id"]) > n(&entry["after"])
                        && (m["nonce"] == entry["nonce"]
                            || (m["content"] == payload["content"]
                                && same_files(m, files.get(i).copied().unwrap_or(&[]))))
                })
            };
            payload["nonce"] = entry["nonce"].clone();
            payload["enforce_nonce"] = json!(true);
            let response = match recovered {
                Some(m) => m,
                None if recover_only => break,
                None => send(
                    ctx,
                    &format!("/channels/{thread}/messages"),
                    &payload,
                    files.get(i).copied().unwrap_or(&[]),
                    prepared.map(|p| p.limit).unwrap_or(BASE_LIMIT),
                )?,
            };
            if s(&response["id"]).is_empty() {
                return Err("post returned no message ID".into());
            }
            ledger["parts"][&part]["id"] = response["id"].clone();
            atomic_json(path, &ledger, 2)?;
            response
        } else {
            send(
                ctx,
                &format!("/channels/{thread}/messages"),
                &payload,
                files.get(i).copied().unwrap_or(&[]),
                prepared.map(|p| p.limit).unwrap_or(BASE_LIMIT),
            )?
        };
        let id = s(&response["id"]);
        if id.is_empty() {
            return Err("post returned no message ID".into());
        }
        ids.push(id.to_owned());
        if let Some(channel) = question {
            let _state = ByteLock::acquire(
                &ctx.here.join("state.lock"),
                std::time::Duration::from_secs(60),
            )?;
            let mut state = ctx.load_fresh()?;
            crate::questions::register_posted_by(ctx, &mut state, channel, &ids[0], &ids);
            if let Some(q) = state["questions"]
                .as_array_mut()
                .unwrap()
                .iter_mut()
                .rev()
                .find(|q| s(&q["thread_id"]) == thread && s(&q["question_message"]) == ids[0])
            {
                q["attachment_hashes"] = json!(fingerprints);
                if let Some(context) = ledger["question_context"].as_object() {
                    for (key, value) in context {
                        q[key] = value.clone();
                    }
                }
            }
            if state["inbox_only_post_contexts"][thread] == ledger["question_context"] {
                if let Some(contexts) = state["inbox_only_post_contexts"].as_object_mut() {
                    contexts.remove(thread);
                }
            }
            atomic_json(&ctx.here.join("state.json"), &state, 2)?;
        }
    }
    Ok(ids)
}

fn same_files(message: &Value, files: &[File]) -> bool {
    let attached = message["attachments"]
        .as_array()
        .map(Vec::as_slice)
        .unwrap_or(&[]);
    attached.len() == files.len()
        && attached
            .iter()
            .zip(files)
            .all(|(a, f)| s(&a["filename"]) == f.name && n(&a["size"]) == f.size)
}

#[cfg(test)]
mod tests {
    use super::*;
    fn credential_pattern_fixture() -> Policy {
        Policy {
            clients: vec![],
            secrets: vec![],
            pattern: production_pattern(),
        }
    }
    #[test]
    fn uri_userinfo_stops_at_binary_control_metadata() {
        let p = credential_pattern_fixture();
        // A Rust format fragment followed by PE metadata is not one URI. NUL
        // normalization must not turn the remaining control byte into a password.
        for host in ["localhost", "127.0.0.1", "public.example"] {
            for control in [1u8, 17, 25, 127] {
                let text = format!("http://{host}:\0public{}\0@", char::from(control));
                let mut encodings = vec![text.as_bytes().to_vec()];
                for little in [true, false] {
                    for offset in [0, 1] {
                        let mut bytes = vec![0; offset];
                        bytes.extend(text.encode_utf16().flat_map(|w| {
                            if little {
                                w.to_le_bytes()
                            } else {
                                w.to_be_bytes()
                            }
                        }));
                        encodings.push(bytes);
                    }
                }
                for bytes in encodings {
                    assert!(p.bytes(&bytes).is_ok());
                }
            }
        }
    }
    #[test]
    fn uri_and_assignment_values_survive_encodings_boundaries_and_nested_zip() {
        let p = credential_pattern_fixture();
        for text in [
            "http://localhost:actual-value@localhost/",
            "http://127.0.0.1:actual-value@public.example/",
            "https://:actual-value@public.example/",
            "postgres://learner:actual-value@localhost/db",
            "https://learner:%19actual-value@localhost/",
            "https://learner:日本語の値@localhost/",
            // A genuine U+FFFD in valid UTF-8 is still a password character;
            // it must not be globally exempted as if every occurrence were lossy.
            "https://learner:\u{fffd}@localhost/",
            "api_key=actual-value",
            "token=actual-value",
        ] {
            let ascii = text.as_bytes().to_vec();
            let nul: Vec<u8> = ascii.iter().flat_map(|b| [*b, 0, 0]).collect();
            let mut encodings = vec![ascii, nul];
            for little in [true, false] {
                for offset in [0, 1] {
                    let mut bytes = vec![0; offset];
                    bytes.extend(text.encode_utf16().flat_map(|w| {
                        if little {
                            w.to_le_bytes()
                        } else {
                            w.to_be_bytes()
                        }
                    }));
                    encodings.push(bytes);
                }
            }
            for bytes in encodings {
                assert_eq!(
                    p.bytes(&bytes).unwrap_err(),
                    "秘密情報を検出したため添付を中止しました"
                );
                for start in [61430, 65530] {
                    let mut data = vec![b' '; start];
                    data.extend_from_slice(&bytes);
                    data.resize(70000, b' ');
                    assert_eq!(
                        p.scan(&data[..], None).unwrap_err(),
                        "秘密情報を検出したため添付を中止しました"
                    );
                }
                let mut blob = bytes;
                for name in ["app.bin", "inner.zip"] {
                    let mut writer = zip::ZipWriter::new(std::io::Cursor::new(Vec::new()));
                    writer
                        .start_file(name, zip::write::SimpleFileOptions::default())
                        .unwrap();
                    writer.write_all(&blob).unwrap();
                    blob = writer.finish().unwrap().into_inner();
                }
                assert_eq!(
                    p.zip(std::io::Cursor::new(blob), 0).unwrap_err(),
                    "秘密情報を検出したため添付を中止しました"
                );
            }
        }
        let mut known = credential_pattern_fixture();
        known.secrets.push("public@".into());
        assert!(known.bytes(b"http://localhost:\0\x19\0public@").is_err());
        assert!(known.bytes(b"http://localhost:\0\x19\0p\0ublic@").is_err());
        assert!(p
            .bytes(b"http://localhost:\0actual-value@localhost/")
            .is_err());
    }
    #[test]
    fn utf16_nul_dispersed_uri_passwords_still_reject_actual_values() {
        let p = credential_pattern_fixture();
        // U+5019's low byte is a C0 control in the raw UTF-8-lossy view.
        // Detection must use actual UTF-16 text when an additional NUL code
        // unit separates password characters.
        let text = "http://learner:候\0補@localhost/";
        for little in [true, false] {
            for offset in [0, 1] {
                let mut bytes = vec![0; offset];
                bytes.extend(text.encode_utf16().flat_map(|w| {
                    if little {
                        w.to_le_bytes()
                    } else {
                        w.to_be_bytes()
                    }
                }));
                assert_eq!(
                    p.bytes(&bytes).unwrap_err(),
                    "秘密情報を検出したため添付を中止しました"
                );
                let mut data = vec![b' '; 65530];
                data.extend_from_slice(&bytes);
                data.resize(70000, b' ');
                assert_eq!(
                    p.scan(&data[..], None).unwrap_err(),
                    "秘密情報を検出したため添付を中止しました"
                );
                let mut blob = bytes;
                for name in ["app.bin", "inner.zip"] {
                    let mut writer = zip::ZipWriter::new(std::io::Cursor::new(Vec::new()));
                    writer
                        .start_file(name, zip::write::SimpleFileOptions::default())
                        .unwrap();
                    writer.write_all(&blob).unwrap();
                    blob = writer.finish().unwrap().into_inner();
                }
                assert_eq!(
                    p.zip(std::io::Cursor::new(blob), 0).unwrap_err(),
                    "秘密情報を検出したため添付を中止しました"
                );
            }
        }
    }
    #[test]
    fn sqlite_printf_diagnostics_do_not_suppress_actual_or_known_values() {
        let mut p = credential_pattern_fixture();
        for marker in ["s", "T"] {
            let good = format!("unrecognized token: \"%{marker}\"");
            assert!(p.bytes(good.as_bytes()).is_ok());
            for bad in [
                format!("unrecognized token: \"%{marker}actual-value\""),
                format!("{good} token=actual-value"),
                "unrecognized token: \"actual-value\"".to_string(),
                format!("token: \"%{marker}\""),
                format!("unrecognized token: \"%{marker}"),
            ] {
                assert!(p.bytes(bad.as_bytes()).is_err());
            }
            for little in [true, false] {
                for offset in [0, 1] {
                    let mut b = vec![b'X'; offset];
                    for w in good.encode_utf16() {
                        b.extend_from_slice(&if little {
                            w.to_le_bytes()
                        } else {
                            w.to_be_bytes()
                        });
                    }
                    assert!(p.bytes(&b).is_ok());
                    let mut leaked = b;
                    let tail = " password=actual-value";
                    for w in tail.encode_utf16() {
                        leaked.extend_from_slice(&if little {
                            w.to_le_bytes()
                        } else {
                            w.to_be_bytes()
                        });
                    }
                    assert!(p.bytes(&leaked).is_err());
                }
            }
        }
        p.secrets.push("%s".into());
        assert!(p.bytes(b"unrecognized token: \"%s\"").is_err());
    }
    #[test]
    fn sqlite_diagnostic_at_buffer_boundary_and_nested_archive_still_checks_following_values() {
        let p = credential_pattern_fixture();
        for start in [
            65513, 65514, 61430, 65465, 65466, 65441, 65442, 65443, 65444,
        ] {
            for little in [None, Some(true), Some(false)] {
                let mut data = vec![b' '; start];
                let good = "unrecognized token: \"%s\"";
                if let Some(le) = little {
                    for word in good.encode_utf16() {
                        data.extend_from_slice(&if le {
                            word.to_le_bytes()
                        } else {
                            word.to_be_bytes()
                        });
                    }
                } else {
                    data.extend_from_slice(good.as_bytes());
                }
                data.resize(70000, b' ');
                assert!(
                    p.scan(&data[..], None).is_ok(),
                    "start={start} encoding={little:?}"
                );
            }
        }
        let mut known = credential_pattern_fixture();
        known.secrets.push("synthetic-known-marker".into());
        for start in [65530, 61430] {
            let mut data = vec![b' '; start];
            data.extend_from_slice(b"synthetic-known-marker");
            data.resize(70000, b' ');
            assert!(known.scan(&data[..], None).is_err());
        }
        assert!(p.bytes(b"password=unrecognized token: \"%s\"").is_err());
        let mut cut = vec![b' '; 61430];
        cut.extend_from_slice(b"unrecognized token: \"%s\"");
        cut.resize(70000, b' ');
        assert!(p.scan(&cut[..], None).is_ok());
        let mut bytes = vec![b' '; 65525];
        bytes.extend_from_slice(b"unrecognized token: \"%s\"");
        assert!(p.scan(&bytes[..], None).is_ok());
        bytes.extend_from_slice(b" api_key=actual-value");
        assert!(p.scan(&bytes[..], None).is_err());
        let mut writer = zip::ZipWriter::new(std::io::Cursor::new(Vec::new()));
        writer
            .start_file("app.txt", zip::write::SimpleFileOptions::default())
            .unwrap();
        writer.write_all(&bytes).unwrap();
        let blob = writer.finish().unwrap().into_inner();
        assert!(p.zip(std::io::Cursor::new(blob), 0).is_err());
    }
    #[test]
    #[ignore = "requires explicit distribution files; never loads credentials"]
    fn owner_distribution_archives_pass_without_configuration_or_network_access() {
        let paths = std::env::var_os("CC_ATTACHMENT_AUDIT_FILES")
            .expect("CC_ATTACHMENT_AUDIT_FILES must name the actual distributions");
        // No Policy::load: no credentials/deny-list reads or network. Known production
        // values cannot be checked here; synthetic known values are tested separately.
        let p = Policy {
            clients: vec![],
            secrets: vec![],
            pattern: production_pattern(),
        };
        for path in std::env::split_paths(&paths) {
            p.path(&path).unwrap();
            p.scan(fs::File::open(&path).unwrap(), None)
                .unwrap_or_else(|e| panic!("{}: {e}", path.display()));
            p.archive(&path, 0)
                .unwrap_or_else(|e| panic!("{}: {e}", path.display()));
            assert!(fs::metadata(path).unwrap().len() <= BASE_LIMIT);
        }
    }
    #[test]
    fn known_non_ascii_secrets_are_detected_in_both_utf16_byte_orders_and_alignments() {
        let policy = Policy {
            clients: vec![],
            secrets: vec!["日本語の秘密".into()],
            pattern: regex::Regex::new("never-match-this-pattern").unwrap(),
        };
        for little in [true, false] {
            for prefix in [vec![], vec![0x41]] {
                let mut data = prefix;
                for word in "日本語の秘密".encode_utf16() {
                    data.extend_from_slice(&if little {
                        word.to_le_bytes()
                    } else {
                        word.to_be_bytes()
                    });
                }
                assert!(policy.bytes(&data).is_err());
            }
        }
    }
    #[test]
    fn real_ffmpeg_image_and_video_outputs_fit_and_decode() {
        let root = tempfile::tempdir().unwrap();
        let empty = PathBuf::new();
        let mut ctx = Context {
            here: empty.clone(),
            client_projects: empty.clone(),
            civ6_home: empty.clone(),
            jobs: empty.clone(),
            discord_tasks: empty.clone(),
            todo: empty.clone(),
            cfg: json!({}),
            effects: Box::new(RealEffects),
            codex_command: empty.clone(),
            scheduler_command: empty.clone(),
            shepherd_command: empty.clone(),
            todo_command: empty,
        };
        let png = root.path().join("noise.png");
        let out = run_hidden(
            &[
                "ffmpeg".into(),
                "-nostdin".into(),
                "-v".into(),
                "error".into(),
                "-f".into(),
                "lavfi".into(),
                "-i".into(),
                "nullsrc=s=640x480,geq=random(1)*255:128:128".into(),
                "-frames:v".into(),
                "1".into(),
                png.to_string_lossy().into_owned(),
            ],
            60,
        )
        .unwrap();
        assert_eq!(out.code, 0, "{}", out.stderr);
        let converted = image(&mut ctx, &png, root.path(), "image", 10000).unwrap();
        assert_eq!(converted.len(), 1);
        assert!(converted[0].size <= 10000);
        let mp4 = root.path().join("video.mp4");
        let out = run_hidden(
            &[
                "ffmpeg".into(),
                "-nostdin".into(),
                "-v".into(),
                "error".into(),
                "-f".into(),
                "lavfi".into(),
                "-i".into(),
                "testsrc2=size=640x360:rate=15".into(),
                "-t".into(),
                "3".into(),
                "-c:v".into(),
                "libx264".into(),
                "-preset".into(),
                "ultrafast".into(),
                mp4.to_string_lossy().into_owned(),
            ],
            60,
        )
        .unwrap();
        assert_eq!(out.code, 0, "{}", out.stderr);
        let files = video(&mut ctx, &mp4, root.path(), "video", 30000).unwrap();
        assert!(!files.is_empty());
        for file in converted.iter().chain(&files) {
            assert!(
                file.size
                    <= if file.mime == "image/webp" {
                        10000
                    } else {
                        30000
                    }
            );
            let out = run_hidden(
                &[
                    "ffmpeg".into(),
                    "-nostdin".into(),
                    "-v".into(),
                    "error".into(),
                    "-i".into(),
                    file.path.to_string_lossy().into_owned(),
                    "-f".into(),
                    "null".into(),
                    "-".into(),
                ],
                60,
            )
            .unwrap();
            assert_eq!(out.code, 0, "{}", out.stderr);
        }
    }
}
