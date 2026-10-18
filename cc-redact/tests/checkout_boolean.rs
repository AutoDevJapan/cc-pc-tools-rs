use cc_redact::{MARK, Redactor, Stream};
use serde_json::json;

#[test]
fn checkout_workflow_content_keeps_its_exact_bytes() {
    let r = Redactor::new();
    for value in ["true", "false", "False", "TRUE", "'false'", "\"false\""] {
        let workflow = format!(
            "steps:\n  - uses: actions/checkout@v4\n    with:\n      persist-credentials: {value}\n      fetch-depth: 1\n"
        );
        assert_eq!(r.text(&workflow), workflow);
        let mut payload =
            json!({"tree": [{"path": ".github/workflows/ci.yml", "content": workflow}]});
        let original = payload.clone();
        r.json(&mut payload);
        assert_eq!(payload, original);

        let mut stream = Stream::default();
        let streamed: String = workflow
            .split_inclusive('\n')
            .map(|line| stream.line(line, |s| r.text(s).into_owned()))
            .collect();
        assert_eq!(
            streamed + &stream.finish(|s| r.text(s).into_owned()),
            workflow
        );
    }
}

#[test]
fn only_the_named_boolean_setting_is_preserved() {
    let r = Redactor::new();
    for value in ["false", "true", "False", "TRUE"] {
        assert_eq!(r.field("persist-credentials", value), value);
        for key in [
            "credentials",
            "clientSecret",
            "other-persist-credentials",
            "credentials[persist-credentials]",
        ] {
            assert_eq!(r.field(key, value), MARK);
        }
        let mut payload = json!({
            "persist-credentials": value,
            "with": {"persist-credentials": value == "true"},
            "credentials": {"persist-credentials": value},
            "clientSecret": false,
        });
        r.json(&mut payload);
        assert_eq!(payload["persist-credentials"], value);
        assert_eq!(payload["with"]["persist-credentials"], value == "true");
        assert_eq!(payload["credentials"], MARK);
        assert_eq!(payload["clientSecret"], MARK);
    }
    for value in [
        "opaque-value",
        "false-private",
        "false\\nsuffix",
        "false private",
    ] {
        assert_eq!(r.field("persist-credentials", value), MARK);
        let mut payload = json!({"persist-credentials": value});
        r.json(&mut payload);
        assert_eq!(payload["persist-credentials"], MARK);
    }
    assert_eq!(
        r.text("persist-credentials: opaque-value"),
        format!("persist-credentials: {MARK}")
    );
    let mut payload = json!({"persist-credentials": {"secret": "opaque-value"}});
    r.json(&mut payload);
    assert_eq!(payload["persist-credentials"], MARK);
}
