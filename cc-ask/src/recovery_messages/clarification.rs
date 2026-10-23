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
    // Associate each option with its own clause. Reasons in another clause
    // cannot relabel a commitment; a bare "なぜ、" still introduces a question.
    let choice = regex::Regex::new(r"(?i)[a-dａ-ｄ]\s*(?:で|を(?:選んだ|選ぶ|選びます|選びたい|選べ|選択|採用|希望)|に(?:する|すれ|し(?:ます|た|て|よう)|決め)|が(?:いい|良い)|採用)").unwrap();
    let separator = regex::Regex::new(r"[、，,\s…]+|けど|けれど|ので|のに|から").unwrap();
    let reason_query_tail = regex::Regex::new(
        r"(?:のか(?:を)?教えて(?:ください|ほしい(?:です)?)?|ですか|ますか|でしょうか)[。!！]*$",
    )
    .unwrap();
    for sentence in body.split_inclusive(['。', '！', '!', '？', '?', '\n', ';', '；']) {
        let options: Vec<_> = choice.find_iter(sentence).collect();
        let mut spans = Vec::new();
        let mut start = 0;
        for boundary in separator.find_iter(sentence) {
            // Whitespace between an option letter and its predicate belongs to
            // the expression itself, not to the surrounding clause boundary.
            if options
                .iter()
                .any(|option| option.start() < boundary.start() && boundary.end() < option.end())
            {
                continue;
            }
            // Polite question endings are not the conjunction "ので".
            if boundary.as_str() == "ので"
                && ["す", "しょう", "は"]
                    .iter()
                    .any(|ending| sentence[boundary.end()..].starts_with(ending))
            {
                continue;
            }
            spans.push(start..boundary.start());
            start = boundary.end();
        }
        spans.push(start..sentence.len());
        let mut previous = "";
        for span in spans {
            let clause = &sentence[span.clone()];
            for phrase in choice.find_iter(clause) {
                let before = &clause[..phrase.start()];
                if before.chars().next_back().is_some_and(|c| {
                    c.is_ascii_alphanumeric()
                        || matches!(c, 'ａ'..='ｚ' | 'Ａ'..='Ｚ' | '０'..='９')
                }) {
                    continue;
                }
                let after = clause[phrase.end()..].trim_start();
                // The standalone topic "いる" must not turn an option's
                // progressive/confirmation predicate into a necessity question.
                let after = after.replace("ている", "てる").replace("でいる", "でる");
                let prefix_question = (asks_why(before)
                    || matches!(previous, "なぜ" | "どうして" | "なんで"))
                    && (clause.contains(['?', '？']) || reason_query_tail.is_match(&after));
                let tail_question = explanation_requested(&after)
                    || nominal_reason_tail(&after)
                        && explanation_requested(&sentence[span.start + phrase.end()..]);
                // A predicate-attached causal "なんで" is a commitment even
                // when the instruction continues in the next clause.
                let questioned = !colloquial_option_cause(&after)
                    && (prefix_question || tail_question)
                    && !directive_before_reason(&after, phrase.as_str());
                if !questioned && !conditional_option_tail(&after) {
                    return false;
                }
            }
            if !clause.trim().is_empty() {
                previous = clause.trim();
            }
        }
    }
    explanation_requested(body)
}

fn nominal_reason_tail(body: &str) -> bool {
    regex::Regex::new(r"(?:説明|理由|内訳|必要|ひつよう|費用|料金|経費|けいひ|用途|根拠|背景|意味)(?:は|が|も|の)?$|の(?:は|が|か)$")
        .unwrap()
        .is_match(body)
}

// Shared by clause classification and directive inspection. Ordinary replies
// without an option still use asks_why(), without this predicate context.
fn predicate_tail(predicate: &str) -> bool {
    predicate.is_empty()
        || predicate.ends_with([
            'う', 'る', 'た', 'だ', 'い', 'く', 'ぐ', 'す', 'つ', 'ぬ', 'ぶ', 'む', 'な',
        ])
}

