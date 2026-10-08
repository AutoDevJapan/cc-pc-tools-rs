//! Every secret here is synthetic and assembled at run time so the source holds
//! no string that looks like a real credential.
use cc_redact::*;
use serde_json::json;

fn r(text: &str) -> String {
    redact_text(text).into_owned()
}
fn synthetic(prefix: &str, unit: &str, n: usize) -> String {
    format!("{prefix}{}", unit.repeat(n))
}
fn b64(raw: &str) -> String {
    const T: &[u8] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    let mut out = String::new();
    for chunk in raw.as_bytes().chunks(3) {
        let b = [
            chunk[0],
            *chunk.get(1).unwrap_or(&0),
            *chunk.get(2).unwrap_or(&0),
        ];
        let n = (b[0] as u32) << 16 | (b[1] as u32) << 8 | b[2] as u32;
        for i in 0..4 {
            out.push(if i <= chunk.len() {
                T[(n >> (18 - 6 * i) & 63) as usize] as char
            } else {
                '='
            });
        }
    }
    out
}
/// Neither the secret nor any half of it may survive.
fn assert_gone(out: &str, secret: &str) {
    let half = secret.len() / 2;
    for part in [secret, &secret[..half], &secret[half..]] {
        assert!(!out.contains(part), "leaked {part:?} in {out:?}");
    }
    assert!(out.contains(MARK), "no mark in {out:?}");
}

#[test]
fn masks_esa_and_http_authorization_schemes() {
    let esa = b64("svc-7Q2:synthetic-4f1e9b77c0d2");
    for (text, keep) in [
        (format!("Authorization: ESA {esa}"), "Authorization: ESA "),
        (
            format!("curl -H 'authorization: ESA {esa}' x"),
            "authorization: ESA ",
        ),
        (format!("header ESA {esa} sent"), "ESA "),
    ] {
        let out = r(&text);
        assert_gone(&out, &esa);
        assert!(out.contains(keep), "{out}");
    }
    let basic = b64("builder:Pa55-synthetic-91");
    let out = r(&format!("Authorization: Basic {basic}"));
    assert_eq!(out, format!("Authorization: Basic {MARK}"));
    let out = r(&format!("use basic {basic} here"));
    assert_gone(&out, &basic);
    let bearer = synthetic("v1.", "Zx9Kq2Lm", 4);
    let out = r(&format!("-H \"Authorization: Bearer {bearer}\""));
    assert_eq!(out, format!("-H \"Authorization: Bearer {MARK}\""));
    assert_gone(&r(&format!("token is Bearer {bearer}.")), &bearer);
}

#[test]
fn masks_service_secret_and_license_key_assignments() {
    let secret = synthetic("sv", "7Hq2Xw", 4);
    for text in [
        format!("serviceSecret = \"{secret}\""),
        format!("serviceSecret: {secret}"),
        format!("{{\"serviceSecret\":\"{secret}\"}}"),
        format!("SERVICE_SECRET={secret} next"),
        format!("licenseKey='{secret}'"),
        format!("licenseKey => {secret}"),
        format!("\\\"licenseKey\\\":\\\"{secret}\\\""),
        format!("?license_key={secret}&page=2"),
        format!("password%3D{secret}"),
    ] {
        assert_gone(&r(&text), &secret);
    }
    // Quotes, separators and following text are kept.
    assert_eq!(
        r(&format!("x serviceSecret = \"{secret}\"; y")),
        format!("x serviceSecret = \"{MARK}\"; y")
    );
    assert_eq!(
        r(&format!("?license_key={secret}&page=2")),
        format!("?license_key={MARK}&page=2")
    );
}

