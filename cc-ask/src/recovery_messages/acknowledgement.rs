//! One silent receipt for one exact owner reply, without making its question a log.
use super::*;

const DOMAIN: &str = "discord-owner-reply-acknowledgement-v1";
const FILE: &str = "recovery-owner-acknowledgements.json";
const INVALID: &str =
    "acknowledgement must match the exact verified question and owner reply version";

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
    reply_edited_timestamp: Value,
    body_sha256: String,
}

#[derive(Default, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Journal {
    domain: String,
    records: BTreeMap<String, Binding>,
    requests: BTreeMap<String, Request>,
    existing: BTreeMap<String, String>,
}

fn hash(value: &str) -> String {
    format!("{:x}", Sha256::digest(value.as_bytes()))
}

impl Binding {
    pub(super) fn read(path: &Path) -> Result<Self> {
        let metadata = fs::symlink_metadata(path).map_err(|_| INVALID)?;
        if !metadata.is_file() || metadata.file_type().is_symlink() {
            return Err(INVALID.into());
        }
        let bytes = read_bounded(fs::File::open(path).map_err(|_| INVALID)?, 16 * 1024)?;
        let binding: Self = serde_json::from_slice(&bytes).map_err(|_| INVALID)?;
        binding.validate()?;
        Ok(binding)
    }

    fn validate(&self) -> Result<()> {
        if self.domain != DOMAIN || self.purpose != "acknowledge_owner_reply" || self.todo_id == 0 {
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
        self.validate()?;
        if request.request_id != self.request_id
            || request.notify
            || !request.images.is_empty()
            || !matches!(
                request.body.as_str(),
                "受け取りました。" | "受け取りました。\n"
            )
            || hash(&request.body) != self.body_sha256
            || request.destination.owner != self.owner_id
            || !matches!(&request.operation, Kind::Log {thread:Some(thread)} if thread == &self.thread_id)
        {
            return Err(INVALID.into());
        }
        no_mentions(&request.body)
    }

    pub(super) fn message_reference(&self) -> Value {
        json!({"message_id":self.reply_message_id,"channel_id":self.thread_id,"fail_if_not_exists":true})
    }

    pub(super) fn nonce(&self, request: &Request, phase: &str) -> Result<String> {
        digest(
            &json!({"fingerprint":request.fingerprint,"phase":phase,"reply":self.reply_message_id,
            "reply_sha":self.reply_body_sha256,"edited":self.reply_edited_timestamp}),
        )
    }

    pub(super) fn bind_effect(&self, effect: &mut Effect) {
        // Keep the original intent fingerprint, but distinguish identical short
        // acknowledgements to different messages or edited versions in GET recovery.
        effect
            .content
            .push_str(&format!("\n-# {}", self.version_marker()));
    }

    fn version_marker(&self) -> String {
        format!(
            "owner-receipt:{}:{}",
            self.reply_message_id,
            &hash(&format!(
                "{}|{}",
                self.reply_body_sha256, self.reply_edited_timestamp
            ))[..24]
        )
    }

    fn same_version(&self, other: &Self) -> bool {
        self.owner_id == other.owner_id
            && self.thread_id == other.thread_id
            && self.reply_message_id == other.reply_message_id
            && self.reply_body_sha256 == other.reply_body_sha256
            && self.reply_timestamp == other.reply_timestamp
            && self.reply_edited_timestamp == other.reply_edited_timestamp
    }

    pub(super) fn check_posted(&self, request: &Request, message: &Value) -> Result<()> {
        self.check_input(request)?;
        if message["message_reference"]["message_id"] != self.reply_message_id
            || message["message_reference"]["channel_id"] != self.thread_id
        {
            return Err("acknowledgement readback is not linked to the exact owner reply".into());
        }
        Ok(())
    }
}

impl Journal {
    fn load(home: &Path) -> Result<Self> {
        let path = home.join(FILE);
        match fs::symlink_metadata(&path) {
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                return Ok(Self {
                    domain: DOMAIN.into(),
                    ..Self::default()
                })
            }
            Ok(m) if m.is_file() && !m.file_type().is_symlink() => {}
            _ => return Err(INVALID.into()),
        }
        let journal: Self = serde_json::from_slice(&read_bounded(
            fs::File::open(path).map_err(|_| INVALID)?,
            LEDGER_BYTES,
        )?)
        .map_err(|_| INVALID)?;
        if journal.domain != DOMAIN || journal.records.len() != journal.requests.len() {
            return Err(INVALID.into());
        }
        for (id, binding) in &journal.records {
            let request = journal.requests.get(id).ok_or(INVALID)?;
            if binding.request_id != *id || request.fingerprint()? != request.fingerprint {
                return Err(INVALID.into());
            }
            binding.check_input(request)?;
        }
        for (id, message) in &journal.existing {
            if !journal.records.contains_key(id) {
                return Err(INVALID.into());
            }
            snowflake(message)?;
        }
        Ok(journal)
    }