fn reason_word_boundary(before: &str) -> bool {
    // A noun such as "同じもの" ends in の without being a nominalized
    // question. Keep an explicit particle or a completed predicate's の.
    let nominal_no = before.strip_suffix('の').is_some_and(predicate_tail);
    // These whole words end in the same kana as a particle but introduce a
    // causal なんで. Do not infer a particle from their final character.
    let lexical_noun = ["しろうと", "いつも"]
        .iter()
        .any(|noun| before.ends_with(noun));
    let particle = !lexical_noun
        && ['と', 'も', 'は', 'が', 'を', 'に', 'へ']
            .iter()
            .any(|&particle| {
                before.strip_suffix(particle).is_some_and(|predicate| {
                    matches!(particle, 'は' | 'を' | 'へ')
                        || predicate_tail(predicate)
                        || predicate.strip_suffix('の').is_some_and(predicate_tail)
                        || matches!(particle, 'も' | 'は') && predicate.ends_with(['て', 'で'])
                        || matches!(particle, 'が' | 'に' | 'も')
                            && predicate.chars().next_back().is_some_and(|c| {
                                matches!(c, '\u{3400}'..='\u{9fff}' | '\u{f900}'..='\u{faff}'
                                    | '\u{30a1}'..='\u{30fa}' | 'ー')
                            })
                })
            });
    before.is_empty()
        || nominal_no
        || particle
        || ["のは", "のが", "のも"]
            .iter()
            .any(|ending| before.ends_with(ending))
        || before
            .chars()
            .next_back()
            .is_some_and(|c| c.is_whitespace() || "、，,".contains(c))
}

fn nominal_before_ga(before: &str) -> bool {
    // Comparison and quantity subjects are not the adversative verb ending
    // used by "払うが、理由は？". Keep later commitment markers detectable.
    [
        "ほう",
        "多く",
        "ひとつ",
        "ふたつ",
        "みっつ",
        "よっつ",
        "いつつ",
        "むっつ",
        "ななつ",
        "やっつ",
        "ここのつ",
        "いくつ",
        "幾つ",
    ]
    .iter()
    .any(|noun| before.ends_with(noun))
        || before.strip_suffix('つ').is_some_and(|number| {
            number
                .chars()
                .next_back()
                .is_some_and(|c| c.is_numeric() || "〇一二三四五六七八九十百千".contains(c))
        })
}

fn colloquial_option_cause(body: &str) -> bool {
    let Some(start) = why_start(body) else {
        return false;
    };
    let Some(tail) = body[start..].strip_prefix("なんで") else {
        return false;
    };
    if reason_word_boundary(&body[..start]) {
        return false;
    }
    // "十分なんですか" / "なんでしょうか" are questions, unlike
    // "十分なんで、進めて" or the assertion "十分なんですよ".
    let tail = tail.trim_end_matches(|c: char| c.is_whitespace() || "。?!？！ー笑".contains(c));
    !["すか", "すかね", "しょうか", "しょうかね"].contains(&tail)
}