#[test]
fn masks_known_token_forms_and_url_credentials() {
    let tokens = [
        synthetic("gh", "p_", 1) + &"Ab3De5Gh7J".repeat(4),
        synthetic("github_", "pat_", 1) + &"11AbCdEf22_".repeat(5),
        synthetic("sk-", "proj-", 1) + &"Q1w2E3r4T5".repeat(4),
        synthetic("sk_", "live_", 1) + &"z9Y8x7W6".repeat(3),
        synthetic("AK", "IA", 1) + "Q7W3E5R1T9Y2U4I6",
        synthetic("AI", "za", 1) + &"Sy1-Ab2_Cd3".repeat(3) + "Ef",
        synthetic("xo", "xb-", 1) + "1234567890-abcdefABCDEF",
        synthetic("eyJ", "hbGciOi1", 1) + ".eyJzdWIiOjEyMzQ1Njc4OTB9." + &"Sf1KxwRJ".repeat(3),
        synthetic("gl", "pat-", 1) + &"Xy12_Zw34-".repeat(3),
        synthetic("hf", "_", 1) + &"Ab12Cd34Ef".repeat(4),
    ];
    for token in &tokens {
        let out = r(&format!("value {token} end"));
        assert_eq!(out, format!("value {MARK} end"), "{token}");
    }
    let pw = synthetic("pw", "9xQ2", 3);
    let out = r(&format!(
        "git clone https://builder:{pw}@git.example.test/r.git"
    ));
    assert_eq!(
        out,
        format!("git clone https://builder:{MARK}@git.example.test/r.git")
    );
    let out = r(&format!("https://u:{pw}@x@host.test/p"));
    assert_gone(&out, &pw);
    let hook = format!("https://discord.com/api/webhooks/123/{}", "Ab1".repeat(10));
    assert_eq!(r(&hook), MARK);
    let pem = format!(
        "a\n-----BEGIN RSA PRIVATE KEY-----\n{}\n-----END RSA PRIVATE KEY-----\nb",
        "MIIx".repeat(20)
    );
    assert_eq!(r(&pem), format!("a\n{MARK}\nb"));
}

#[test]
fn keeps_prose_identifiers_and_placeholders() {
    for text in [
        "Use a Bearer token from $GITHUB_TOKEN",
        "token: <token>",
        "password = ${{ secrets.DB_PASSWORD }}",
        "api_key = os.environ['API_KEY']",
        "secret = getSecret()",
        "max_tokens=4096 and input_tokens: 105429",
        "Basic understanding of authorization: required for everyone",
        "Authorization header is missing",
        "branch sk-feature-make-redaction-better",
        "ghp_short task-1234 item_123 01a0e883-7628-7121-9540-6780fd4cd6f4",
        "https://github.com/owner/repo/pull/12 git@github.com:owner/repo.git",
        "https://user:${PASSWORD}@host.test/",
        "licenseKey: [REDACTED]",
        "C:\\Users\\me\\AppData\\Local\\Temp\\job\\last_message.md",
        r#"{"type":"token_count","total_token_usage":1}"#,
    ] {
        assert_eq!(r(text), text, "over-redacted");
    }
}

#[test]
fn json_strings_are_masked_without_breaking_quotes_or_escapes() {
    let token = synthetic("gh", "o_", 1) + &"Zz9Yy8Xx7W".repeat(4);
    let secret = synthetic("s", "Q7r2", 4);
    let mut v = json!({
        "type": "item.completed",
        "item": {
            "command": format!("curl -H \"Authorization: token {token}\" -d '{{\"serviceSecret\":\"{secret}\"}}'"),
            "aggregated_output": format!("line1\r\n\"licenseKey\": \"{secret}\"\\n"),
            "arguments": {"serviceSecret": secret, "password": "$DB_PASSWORD", "name": "keep"},
        },
        "nested": [format!("x {token}"), 5, null],
    });
    redact_json(&mut v);
    let serialized = v.to_string();
    let back: serde_json::Value = serde_json::from_str(&serialized).unwrap();
    assert_eq!(back, v);
    assert_gone(&serialized, &token);
    assert_gone(&serialized, &secret);
    assert_eq!(v["item"]["arguments"]["password"], "$DB_PASSWORD");
    assert_eq!(v["item"]["arguments"]["name"], "keep");
    assert_eq!(v["item"]["arguments"]["serviceSecret"], MARK);
    assert_eq!(v["type"], "item.completed");
}

#[test]
fn redaction_is_idempotent_and_env_values_are_masked_whole() {
    let value = synthetic("Q", "w8-Er", 3);
    let r = Redactor::with_env([
        ("ODD_FORMAT_TOKEN", value.as_str()),
        ("GOOGLE_APPLICATION_CREDENTIALS", "C:\\keys\\svc.json"),
        ("PATH", "C:\\Windows"),
    ]);
    let text = format!("got {value} and C:\\keys\\svc.json");
    let once = r.text(&text).into_owned();
    assert_eq!(once, format!("got {MARK} and C:\\keys\\svc.json"));
    assert_eq!(r.text(&once), once);
    assert!(r.contains_secret(&text));
    assert!(!r.contains_secret("plain words"));
    assert!(is_secret_key("serviceSecret") && is_secret_key("licenseKey"));
    assert!(!is_secret_key("total_token_usage"));
}