    fn save(&self, home: &Path) -> Result<()> {
        let bytes = serde_json::to_vec_pretty(self).map_err(|_| INVALID)?;
        if bytes.len() > LEDGER_BYTES {
            return Err(INVALID.into());
        }
        let mut file = tempfile::Builder::new()
            .prefix("recovery-owner-acknowledgements.")
            .tempfile_in(home)
            .map_err(|_| INVALID)?;
        file.write_all(&bytes).map_err(|_| INVALID)?;
        file.as_file().sync_all().map_err(|_| INVALID)?;
        core::replace_file(file.path(), &home.join(FILE)).map_err(|_| INVALID)?;
        #[cfg(not(windows))]
        fs::File::open(home)
            .and_then(|dir| dir.sync_all())
            .map_err(|_| INVALID)?;
        Ok(())
    }
}

/// Same message lock as the original log. Validate before intent/Pending and again at POST.
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
    let mut journal = Journal::load(home)?;
    if ledger
        .request(&binding.request_id)
        .is_some_and(|saved| saved != request)
        || api.bot != binding.bot_id
        || journal
            .records
            .get(&binding.request_id)
            .is_some_and(|saved| saved != binding)
        || journal
            .requests
            .get(&binding.request_id)
            .is_some_and(|saved| saved != request)
        || journal
            .records
            .values()
            .any(|saved| saved.request_id != binding.request_id && saved.same_version(binding))
    {
        return Err(INVALID.into());
    }
    if !journal.records.contains_key(&binding.request_id)
        && ledger.effect(&request.request_id, "log:0").is_some()
    {
        return Err(INVALID.into());
    }
    let question = ledger
        .request(&binding.question_request_id)
        .ok_or(INVALID)?;
    let receipt = ledger
        .receipt(&question.request_id, "question:0")
        .ok_or(INVALID)?;
    if question.request_id != binding.question_request_id
        || question.destination != request.destination
        || question.fingerprint != binding.question_fingerprint
        || hash(&question.body) != binding.question_body_sha256
        || !matches!(question.operation, Kind::Ask {todo_id,..} if todo_id == binding.todo_id)
        || ledger.bot(&question.request_id) != Some(binding.bot_id.as_str())
        || receipt.thread_id != binding.thread_id
        || receipt.message_id != binding.question_message_id
        || receipt.author_id != binding.bot_id
    {
        return Err(INVALID.into());
    }
    api.verify(
        ledger,
        question,
        ledger
            .effect(&question.request_id, "question:0")
            .ok_or(INVALID)?,
        &receipt.thread_id,
        &receipt.message_id,
    )?;
    let recovering = journal.records.get(&binding.request_id) == Some(binding)
        && ledger.effect(&request.request_id, "log:0").is_some();
    if recovering && !new_phase {
        return Ok(());
    }
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
            "original question has an explicit close record; no new acknowledgement phase reserved"
                .into(),
        );
    }
    let reply = api.get(&format!(
        "/channels/{}/messages/{}",
        binding.thread_id, binding.reply_message_id
    ))?;
    if !exact_owner_reply(
        &reply,
        &binding.thread_id,
        &binding.reply_message_id,
        &binding.owner_id,
    ) || hash(reply["content"].as_str().ok_or(INVALID)?) != binding.reply_body_sha256
        || reply["timestamp"] != binding.reply_timestamp
        || reply["edited_timestamp"] != binding.reply_edited_timestamp
        || core::n(&reply["id"]) <= core::n(&json!(binding.question_message_id))
        || chrono::DateTime::parse_from_rfc3339(&binding.reply_timestamp).map_err(|_| INVALID)?
            < chrono::DateTime::parse_from_rfc3339(&receipt.timestamp).map_err(|_| INVALID)?
    {
        return Err(INVALID.into());
    }
    SecretCheck::new().text(reply["content"].as_str().ok_or(INVALID)?)?;
    if persist && !journal.records.contains_key(&binding.request_id) {
        journal
            .records
            .insert(binding.request_id.clone(), binding.clone());
        journal
            .requests
            .insert(binding.request_id.clone(), request.clone());
        journal.save(home)?;
    }
    Ok(())
}

