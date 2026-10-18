//! Mask credentials in text and JSON before they are stored or published.
//!
//! Detection is structural: known token prefixes, credential assignments,
//! Authorization headers and URL passwords. Prose, identifiers and placeholders
//! such as `$TOKEN`, `<token>` or `${{ secrets.X }}` stay as they are. A match
//! is always replaced as a whole, so no part of an encoded value remains.
use regex::{Captures, Regex};
use serde_json::Value;
use std::{borrow::Cow, sync::LazyLock};

pub const MARK: &str = "[REDACTED]";

/// Names whose value is a credential, matched case-insensitively at the end
/// (`serviceSecret`, `licenseKey`, `GH_TOKEN`, `client_secret`, ...).
const KEY: &str = r"[A-Za-z0-9_.-]*?(?:password|passwd|passphrase|secret|token|api[_-]?key|apikey|license[_-]?key|private[_-]?key|access[_-]?key|auth[_-]?key|signing[_-]?key|session[_-]?key|credentials?|webhook[_-]?url|connection[_-]?string)";

fn re(pattern: &str) -> Regex {
    Regex::new(pattern).unwrap()
}
static SECRET_KEY: LazyLock<Regex> =
    LazyLock::new(|| re(&format!(r"(?i)^(?:{KEY}|(?:proxy-)?authorization)$")));