fn directive_before_reason(body: &str, option: &str) -> bool {
    let topic = regex::Regex::new(
        r"説明|理由|内訳|必要|ひつよう|費用|料金|経費|けいひ|用途|根拠|背景|意味|何のため",
    )
    .unwrap();
    let no_ka = regex::Regex::new(r"のか(?:を)?教えて").unwrap();
    let topic_start = topic.find(body).map(|m| m.start());
    let raw_why = why_start(body);
    let no_ka_start = no_ka.find(body).map(|m| m.start());
    // Confirmation endings remain assertions before any reason word, even
    // when that word is not a grammatical boundary for the full predicate.
    let assertion_cutoff = [topic_start, raw_why, no_ka_start]
        .into_iter()
        .flatten()
        .min()
        .unwrap_or(body.len());
    let cutoff = [
        topic_start,
        raw_why.filter(|&start| reason_word_boundary(&body[..start])),
        no_ka_start,
    ]
    .into_iter()
    .flatten()
    .min()
    .unwrap_or(body.len());
    let prefix = &body[..cutoff];
    let joined = format!("{option}{}", &body[..assertion_cutoff]);
    // The choice expression may itself consume て ("aにして"). Inspect the
    // complete prefix, while a following 理由 keeps the genuine reason query
    // "してもらえる理由は？" outside the request pattern.
    if regex::Regex::new(r"て(?:もらえ|いただけ)(?:ますか|ませんか|る\s*[?？])")
        .unwrap()
        .is_match(&format!("{option}{prefix}"))
    {
        return true;
    }
    if regex::Regex::new(r"[うくぐすつぬぶむるただい]が|けれど|(?:よ(?:ね)?|[oO][kK])\s*$")
        .unwrap()
        .find_iter(&joined)
        .any(|word| {
            !word.as_str().ends_with('が')
                || !nominal_before_ga(&joined[..word.end() - 'が'.len_utf8()])
        })
    {
        return true;
    }
    regex::Regex::new(r"お願い|ます|です|て")
        .unwrap()
        .find_iter(prefix)
        .any(|word| {
            let tail = &prefix[word.end()..];
            match word.as_str() {
                "て" => {
                    // A topic particle directly before the recognized reason
                    // is not an instruction. Keep standalone "ては？" rejected.
                    tail != "は"
                        && !["も", "る", "いる", "いた", "いま", "いな"]
                            .iter()
                            .any(|suffix| tail.starts_with(suffix))
                }
                "です" | "ます" => !tail.starts_with(['か', '?', '？']),
                _ => true,
            }
        })
}

fn conditional_option_tail(remaining: &str) -> bool {
    let remaining = remaining.trim_start();
    // A conditional is not itself a commitment. A recommendation needs its own
    // explanation question, checked by the caller before this fallback.
    if regex::Regex::new(r"^(?:ら|ば|なら|と|あれば|あるなら|したら|すれば)(?:いい|良い|よい)")
        .unwrap()
        .is_match(remaining)
    {
        return false;
    }
    let bare = remaining.trim_end_matches([
        '。', '、', '，', ',', '！', '!', '？', '?', '\n', ';', '；', ' ',
    ]);
    bare == "と"
        || regex::Regex::new(r"^と(?:[、，,\s]|何|どう|ど|費用|料金|必要|理由|経費|内訳|用途|機能)")
            .unwrap()
            .is_match(remaining)
        || [
            "ら",
            "ば",
            "なら",
            "あれば",
            "あるなら",
            "したら",
            "すれば",
            "場合",
            "とき",
        ]
        .iter()
        .any(|suffix| remaining.starts_with(suffix))
        || remaining.strip_prefix('時').is_some_and(|tail| {
            tail.chars()
                .next()
                .is_none_or(|next| next.is_whitespace() || "はにのも、，,。？！?!".contains(next))
        })
}

fn explanation_requested(body: &str) -> bool {
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
            (topic.is_match(sentence) && teach.is_match(sentence))
                || regex::Regex::new(r"^のか(?:を)?教えて(?:ください|ほしい(?:です)?)?$")
                    .unwrap()
                    .is_match(sentence)
                || more.is_match(sentence)
        });
    let question = body.contains(['?', '？'])
        || regex::Regex::new(r"(?:なの|ですか|ますか|どういう意味|何のため)\s*[。！!]*\s*$")
            .unwrap()
            .is_match(body);
    explicit || detail || asks_why(body) || question && regex::Regex::new(r"(?i)どういう|何の|何で|要る|いる|必要|ひつよう|理由|内訳|用途|根拠|経費|けいひ|費用|料金|why|what").unwrap().is_match(body)
}

fn asks_why(body: &str) -> bool {
    why_start(body).is_some()
}