fn existing_matches(message: &Value, binding: &Binding) -> bool {
    let Some(body) = message["content"].as_str() else {
        return false;
    };
    let text = body.split("\n\n-# ").next().unwrap_or(body).trim();
    let stamp = message["timestamp"]
        .as_str()
        .and_then(|v| chrono::DateTime::parse_from_rfc3339(v).ok());
    let version = binding
        .reply_edited_timestamp
        .as_str()
        .unwrap_or(&binding.reply_timestamp);
    let version = chrono::DateTime::parse_from_rfc3339(version).ok();
    let markers: Vec<_> = body
        .lines()
        .filter_map(|line| line.strip_prefix("-# owner-receipt:"))
        .collect();
    let version_matches = markers.is_empty()
        || (markers.len() == 1
            && format!("owner-receipt:{}", markers[0]) == binding.version_marker());
    message["channel_id"] == binding.thread_id
        && message["author"]["id"] == binding.bot_id
        && message["author"]["bot"] == true
        && message.get("webhook_id").is_none_or(|v| v.is_null())
        && message["message_reference"]["message_id"] == binding.reply_message_id
        && message["message_reference"]["channel_id"] == binding.thread_id
        && text == "受け取りました。"
        && version_matches
        && core::n(&message["id"]) > core::n(&json!(binding.reply_message_id))
        && stamp
            .zip(version)
            .is_some_and(|(sent, source)| sent >= source)
        && message["flags"]
            .as_u64()
            .is_some_and(|flags| flags & SILENT != 0)
        && message["mentions"].as_array().is_some_and(Vec::is_empty)
        && message["mention_roles"]
            .as_array()
            .is_some_and(Vec::is_empty)
        && message["mention_everyone"] == false
        && message["attachments"].as_array().is_some_and(Vec::is_empty)
        && matches!(message["type"].as_u64(), Some(0 | 19))
}

/// Reuse another path's exact receipt without changing the log effect or sending.
pub(super) fn existing(
    api: &mut RecoveryApi<'_>,
    ledger: &Ledger,
    home: &Path,
    request: &Request,
    binding: &Binding,
    persist: bool,
) -> Result<Option<Value>> {
    let mut journal = Journal::load(home)?;
    let saved = journal.existing.get(&binding.request_id).cloned();
    let matches = if let Some(id) = &saved {
        vec![api.get(&format!("/channels/{}/messages/{id}", binding.thread_id))?]
    } else {
        api.history(&binding.thread_id)?
            .into_iter()
            .filter(|m| {
                existing_matches(m, binding)
                    && ledger
                        .effect(&request.request_id, "log:0")
                        .is_none_or(|effect| m["content"] != effect.content)
            })
            .collect()
    };
    let [message] = matches.as_slice() else {
        return if matches.is_empty() && saved.is_none() {
            Ok(None)
        } else {
            Err("ambiguous existing acknowledgement; no POST".into())
        };
    };
    if !existing_matches(message, binding) {
        return Err("existing acknowledgement changed; no replacement POST".into());
    }
    let id = snowflake(core::s(&message["id"]))?;
    let checked = api.get(&format!("/channels/{}/messages/{id}", binding.thread_id))?;
    if checked["id"] != id || !existing_matches(&checked, binding) {
        return Err("existing acknowledgement changed at readback; no POST".into());
    }
    if persist && saved.is_none() {
        journal
            .existing
            .insert(binding.request_id.clone(), id.clone());
        journal.save(home)?;
    }
    Ok(Some(
        json!({"domain":DOMAIN,"request_id":request.request_id,"delivery":"verified",
        "existing_acknowledgement":true,"thread_id":binding.thread_id,"message_id":id,
        "url":request.destination.url(&binding.thread_id),"notify":false,"owner_mentions":0,
        "reply_delivery":"not_dispatched","application":"not_inferred"}),
    ))
}
