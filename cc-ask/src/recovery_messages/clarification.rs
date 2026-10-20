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
}

fn hash(text: &str) -> String {
    format!("{:x}", Sha256::digest(text.as_bytes()))
}

impl Binding {
    pub(super) fn read(path: &Path) -> Result<Self> {
        let info = fs::symlink_metadata(path).map_err(|_| INVALID)?;
        if !info.is_file() || info.file_type().is_symlink() {
            return Err(INVALID.into());
        }
        let value: Self = serde_json::from_slice(&read_bounded(
            fs::File::open(path).map_err(|_| INVALID)?,
            16 * 1024,
        )?)
        .map_err(|_| INVALID)?;
        value.validate()?;
        Ok(value)
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

    pub(super) fn check_input(&self, request: &Request) -> Result<()> {
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
) -> Result<()> {
    binding.check_input(request)?;
    if ledger.request(&binding.request_id) != Some(request) || api.bot != binding.bot_id {
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
    if let Some(saved) = bindings.records.get(&request.request_id) {
        if saved != binding {
            return Err(INVALID.into());
        }
    } else if ledger.events.iter().any(|event| {
        matches!(event,
        Event::Pending { request_id, .. } | Event::Posted { request_id, .. }
        | Event::Verified { request_id, .. } | Event::DeliveryVerified { request_id }
            if request_id == &request.request_id)
    }) || bindings.records.values().any(|saved| {
        saved.question_request_id == binding.question_request_id
            && saved.reply_message_id == binding.reply_message_id
    }) {
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
    // Each existing Pending phase can only execute GET recovery in ensure().
    // Keep an already authorized delivery recoverable after an owner edit,
    // without permitting any newly reserved POST phase from stale evidence.
    if bindings.records.get(&request.request_id) == Some(binding)
        && parts(&request.body).iter().enumerate().all(|(index, _)| {
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
        || !regex::Regex::new(
            r"(?i)どういう意味|何のため|説明|詳しく|clarif|what.*(?:mean|for)|explain",
        )
        .unwrap()
        .is_match(body)
    {
        return Err(INVALID.into());
    }
    if persist && !bindings.records.contains_key(&request.request_id) {
        bindings
            .records
            .insert(request.request_id.clone(), binding.clone());
        bindings.save(home)?;
    }
    Ok(())
}