static PEM: LazyLock<Regex> = LazyLock::new(|| {
    re(r"(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----(?:.*?-----END [A-Z0-9 ]*PRIVATE KEY-----|.*)")
});
static WEBHOOK: LazyLock<Regex> = LazyLock::new(|| {
    re(
        r#"(?i)https://(?:hooks\.slack\.com/services|(?:canary\.|ptb\.)?discord(?:app)?\.com/api/webhooks)/[^\s"'<>\\]+"#,
    )
});
// The password may contain '@'; the greedy match ends at the last one before the host.
static URL_CREDENTIAL: LazyLock<Regex> =
    LazyLock::new(|| re(r#"(?i)\b([a-z][a-z0-9+.-]*://)([^\s/?#@:'"<>\\]*):([^\s/?#'"<>\\]+)@"#));
static AUTHORIZATION: LazyLock<Regex> = LazyLock::new(|| {
    re(
        r#"(?i)\b((?:proxy-)?authorization\\?["']?\s*[:=]\s*\\?["']?)(?:([a-z][a-z0-9_-]*)(\s+))?([A-Za-z0-9._~+/=%-]+)"#,
    )
});
static BEARER: LazyLock<Regex> =
    LazyLock::new(|| re(r"(?i)\b(bearer\s+)([A-Za-z0-9._~+/-]{16,}=*)"));
static BASIC: LazyLock<Regex> = LazyLock::new(|| re(r"(?i)\b(basic\s+)([A-Za-z0-9+/]{8,}={0,2})"));
static ESA: LazyLock<Regex> = LazyLock::new(|| re(r"(?i)\b(ESA\s+)([A-Za-z0-9+/_-]{16,}={0,2})"));
static TOKEN: LazyLock<Regex> = LazyLock::new(|| {
    re(concat!(
        r"\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,}",
        r"|(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}|whsec_[A-Za-z0-9+/=]{16,}",
        r"|(?:AKIA|ASIA)[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{35}|xox[abposr]-[A-Za-z0-9-]{10,}",
        r"|glpat-[A-Za-z0-9_-]{20,}|npm_[A-Za-z0-9]{36}|hf_[A-Za-z0-9]{30,}",
        r"|eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}",
        r"|[MNO][A-Za-z0-9_-]{23,27}\.[A-Za-z0-9_-]{6}\.[A-Za-z0-9_-]{27,})"
    ))
});
// OpenAI/Anthropic style keys; preserve ordinary hyphenated branch names.
static SK: LazyLock<Regex> = LazyLock::new(|| re(r"\bsk-[A-Za-z0-9_-]{20,}"));
// A quoted value without its closing quote (cut off, or continued on later
// lines) is masked to the end of the text, so no suffix stays readable.
static ASSIGNMENT: LazyLock<Regex> = LazyLock::new(|| {
    re(&format!(
        r#"(?i)({KEY})(\\?["']?\s*(?:=>|:=|[:=]|%3a|%3d)\s*)(?:\\"((?:[^"\\]|\\[^"])*)(?:\\"|\z)|"((?:[^"\\]|\\(?s:.))*)"?|'([^']*)'?|(\$\{{\{{[^{{}}]*\}}\}}|\$\{{[^{{}}]*\}}|<[^<>]*>|\{{\{{[^{{}}]*\}}\}}|(?:[A-Za-z_][A-Za-z0-9_.:]*\(\)|[^\s"'\\,;&<>(){{}}\[\]`]|\\[^"'])+))"#
    ))
});
static PEM_BEGIN: LazyLock<Regex> = LazyLock::new(|| re(r"-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----"));
static PEM_END: LazyLock<Regex> = LazyLock::new(|| re(r"-----END [A-Z0-9 ]*PRIVATE KEY-----"));
/// A credential assignment whose double-quoted value is still open at the end.
static OPEN_QUOTE: LazyLock<Regex> = LazyLock::new(|| {
    re(&format!(
        r#"(?i){KEY}\\?["']?\s*(?:=>|:=|[:=])\s*"(?:[^"\\]|\\(?s:.))*\z"#
    ))
});
static CLOSE_QUOTE: LazyLock<Regex> = LazyLock::new(|| re(r#"(?:^|[^\\])(?:\\\\)*""#));

/// Values that only name or describe a credential.
fn placeholder(value: &str) -> bool {
    let v = value.trim();
    let lower = v.to_ascii_lowercase();
    let first = v.chars().next();
    v.is_empty()
        || v.contains(MARK)
        || v.contains("秘密")
        || v.contains("***")
        || v.contains("...")
        || v.contains('…')
        || matches!(first, Some('$' | '%' | '<' | '{' | '['))
        || v.chars().all(|c| Some(c) == first)
        || [
            "null",
            "none",
            "nil",
            "true",
            "false",
            "undefined",
            "string",
            "redacted",
            "changeme",
        ]
        .contains(&lower.as_str())
        || [
            "your",
            "example",
            "dummy",
            "placeholder",
            "process.env",
            "os.environ",
            "env.",
            "secrets.",
            "vars.",
            "import.meta.env",
            "std::env",
        ]
        .iter()
        .any(|p| lower.starts_with(p))
}

/// Under an explicit credential name only a pure reference stays readable
/// (`$TOKEN`, `${TOKEN}`, `%TOKEN%`, `<token>`, `{{ x }}`, the mark itself);
/// prefixes such as `example-` prove nothing about the value.
fn reference(value: &str) -> bool {
    static REF: LazyLock<Regex> = LazyLock::new(|| {
        re(
            r"^(?:\$[A-Za-z_][A-Za-z0-9_]*|\$\{[^{}]*\}|\$\{\{[^{}]*\}\}|%[A-Za-z_][A-Za-z0-9_]*%|<[^<>]*>|\{\{[^{}]*\}\})$",
        )
    });
    let v = value.trim();
    v.is_empty() || v == MARK || REF.is_match(v)
}

// actions/checkout's input is a boolean switch, not credential material.
// Keep the exception value-sensitive: other values and credential-named
// fields (including an outer credentials object) retain normal redaction.
fn checkout_boolean(name: &str, value: &str) -> bool {
    name.eq_ignore_ascii_case("persist-credentials") && matches!(value, "true" | "false")
}

fn has_digit(v: &str) -> bool {
    v.bytes().any(|b| b.is_ascii_digit())
}

/// A Basic credential is base64 of printable `user:password`.
fn basic_credential(b64: &str) -> bool {
    let (mut bits, mut n, mut out) = (0u32, 0, Vec::new());
    for c in b64.bytes().take_while(|&c| c != b'=') {
        let d = match c {
            b'A'..=b'Z' => c - b'A',
            b'a'..=b'z' => c - b'a' + 26,
            b'0'..=b'9' => c - b'0' + 52,
            b'+' => 62,
            b'/' => 63,
            _ => return false,
        };
        bits = bits << 6 | d as u32;
        n += 6;
        if n >= 8 {
            n -= 8;
            out.push((bits >> n) as u8);
        }
    }
    out.contains(&b':') && out.iter().all(|&b| b >= 0x20 && b != 0x7f)
}

fn apply(text: &mut Cow<'_, str>, re: &Regex, f: impl FnMut(&Captures) -> String) {
    let changed = match re.replace_all(text, f) {
        Cow::Owned(s) => Some(s),
        Cow::Borrowed(_) => None,
    };
    if let Some(s) = changed {
        *text = Cow::Owned(s);
    }
}

/// Explicit credential assignments mask every literal value. Only complete
/// references and unquoted environment accesses/empty calls stay readable.
fn assignment(c: &Captures) -> String {
    let whole = c.get(0).unwrap();
    let value = c.get(3).or(c.get(4)).or(c.get(5)).or(c.get(6)).unwrap();
    let v = value.as_str();
    static EXPR: LazyLock<Regex> = LazyLock::new(|| {
        re(
            r"^(?:[A-Za-z_][A-Za-z0-9_.:]*\(\)|os\.environ|(?:process\.|import\.meta\.)?env\.[A-Za-z_][A-Za-z0-9_]*|std::env::[A-Za-z_][A-Za-z0-9_:]*)$",
        )
    });
    if checkout_boolean(&c[1], v) || reference(v) || (c.get(6).is_some() && EXPR.is_match(v)) {
        return whole.as_str().to_string();
    }
    let (start, end) = (value.start() - whole.start(), value.end() - whole.start());
    format!(
        "{}{MARK}{}",
        &whole.as_str()[..start],
        &whole.as_str()[end..]
    )
}

/// Text and JSON redaction, optionally with exact values known to be secret.
#[derive(Debug, Clone, Default)]
pub struct Redactor {
    literals: Vec<String>,
}

impl Redactor {
    pub fn new() -> Self {
        Self::default()
    }

    /// Also mask the values of credential-named environment variables
    /// (`GH_TOKEN`, `OPENAI_API_KEY`, ...), whatever their format.
    pub fn with_env<K: AsRef<str>, V: AsRef<str>>(vars: impl IntoIterator<Item = (K, V)>) -> Self {
        let mut literals = vars
            .into_iter()
            .filter(|(k, v)| {
                let v = v.as_ref();
                // Paths (GOOGLE_APPLICATION_CREDENTIALS=C:\...) name a secret, they are not one.
                SECRET_KEY.is_match(k.as_ref())
                    && !v.is_empty()
                    && !reference(v)
                    && !std::path::Path::new(v).is_absolute()
            })
            .map(|(_, v)| v.as_ref().to_string())
            .collect::<Vec<_>>();
        // Longer values first so a value containing another is masked whole.
        literals.sort_by_key(|v| std::cmp::Reverse(v.len()));
        literals.dedup();
        Self { literals }
    }

    pub fn text<'a>(&self, input: &'a str) -> Cow<'a, str> {
        let mut out = Cow::Borrowed(input);
        for literal in &self.literals {
            if out.contains(literal.as_str()) {
                // Short values must not corrupt IDs or executable/model names
                // merely containing the same digit or letters.
                if literal.len() < 8 {
                    let text = out.as_ref();
                    let identifier = |c: char| c.is_alphanumeric() || matches!(c, '_' | '-' | '.');
                    let mut masked = String::new();
                    let mut end = 0;
                    for (at, value) in text.match_indices(literal) {
                        let after = at + value.len();
                        let left = text[..at]
                            .chars()
                            .next_back()
                            .is_none_or(|c| !identifier(c));
                        let right = text[after..].chars().next().is_none_or(|c| !identifier(c));
                        if left && right {
                            masked.push_str(&text[end..at]);
                            masked.push_str(MARK);
                            end = after;
                        }
                    }
                    if end > 0 {
                        masked.push_str(&text[end..]);
                        out = Cow::Owned(masked);
                    }
                } else {
                    out = Cow::Owned(out.replace(literal.as_str(), MARK));
                }
            }
        }
        apply(&mut out, &PEM, |_| MARK.into());
        apply(&mut out, &WEBHOOK, |_| MARK.into());
        apply(&mut out, &URL_CREDENTIAL, |c| {
            if reference(&c[3]) {
                c[0].to_string()
            } else {
                format!("{}{}:{MARK}@", &c[1], &c[2])
            }
        });
        apply(&mut out, &AUTHORIZATION, |c| {
            let credential = &c[4];
            let secret = match c.get(2) {
                Some(_) => credential.len() >= 8,
                None => credential.len() >= 16 && has_digit(credential),
            };
            if !secret || placeholder(credential) {
                return c[0].to_string();
            }
            let scheme = c
                .get(2)
                .map_or(String::new(), |s| format!("{}{}", s.as_str(), &c[3]));
            format!("{}{scheme}{MARK}", &c[1])
        });
        apply(&mut out, &BEARER, |c| format!("{}{MARK}", &c[1]));
        apply(&mut out, &BASIC, |c| {
            if basic_credential(&c[2]) {
                format!("{}{MARK}", &c[1])
            } else {
                c[0].to_string()
            }
        });
        apply(&mut out, &ESA, |c| format!("{}{MARK}", &c[1]));
        apply(&mut out, &TOKEN, |_| MARK.into());
        apply(&mut out, &SK, |c| {
            let token = &c[0];
            if has_digit(token)
                || ["sk-proj-", "sk-svcacct-", "sk-ant-"]
                    .iter()
                    .any(|p| token.starts_with(p))
                || token[3..]
                    .bytes()
                    .all(|b| b.is_ascii_alphanumeric() || b == b'_')
            {
                MARK.into()
            } else {
                c[0].to_string()
            }
        });
        apply(&mut out, &ASSIGNMENT, assignment);
        out
    }

    /// Sanitize every string inside a JSON value. Serialized JSON stays valid
    /// because only parsed string contents change, never quotes or escapes.
    pub fn json(&self, value: &mut Value) {
        match value {
            Value::String(s) => {
                let masked = match self.text(s) {
                    Cow::Owned(m) => Some(m),
                    Cow::Borrowed(_) => None,
                };
                if let Some(m) = masked {
                    *s = m;
                }
            }
            Value::Array(items) => items.iter_mut().for_each(|v| self.json(v)),
            Value::Object(map) => {
                for (key, v) in map.iter_mut() {
                    if key.eq_ignore_ascii_case("persist-credentials")
                        && (v.is_boolean() || v.as_str().is_some_and(|s| checkout_boolean(key, s)))
                    {
                        self.json(v);
                        continue;
                    }
                    if SECRET_KEY.is_match(key) {
                        if !matches!(v, Value::String(s) if reference(s)) {
                            *v = MARK.into();
                        }
                    } else {
                        self.json(v);
                    }
                }
                if map.keys().any(|k| matches!(self.text(k), Cow::Owned(_))) {
                    *map = std::mem::take(map)
                        .into_iter()
                        .map(|(k, v)| (self.text(&k).into_owned(), v))
                        .collect();
                }
            }
            _ => (),
        }
    }

    pub fn contains_secret(&self, text: &str) -> bool {
        self.text(text).as_ref() != text
    }

    /// The value of a named field (`licenseKey=...`, `variables[token]=...`).
    /// A credential name masks the whole value, whatever its format.
    pub fn field<'a>(&self, name: &str, value: &'a str) -> Cow<'a, str> {
        if is_secret_field(name) && !reference(value) && !checkout_boolean(name, value) {
            Cow::Borrowed(MARK)
        } else {
            self.text(value)
        }
    }
}

