//! One explanation of one original question. Original log intents stay immutable.
use super::*;

const DOMAIN: &str = "discord-question-clarification-v1";
const FILE: &str = "recovery-clarifications.json";
const INVALID: &str =
    "clarification must match the exact original question, owner request and fixed unsent log";

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
pub(super) struct Binding {
    domain: String,
    purpose: String,
    request_id: String,
    question_request_id: String,
    todo_id: u64,
    thread_id: String,
    question_message_id: String,
    question_fingerprint: String,
    question_body_sha256: String,
    bot_id: String,
    owner_id: String,
    reply_message_id: String,
    reply_body_sha256: String,
    reply_timestamp: String,
    // Value makes this field required even when its expected value is null.
    reply_edited_timestamp: Value,
    body_sha256: String,
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Bindings {
    domain: String,
    records: BTreeMap<String, Binding>,
    // New authorizations pin the full intent before reserve(). Old records are
    // still checked against their already saved immutable ledger intent.
    #[serde(default)]
    requests: BTreeMap<String, Request>,
}

fn hash(text: &str) -> String {
    format!("{:x}", Sha256::digest(text.as_bytes()))
}

impl Binding {
    pub(super) fn read(path: &Path) -> Result<Self> {
        Ok(Self::read_with_hash(path)?.0)
    }

    pub(super) fn read_with_hash(path: &Path) -> Result<(Self, String)> {
        let info = fs::symlink_metadata(path).map_err(|_| INVALID)?;
        if !info.is_file() || info.file_type().is_symlink() {
            return Err(INVALID.into());
        }
        let bytes = read_bounded(fs::File::open(path).map_err(|_| INVALID)?, 16 * 1024)?;
        let value: Self = serde_json::from_slice(&bytes).map_err(|_| INVALID)?;
        value.validate()?;
        Ok((value, format!("{:x}", Sha256::digest(&bytes))))
    }

    fn validate(&self) -> Result<()> {
        if self.domain != DOMAIN || self.purpose != "explain_original_question" || self.todo_id == 0
        {
            return Err(INVALID.into());
        }
        request_key(&self.request_id)?;
        request_key(&self.question_request_id)?;
        for id in [
            &self.thread_id,
            &self.question_message_id,
            &self.bot_id,
            &self.owner_id,
            &self.reply_message_id,
        ] {
            snowflake(id)?;
        }
        for sha in [
            &self.question_fingerprint,
            &self.question_body_sha256,
            &self.reply_body_sha256,
            &self.body_sha256,
        ] {
            if sha.len() != 64
                || !sha
                    .bytes()
                    .all(|b| b.is_ascii_hexdigit() && !b.is_ascii_uppercase())
            {
                return Err(INVALID.into());
            }
        }
        chrono::DateTime::parse_from_rfc3339(&self.reply_timestamp).map_err(|_| INVALID)?;
        if !self.reply_edited_timestamp.is_null() {
            chrono::DateTime::parse_from_rfc3339(
                self.reply_edited_timestamp.as_str().ok_or(INVALID)?,
            )
            .map_err(|_| INVALID)?;
        }
        Ok(())
    }

    fn check_identity(&self, request: &Request) -> Result<()> {
        let Kind::Log {
            thread: Some(thread),
        } = &request.operation
        else {
            return Err(INVALID.into());
        };
        if request.request_id != self.request_id
            || thread != &self.thread_id
            || request.notify
            || !request.images.is_empty()
            || request.destination.owner != self.owner_id
            || hash(&request.body) != self.body_sha256
        {
            return Err(INVALID.into());
        }
        Ok(())
    }

    pub(super) fn check_input(&self, request: &Request) -> Result<()> {
        self.check_identity(request)?;
        no_mentions(&request.body)?;
        if regex::Regex::new(r"(?im)[?？]|^\s*(?:[abａｂ0-9０-９][.．、:：)）]|[（(][abａｂ0-9０-９][)）]|[ab]\s*/\s*[ab])|選んで|選択して|回答して|答えて|教えて|承認して|決めて|ください|お願い|どちら(?:に|が|を)|いかが|(?:です|ます)か(?:[。！!\s]|$)")
            .unwrap().is_match(&request.body)
        { return Err("clarification cannot contain another question or repeated choices".into()); }
        Ok(())
    }
}

impl Bindings {
    fn load(home: &Path) -> Result<Self> {
        let path = home.join(FILE);
        match fs::symlink_metadata(&path) {
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                return Ok(Self {
                    domain: DOMAIN.into(),
                    records: BTreeMap::new(),
                    requests: BTreeMap::new(),
                })
            }
            Ok(m) if m.is_file() && !m.file_type().is_symlink() => {}
            _ => return Err(INVALID.into()),
        }
        let value: Self = serde_json::from_slice(&read_bounded(
            fs::File::open(path).map_err(|_| INVALID)?,
            LEDGER_BYTES,
        )?)
        .map_err(|_| INVALID)?;
        if value.domain != DOMAIN {
            return Err(INVALID.into());
        }
        for (id, binding) in &value.records {
            binding.validate()?;
            if id != &binding.request_id {
                return Err(INVALID.into());
            }
        }
        for (id, request) in &value.requests {
            value
                .records
                .get(id)
                .ok_or(INVALID)?
                .check_identity(request)?;
            if request.request_id != *id || request.fingerprint()? != request.fingerprint {
                return Err(INVALID.into());
            }
        }
        Ok(value)
    }

