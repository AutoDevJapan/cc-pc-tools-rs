use cc_redact::{MARK, Redactor};
use serde_json::json;

#[test]
fn a_bearer_credential_does_not_require_digits() {
    for value in [
        ["AbCdEfGhIjKlMn", "OpQrStUvWxYz"].concat(),
        ["abcdefghijklmn", "opqrstuvwxyz"].concat(),
        "Z".repeat(32),
    ] {
        assert_eq!(
            Redactor::new().text(&format!("Bearer {value}")),
            format!("Bearer {MARK}")
        );
    }
}

#[test]
fn explicit_credential_fields_do_not_trust_placeholder_prefixes() {
    let redactor = Redactor::new();
    for value in [
        ["example-", "live-credential-9x"].concat(),
        ["dummy-", "live-credential-7z"].concat(),
        ["$Pa55", "-word!"].concat(),
        "Q".repeat(32),
        [MARK, "-remaining-private-suffix"].concat(),
    ] {
        assert_eq!(redactor.field("serviceSecret", &value), MARK);
        let mut body = json!({"licenseKey": value});
        redactor.json(&mut body);
        assert_eq!(body["licenseKey"], MARK);
    }
    for reference in ["$TOKEN", "${TOKEN}", "<token>", MARK] {
        assert_eq!(redactor.field("serviceSecret", reference), reference);
    }
}

#[test]
fn known_api_key_formats_do_not_require_digits() {
    for value in [
        ["sk", "-", &"abcdefghijklmno".repeat(3)].concat(),
        ["sk", "-proj-", &"abcdefghijklmno".repeat(3)].concat(),
    ] {
        assert_eq!(Redactor::new().text(&value), MARK);
    }
    let encoded = ["QUJDREVGR0hJSktMTU5P", "UFFSU1RVVldYWVo="].concat();
    assert_eq!(
        Redactor::new().text(&format!("esa {encoded}")),
        format!("esa {MARK}")
    );
}

#[test]
fn numeric_credentials_are_masked_without_changing_usage_counters() {
    let mut body = json!({"token": 709331, "licenseKey": 602177, "input_tokens": 12});
    Redactor::new().json(&mut body);
    assert_eq!(body["token"], MARK);
    assert_eq!(body["licenseKey"], MARK);
    assert_eq!(body["input_tokens"], 12);
}

#[test]
fn all_credential_value_types_and_short_literals_are_hidden() {
    for value in [
        json!({"value":"opaque"}),
        json!(["opaque"]),
        json!(false),
        json!(null),
    ] {
        let mut body = json!({"serviceSecret":value,"ordinary":{"value":"visible"}});
        Redactor::new().json(&mut body);
        assert_eq!(body["serviceSecret"], MARK);
        assert_eq!(body["ordinary"]["value"], "visible");
    }
    let redactor = Redactor::new();
    for literal in ["abc123", "abc...", "example-key", "QQQQQQ"] {
        assert_eq!(
            redactor.text(&format!("serviceSecret={literal}")),
            format!("serviceSecret={MARK}")
        );
        assert_eq!(
            redactor.text(&format!("https://u:{literal}@host.test/p")),
            format!("https://u:{MARK}@host.test/p")
        );
    }
    assert_eq!(
        redactor.text("password: \"\" token = null secret=false"),
        format!("password: \"\" token = {MARK} secret={MARK}")
    );
    assert_eq!(
        redactor.text("the password: Please enter it"),
        format!("the password: {MARK} enter it")
    );
    assert!(!redactor.contains_secret("serviceSecret=$TOKEN"));
    assert!(!redactor.contains_secret("secret = getSecret()"));
}

#[test]
fn short_named_environment_secrets_are_registered_without_corrupting_identifiers() {
    let redactor = Redactor::with_env([("ODD_SERVICE_TOKEN", "s6!")]);
    assert_eq!(redactor.text("out: s6!"), format!("out: {MARK}"));
    assert!(redactor.contains_secret("s6!"));
    let single = Redactor::with_env([("ODD_SERVICE_TOKEN", "1")]);
    assert_eq!(single.text("1"), MARK);
    assert_eq!(
        single.text("gpt-6.1-sol 019abc123"),
        "gpt-6.1-sol 019abc123"
    );
}
#[test]
fn single_quoted_multiline_credential_masks_the_whole_value() {
    let raw = "secret='artificial gamma\ndelta'\n正常な本文";
    let masked = cc_redact::Redactor::new().text(raw).into_owned();
    assert!(!masked.contains("gamma"));
    assert!(!masked.contains("delta"));
    assert!(masked.ends_with("\n正常な本文"));
}