#[test]
fn quoted_values_cut_off_or_wrapped_over_lines_are_masked_whole() {
    let a = synthetic("q", "Xr4tY", 3);
    let b = synthetic("w", "Pz7mK", 3);
    for text in [
        format!("licenseKey = \"{a}\n{b}\" rest"),
        format!("serviceSecret: \"{a}\n{b}\" rest"),
        format!("{{\\\"licenseKey\\\":\\\"{a}{b}"),
        format!("serviceSecret: \"{a}{b}"),
        format!("serviceSecret='{a}{b}"),
    ] {
        let out = r(&text);
        assert_gone(&out, &a);
        assert_gone(&out, &b);
    }
    assert_eq!(
        r("password: \"see the docs\" ok"),
        format!("password: \"{MARK}\" ok")
    );
}

#[test]
fn credential_field_names_mask_the_whole_value() {
    let red = Redactor::new();
    for name in [
        "licenseKey",
        "variables[serviceSecret]",
        "input[token][]",
        "GH_TOKEN",
    ] {
        assert_eq!(red.field(name, "opaque"), MARK, "{name}");
    }
    assert_eq!(red.field("token", "$TOKEN"), "$TOKEN");
    assert_eq!(red.field("title", "opaque"), "opaque");
    assert_eq!(red.field("labels[]", "bug"), "bug");
    assert!(!is_secret_field("token_type") && !is_secret_field("secret_name"));
    let mut v = json!({"licenseKey": 12345678, "count": 3});
    redact_json(&mut v);
    assert_eq!(v, json!({"licenseKey": MARK, "count": 3}));
}

fn stream(lines: &[&str]) -> String {
    let mut s = Stream::default();
    let mask = |t: &str| redact_text(t).into_owned();
    let mut out = String::new();
    for line in lines {
        out += &s.line(line, mask);
    }
    out + &s.finish(mask)
}

#[test]
fn stream_masks_private_keys_and_quoted_values_split_over_lines() {
    let body = synthetic("MII", "Ev4Qb7Wn", 8);
    let tail = synthetic("x", "K9pLm2", 6);
    let (begin, end) = (
        "-----BEGIN RSA PRIVATE KEY-----",
        "-----END RSA PRIVATE KEY-----",
    );
    let out = stream(&[
        "before\n",
        &format!("key: {begin}\n"),
        &format!("{body}\n"),
        &format!("{tail}\n"),
        &format!("{end} after\n"),
        "plain line\n",
    ]);
    assert_gone(&out, &body);
    assert_gone(&out, &tail);
    assert!(
        out.starts_with("before\nkey: ") && out.ends_with(" after\nplain line\n"),
        "{out}"
    );

    // A block that never closes is masked to the end of the stream.
    let out = stream(&[&format!("{begin}\n"), &format!("{body}\n")]);
    assert_gone(&out, &body);

    // Past the hold limit the masked prefix is written and the rest dropped.
    let mut s = Stream::default();
    let mask = |t: &str| redact_text(t).into_owned();
    let mut out = s.line(&format!("{begin}\n"), mask);
    let line = format!("{body}\n");
    for _ in 0..(Stream::LIMIT / line.len() + 2) {
        out += &s.line(&line, mask);
    }
    out += &s.line(&format!("{end}\n"), mask);
    out += &s.line("visible\n", mask);
    out += &s.finish(mask);
    assert_gone(&out, &body);
    assert!(out.ends_with("visible\n"), "{out}");

    let out = stream(&[
        &format!("serviceSecret = \"{body}\n"),
        &format!("{tail}\"\n"),
        "done\n",
    ]);
    assert_gone(&out, &body);
    assert_gone(&out, &tail);
    assert!(out.ends_with("done\n"));

    let prose = ["a normal line\n", "\"quoted\" words\n", "the end\n"];
    assert_eq!(stream(&prose), prose.concat());
}