fn why_start(body: &str) -> Option<usize> {
    regex::Regex::new(r"なぜ|どうして|なんで")
        .unwrap()
        .find_iter(body)
        .find(|word| {
            let tail = &body[word.end()..];
            match word.as_str() {
                "なぜ" => {
                    if tail.starts_with("なら") {
                        return false;
                    }
                    tail.strip_prefix('か').is_none_or(|after| {
                        after.is_empty()
                            || ["教え", "分か", "わか", "な？", "な?", "？", "?", "。", "を"]
                                .iter()
                                .any(|text| after.starts_with(text))
                    })
                }
                "どうして" | "なんで" => !tail.starts_with('も'),
                _ => false,
            }
        })
        .map(|word| word.start())
}

#[cfg(test)]
mod intent_tests {
    use super::requests_explanation;

    #[test]
    fn choice_and_reason_matrix_keeps_clause_order_and_predicates() {
        let mut mismatches = Vec::new();
        for separator in ["、", ",", " ", "　", "…", "けど", "ので", "のに", "から"] {
            let reason_prefix = match separator {
                "けど" | "のに" => "なんで迷うか分からない",
                "ので" | "から" => "なんで迷うか分かった",
                _ => "なんで迷うの",
            };
            for (decision, instruction) in [
                ("aにする", "aで進めてって言ったよね"),
                ("bにした", "bでお願いします"),
                ("cにすると決めた", "cにしたらいいでしょ"),
                ("dを採用する", "dを採用してほしい"),
            ] {
                for reply in [
                    format!("{decision}{separator}なんで必要？"),
                    format!("{reason_prefix}{separator}{instruction}？"),
                ] {
                    if requests_explanation(&reply) {
                        mismatches.push((reply, false));
                    }
                }
            }
        }
        for separator in ["", "、", ",", " ", "　", "…"] {
            for choice in ["aがいい", "bにした", "cを選ぶ", "dを採用する"] {
                let reply = format!("なぜ{separator}{choice}の？");
                if !requests_explanation(&reply) {
                    mismatches.push((reply, true));
                }
            }
        }
        for predicate in ["aでも", "bでの", "cで進めたら", "dを採用すると"] {
            for topic in ["費用", "料金", "必要な理由"] {
                let reply = format!("{predicate}{topic}は？");
                if !requests_explanation(&reply) {
                    mismatches.push((reply, true));
                }
            }
        }
        for choice in ["aで", "a で", "ａ　で"] {
            let committed = format!("{choice}進めて、理由も教えて");
            if requests_explanation(&committed) {
                mismatches.push((committed, false));
            }
            let hypothetical = format!("{choice}進めたら費用は？");
            if !requests_explanation(&hypothetical) {
                mismatches.push((hypothetical, true));
            }
        }
        for reply in ["なぜ、aにしたのですか？", "なぜaで進めているのですか？"]
        {
            if !requests_explanation(reply) {
                mismatches.push((reply.into(), true));
            }
        }
        for option in ["aにした", "bを選んだ", "cを採用した"] {
            for question in [
                format!("{option}のはなぜか教えて"),
                format!("なぜ{option}のか教えて？"),
                format!("なんで、勝手に{option}の？"),
                format!("{option}のは、なぜですか？"),
                format!("{option}のが、なぜですか？"),
                format!("なぜ{option}のでしょうか？"),
                format!("なぜ{option}のではないですか？"),
            ] {
                if !requests_explanation(&question) {
                    mismatches.push((question, true));
                }
            }
        }
        for option in ["aで", "bで", "ｃ で"] {
            for (predicate, explanation) in [
                ("決まっているよね？", "決まっている理由は？"),
                ("決まってるよね？", "決まってる理由は？"),
                ("行くけれど費用は？", "行く理由は？"),
                ("いいよ理由も教えて", "いい理由も教えて"),
                ("OK理由も教えて", "OKな理由も教えて"),
            ] {
                let committed = format!("{option}{predicate}");
                if requests_explanation(&committed) {
                    mismatches.push((committed, false));
                }
                let question = format!("{option}{explanation}");
                if !requests_explanation(&question) {
                    mismatches.push((question, true));
                }
            }
        }
        assert!(mismatches.is_empty(), "{mismatches:?}");
    }