    fn save(&self, home: &Path) -> Result<()> {
        let bytes = serde_json::to_vec_pretty(self).map_err(|_| INVALID)?;
        if bytes.len() > LEDGER_BYTES {
            return Err(INVALID.into());
        }
        let mut temp = tempfile::Builder::new()
            .prefix("recovery-clarifications.")
            .tempfile_in(home)
            .map_err(|_| INVALID)?;
        temp.write_all(&bytes).map_err(|_| INVALID)?;
        temp.as_file().sync_all().map_err(|_| INVALID)?;
        core::replace_file(temp.path(), &home.join(FILE)).map_err(|_| INVALID)?;
        #[cfg(not(windows))]
        fs::File::open(home)
            .and_then(|dir| dir.sync_all())
            .map_err(|_| INVALID)?;
        Ok(())
    }
}

/// Under the message byte lock, before any Pending/POST; every retry rechecks GET.
pub(super) fn authorize(
    api: &mut RecoveryApi<'_>,
    ledger: &Ledger,
    home: &Path,
    request: &Request,
    binding: &Binding,
    persist: bool,
    new_phase: bool,
) -> Result<()> {
    binding.check_input(request)?;
    if ledger
        .request(&binding.request_id)
        .is_some_and(|saved| saved != request)
        || api.bot != binding.bot_id
        || ledger.events.iter().any(|event| {
            matches!(event, Event::Intent {request: saved}
            if saved.request_id != request.request_id && saved.fingerprint == request.fingerprint)
        })
    {
        return Err(INVALID.into());
    }
    let question = ledger
        .request(&binding.question_request_id)
        .ok_or(INVALID)?;
    if question.request_id != binding.question_request_id
        || question.destination != request.destination
        || question.fingerprint != binding.question_fingerprint
        || hash(&question.body) != binding.question_body_sha256
        || !matches!(question.operation, Kind::Ask { todo_id, .. } if todo_id == binding.todo_id)
        || ledger.bot(&question.request_id) != Some(binding.bot_id.as_str())
    {
        return Err(INVALID.into());
    }
    let receipt = ledger
        .receipt(&question.request_id, "question:0")
        .ok_or(INVALID)?;
    if receipt.thread_id != binding.thread_id
        || receipt.message_id != binding.question_message_id
        || receipt.author_id != binding.bot_id
    {
        return Err(INVALID.into());
    }
    let mut bindings = Bindings::load(home)?;
    if bindings
        .requests
        .get(&request.request_id)
        .is_some_and(|saved| saved != request)
    {
        return Err(INVALID.into());
    }
    if let Some(saved) = bindings.records.get(&request.request_id) {
        if saved != binding {
            return Err(INVALID.into());
        }
    } else if ledger.events.iter().any(|event| {
        matches!(event,
        Event::Pending { request_id, .. } | Event::Posted { request_id, .. }
        | Event::Verified { request_id, .. } | Event::DeliveryVerified { request_id }
            if request_id == &request.request_id)
    }) || bindings
        .records
        .values()
        .any(|saved| saved.reply_message_id == binding.reply_message_id)
    {
        return Err(INVALID.into());
    }
    let expected = ledger
        .effect(&question.request_id, "question:0")
        .ok_or(INVALID)?;
    api.verify(
        ledger,
        question,
        expected,
        &receipt.thread_id,
        &receipt.message_id,
    )?;
    // Existing phases must be recoverable even if a later part has never been
    // reserved. Only a new phase is subject to the current owner reply version.
    if bindings.records.get(&request.request_id) == Some(binding)
        && !new_phase
        && parts(&request.body).iter().enumerate().any(|(index, _)| {
            ledger
                .effect(&request.request_id, &format!("log:{index}"))
                .is_some()
        })
    {
        return Ok(());
    }
    let reply = api.get(&format!(
        "/channels/{}/messages/{}",
        binding.thread_id, binding.reply_message_id
    ))?;
    let body = reply["content"].as_str().ok_or(INVALID)?;
    SecretCheck::new().text(body)?;
    if !exact_owner_reply(
        &reply,
        &binding.thread_id,
        &binding.reply_message_id,
        &binding.owner_id,
    ) || hash(body) != binding.reply_body_sha256
        || reply["timestamp"] != binding.reply_timestamp
        || reply["edited_timestamp"] != binding.reply_edited_timestamp
        || core::n(&reply["id"]) <= core::n(&json!(binding.question_message_id))
        || chrono::DateTime::parse_from_rfc3339(&binding.reply_timestamp).map_err(|_| INVALID)?
            < chrono::DateTime::parse_from_rfc3339(&receipt.timestamp).map_err(|_| INVALID)?
        || !requests_explanation(body)
    {
        return Err(INVALID.into());
    }
    // An explicit close is durable and uses this same message lock. Ordinary
    // Discord auto-archiving alone does not indicate an owner stop or close.
    if CloseLedger::load(home)?
        .records
        .get(&binding.thread_id)
        .is_some_and(|closed| {
            closed.binding.request_id == binding.question_request_id
                && (matches!(closed.phase.as_str(), "verified" | "patch_pending")
                    || closed.outcome.is_some()
                    || closed.patch_attempted)
        })
    {
        return Err(
            "original question has an explicit close record; no new explanation phase reserved"
                .into(),
        );
    }
    if persist && !bindings.records.contains_key(&request.request_id) {
        bindings
            .records
            .insert(request.request_id.clone(), binding.clone());
        bindings
            .requests
            .insert(request.request_id.clone(), request.clone());
        bindings.save(home)?;
    }
    Ok(())
}