/// Masks text that arrives line by line. A private key block or a quoted
/// credential that continues on later lines is held until it closes (up to
/// `LIMIT` bytes), then masked as a whole. Past the limit the masked prefix is
/// written and later lines are dropped until the block closes, so no part of
/// the value is ever written.
#[derive(Debug, Default)]
pub struct Stream {
    pending: String,
    skip: Option<&'static LazyLock<Regex>>,
}

impl Stream {
    pub const LIMIT: usize = 64 * 1024;

    /// Returns the text that can be written now.
    pub fn line(&mut self, mut line: &str, mask: impl Fn(&str) -> String) -> String {
        if let Some(close) = self.skip {
            let Some(m) = close.find(line) else {
                return String::new();
            };
            self.skip = None;
            line = &line[m.end()..];
        }
        self.pending.push_str(line);
        if let Some(close) = open_block(&self.pending) {
            if self.pending.len() < Self::LIMIT {
                return String::new();
            }
            self.skip = Some(close);
        }
        mask(&std::mem::take(&mut self.pending))
    }

    /// Text still held at the end of the stream; an unclosed block is masked to the end.
    pub fn finish(&mut self, mask: impl Fn(&str) -> String) -> String {
        self.skip = None;
        mask(&std::mem::take(&mut self.pending))
    }
}

/// The closing pattern of a private key or quoted credential still open at the end.
fn open_block(text: &str) -> Option<&'static LazyLock<Regex>> {
    if let Some(begin) = PEM_BEGIN.find_iter(text).last()
        && !PEM_END.is_match(&text[begin.end()..])
    {
        return Some(&PEM_END);
    }
    OPEN_QUOTE.is_match(text).then_some(&CLOSE_QUOTE)
}

pub fn redact_text(text: &str) -> Cow<'_, str> {
    Redactor::default().text(text)
}

pub fn redact_json(value: &mut Value) {
    Redactor::default().json(value)
}

pub fn contains_secret(text: &str) -> bool {
    Redactor::default().contains_secret(text)
}

/// True for names such as `serviceSecret`, `licenseKey` or `Authorization`.
pub fn is_secret_key(name: &str) -> bool {
    SECRET_KEY.is_match(name)
}

/// Like `is_secret_key`, for nested field syntax: `variables[licenseKey]`, `token[]`.
pub fn is_secret_field(name: &str) -> bool {
    name.split(['[', ']']).any(is_secret_key)
}