    #[test]
    fn clarification_colloquial_cause_does_not_hide_a_directive() {
        let mut mismatches = Vec::new();
        for option in ["aで", "bで", "ｃ で"] {
            for predicate in ["大丈夫", "十分", "決まり"] {
                for ending in [
                    "なんでお願いします",
                    "なんですが、進めてください",
                    "なんで進めて",
                    "なんですよ。進めて",
                ] {
                    let reply = format!("{option}{predicate}{ending}");
                    if requests_explanation(&reply) {
                        mismatches.push((reply, false));
                    }
                }
                for ending in ["のはなんで？", "なのはなんで？"] {
                    let reply = format!("{option}{predicate}{ending}");
                    if !requests_explanation(&reply) {
                        mismatches.push((reply, true));
                    }
                }
            }
            for why in ["なぜ", "なんで", "どうして"] {
                for ending in ["のですか", "んですか", "のでしょうか"] {
                    let reply = format!("{why}{option}進めている{ending}");
                    if !requests_explanation(&reply) {
                        mismatches.push((reply, true));
                    }
                }
            }
        }
        assert!(mismatches.is_empty(), "{mismatches:?}");
    }

    #[test]
    fn clarification_reason_boundary_keeps_commitments_across_clauses() {
        let mut mismatches = Vec::new();
        for option in ["aで", "b で", "ｃで"] {
            for marker in ["いいよ", "いいよね", "OK"] {
                for reason in ["なんで？", "なぜ必要？", "どうして聞くの？"] {
                    let reply = format!("{option}{marker}{reason}");
                    if requests_explanation(&reply) {
                        mismatches.push((reply, false));
                    }
                }
            }
            for predicate in ["大丈夫", "十分", "決まり", "同じもの", "別のもの"] {
                for ending in ["なんですか", "なんでしょうか"] {
                    for prefix in ["", "なぜ"] {
                        let reply = format!("{prefix}{option}{predicate}{ending}");
                        if !requests_explanation(&reply) {
                            mismatches.push((reply, true));
                        }
                    }
                }
                for separator in [",", "、", " ", "　", "。", ";", "！"] {
                    for instruction in ["お願いします", "進めて"] {
                        let reply = format!("{option}{predicate}なんで{separator}{instruction}");
                        if requests_explanation(&reply) {
                            mismatches.push((reply, false));
                        }
                    }
                }
                for separator in ["", "、", " "] {
                    let reply = format!("{option}{predicate}なのは{separator}なんで？");
                    if !requests_explanation(&reply) {
                        mismatches.push((reply, true));
                    }
                }
            }
            for ending in ["てもらえますか", "ていただけますか"] {
                for punctuation in ["", "？"] {
                    let reply =
                        format!("どうして止まったか知らないが{option}進め{ending}{punctuation}");
                    if requests_explanation(&reply) {
                        mismatches.push((reply, false));
                    }
                }
            }
        }
        assert!(mismatches.is_empty(), "{mismatches:?}");
    }