fn requests_explanation(body: &str) -> bool {
    if regex::Regex::new(r"(?:説明|補足|解説)(?:は|が|も)?(?:不要|いらない|要らない)")
        .unwrap()
        .is_match(body)
    {
        return false;
    }
    // A commitment is different from asking about the consequences of an
    // option. Inspect complete choice phrases before the explanatory clause.
    let choice = regex::Regex::new(r"(?i)(?:^|[^a-z0-9ａ-ｚ０-９])[a-dａ-ｄ]\s*(?:で|を(?:選んだ|選ぶ|選びます|選びたい|選べ|選択|採用|希望)|に(?:する|し(?:ます|た|て|よう)|決め)|が(?:いい|良い)|採用)").unwrap();
    for phrase in choice.find_iter(body) {
        let remaining = &body[phrase.end()..];
        if !["ら", "ば", "なら", "あれば", "あるなら", "したら", "すれば"]
            .iter()
            .any(|suffix| remaining.starts_with(suffix))
        {
            return false;
        }
    }
    let explicit = regex::Regex::new(
        r"(?i)説明(?:して|を(?:お願い|ください|聞きたい))|解説して|explain|clarif",
    )
    .unwrap()
    .is_match(body);
    let topic = regex::Regex::new(
        r"説明|理由|内訳|必要|ひつよう|費用|料金|経費|けいひ|用途|根拠|背景|意味|何のため",
    )
    .unwrap();
    let teach = regex::Regex::new(r"教えて(?:ほしい(?:です)?|ください)?\s*$").unwrap();
    let more = regex::Regex::new(
        r"^(?:もっと|もう少し)?\s*詳しく(?:教えて(?:ほしい(?:です)?|ください)?)?$",
    )
    .unwrap();
    let detail = body
        .split(['。', '！', '!', '？', '?', '\n', ';', '；'])
        .any(|sentence| {
            let sentence = sentence.trim();
            (topic.is_match(sentence) && teach.is_match(sentence)) || more.is_match(sentence)
        });
    let why = regex::Regex::new(r"なぜ|どうして|なんで")
        .unwrap()
        .is_match(
            &body
                .replace("なぜなら", "")
                .replace("なぜか", "")
                .replace("どうしても", "")
                .replace("なんでも", ""),
        );
    let question = body.contains(['?', '？'])
        || regex::Regex::new(r"(?:なの|ですか|ますか|どういう意味|何のため)\s*[。！!]*\s*$")
            .unwrap()
            .is_match(body);
    explicit || detail || why || question && regex::Regex::new(r"(?i)どういう|何の|何で|要る|いる|必要|ひつよう|理由|内訳|用途|根拠|経費|けいひ|費用|料金|why|what").unwrap().is_match(body)
}
