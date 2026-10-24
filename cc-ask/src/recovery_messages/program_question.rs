//! Go drawings have their own native evidence contract; imagegen receipts and
//! the general classifier are neither rewritten nor reused as authority.
use super::*;

pub(super) fn task_lock(ctx: &Context) -> Result<ByteLock> {
    #[cfg(not(test))]
    if image_path_key(&ctx.todo) != image_path_key(Path::new("D:/CC/Tools/todo/todo.json")) {
        return Err("program question task store differs from its verifier".into());
    }
    let root = ctx
        .todo
        .parent()
        .ok_or("program question task store unavailable")?;
    ByteLock::acquire(&root.join("todo.lock"), Duration::ZERO)
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct BoundImage {
    path: PathBuf,
    sha256: String,
    size: usize,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Proof {
    domain: String,
    validated: bool,
    send_allowed: bool,
    request_id: String,
    origin_thread_id: String,
    scope: crate::question_images::Scope,
    images: Vec<BoundImage>,
}

fn qualify(ctx: &mut Context, request: &Request, evidence: &Path) -> Result<Proof> {
    const FAILED: &str =
        "Go drawing evidence does not qualify this exact question; no image is sent";
    let Kind::Ask { origin_thread, .. } = &request.operation else {
        return Err(FAILED.into());
    };
    let scope = question_image_scope(ctx, request).ok_or(FAILED)?;
    let output = ctx
        .effects
        .run(
            &[
                "D:/CC/Tools/discord-ask/unattended-runner.exe".into(),
                "--check-program-question-evidence".into(),
                "--evidence".into(),
                evidence.to_str().ok_or(FAILED)?.into(),
            ],
            25,
        )
        .map_err(|_| FAILED)?;
    if output.code != 0 || output.stdout.len() > 64 * 1024 {
        return Err(FAILED.into());
    }
    let proof: Proof = serde_json::from_str(&output.stdout).map_err(|_| FAILED)?;
    if proof.domain != "owner-program-question-image-v1"
        || !proof.validated
        || !proof.send_allowed
        || proof.request_id != request.request_id
        || proof.origin_thread_id != *origin_thread
        || proof.scope != scope
        || proof.images.is_empty()
        || proof.images.len() > 10
    {
        return Err(FAILED.into());
    }
    Ok(proof)
}

/// Every new or resumed delivery rechecks the canonical saved question under
/// the todo writer lock. Its image receipts must be those currently qualified.
pub(super) fn revalidate(ctx: &mut Context, request: &Request, evidence: &Path) -> Result<()> {
    let proof = qualify(ctx, request, evidence)?;
    if request.images.len() != proof.images.len()
        || request
            .images
            .iter()
            .zip(&proof.images)
            .any(|(saved, current)| saved.sha256 != current.sha256 || saved.size != current.size)
    {
        return Err("current drawing evidence differs from the saved image receipts".into());
    }
    Ok(())
}

pub(super) fn images(
    ctx: &mut Context,
    request: &Request,
    args: &AskArgs,
    evidence: &Path,
) -> Result<Vec<Image>> {
    const FAILED: &str =
        "Go drawing evidence does not qualify this exact question; no image is sent";
    if args.attach.is_empty()
        || args.attach.len() > 10
        || args.expected_image_sha256.len() != args.attach.len()
    {
        return Err(FAILED.into());
    }
    let proof = qualify(ctx, request, evidence)?;
    if proof.images.len() != args.attach.len() {
        return Err(FAILED.into());
    }
    let secrets = SecretCheck::new();
    let mut result = Vec::new();
    let mut total = 0;
    for ((path, expected_sha), bound) in args
        .attach
        .iter()
        .zip(&args.expected_image_sha256)
        .zip(proof.images)
    {
        let resolved = fs::canonicalize(path).map_err(|_| FAILED)?;
        if !path.is_absolute()
            || image_path_key(path) != image_path_key(&resolved)
            || image_path_key(&bound.path) != image_path_key(path)
            || bound.size == 0
            || bound.size > IMAGE_BYTES
            || *expected_sha != bound.sha256
        {
            return Err(FAILED.into());
        }
        let file = fs::File::open(&resolved).map_err(|_| FAILED)?;
        if !file.metadata().map_err(|_| FAILED)?.is_file() {
            return Err(FAILED.into());
        }
        let bytes = read_bounded(file, IMAGE_BYTES)?;
        let sha256 = format!("{:x}", Sha256::digest(&bytes));
        if bytes.len() != bound.size
            || sha256 != bound.sha256
            || !bytes.starts_with(b"\x89PNG\r\n\x1a\n")
        {
            return Err(FAILED.into());
        }
        secrets.bytes(&bytes)?;
        total += bytes.len();
        if total > UPLOAD_BYTES {
            return Err(FAILED.into());
        }
        result.push(Image {
            receipt: ImageReceipt {
                filename: format!("image-{}.png", &sha256[..20]),
                mime: "image/png".into(),
                size: bytes.len(),
                sha256,
            },
            bytes,
        });
    }
    Ok(result)
}