    #[test]
    fn clarification_particle_questions_keep_conditional_and_nominal_forms() {
        let mut mismatches = Vec::new();
        for option in ["aで", "b で", "ｃで"] {
            for verb in ["払う", "使う", "買う"] {
                let committed = format!("{option}{verb}がなんで必要？");
                if requests_explanation(&committed) {
                    mismatches.push((committed, false));
                }
                for reason in ["なんで必要？", "どうして？"] {
                    let reply = format!("{option}{verb}の{reason}");
                    if !requests_explanation(&reply) {
                        mismatches.push((reply, true));
                    }
                }
            }
            for predicate in ["同じもの", "同じこと", "決まり"] {
                for tail in [
                    "なんで、お願いします",
                    "なんで。進めて",
                    "なんですから、進めて",
                ] {
                    let reply = format!("{option}{predicate}{tail}");
                    if requests_explanation(&reply) {
                        mismatches.push((reply, false));
                    }
                }
            }
            for ornament in ["ー？", "笑"] {
                let reply = format!("なぜ{option}十分なんですか{ornament}");
                if !requests_explanation(&reply) {
                    mismatches.push((reply, true));
                }
            }
        }
        for conditional in [
            "aにすると",
            "bでも",
            "cにしても",
            "dを採用すると",
            "aで働くと",
        ] {
            for reason in ["なんでダメなの？", "なんで必要なの？"] {
                let reply = format!("{conditional}{reason}");
                if !requests_explanation(&reply) {
                    mismatches.push((reply, true));
                }
            }
        }
        assert!(mismatches.is_empty(), "{mismatches:?}");
    }

    #[test]
    fn clarification_noun_particles_and_comparisons_preserve_reason_scope() {
        let mut mismatches = Vec::new();
        for option in ["aで", "b で", "ｃで"] {
            for noun in [
                "費用は",
                "料金を",
                "これへ",
                "システムが",
                "プランに",
                "プランも",
            ] {
                for why in ["なぜ", "なんで", "どうして"] {
                    let question = format!("{option}{noun}{why}必要？");
                    if !requests_explanation(&question) {
                        mismatches.push((question, true));
                    }
                }
            }
            for predicate in ["進めても", "進めては", "選んでも", "選んでは"] {
                let question = format!("{option}{predicate}なんで必要？");
                if !requests_explanation(&question) {
                    mismatches.push((question, true));
                }
            }
            for noun in ["ほう", "多く", "ひとつ", "ふたつ", "みっつ", "一つ", "2つ"]
            {
                let question = format!("{option}{noun}が安い理由は？");
                if !requests_explanation(&question) {
                    mismatches.push((question, true));
                }
            }
            for verb in ["払う", "働く", "選ぶ", "待つ"] {
                let commitment = format!("{option}{verb}がそれはなんで必要？");
                if requests_explanation(&commitment) {
                    mismatches.push((commitment, false));
                }
            }
            let recommendation = format!("どうして止まったか知らないが{option}進めては？");
            if requests_explanation(&recommendation) {
                mismatches.push((recommendation, false));
            }
            for request in [
                "てもらえませんか？",
                "ていただけませんか？",
                "てもらえる？",
                "ていただける？",
            ] {
                let commitment = format!("どうして止まったか知らないが{option}進め{request}");
                if requests_explanation(&commitment) {
                    mismatches.push((commitment, false));
                }
            }
            for request in ["てもらえる理由は？", "ていただける理由は？"] {
                let question = format!("{option}進め{request}");
                if !requests_explanation(&question) {
                    mismatches.push((question, true));
                }
            }
            for noun in ["同じこと", "しろうと", "いつも"] {
                let commitment = format!("{option}{noun}なんで、お願いします");
                if requests_explanation(&commitment) {
                    mismatches.push((commitment, false));
                }
            }
        }
        assert!(mismatches.is_empty(), "{mismatches:?}");
    }

    #[test]
    fn clarification_polite_request_spans_the_option_boundary() {
        let mut mismatches = Vec::new();
        for ending in [
            "もらえますか",
            "もらえませんか",
            "いただけますか",
            "いただけませんか",
            "もらえる？",
            "いただける？",
        ] {
            let reply = format!("どうして止まったか知らないがaにして{ending}理由も教えて");
            if requests_explanation(&reply) {
                mismatches.push((reply, false));
            }
        }
        for ending in ["もらえる理由は？", "いただける理由は？"] {
            let reply = format!("aにして{ending}");
            if !requests_explanation(&reply) {
                mismatches.push((reply, true));
            }
        }
        assert!(mismatches.is_empty(), "{mismatches:?}");
    }
}
