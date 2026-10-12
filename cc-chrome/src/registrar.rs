//! Owner's registrar restriction. Never infer authorization from saved credentials.
use serde_json::Value;

pub const REASON: &str = "ドメイン管理はRakkoのみです。指定外の事業者への表示・ログイン・登録・調査アクセスは、対象を指定したオーナーの明示依頼がない限り禁止です。保存済み認証や一般のログイン代行許可は対象サイトの許可ではありません。";

// These are known registrar endpoints, not a claim that every registrar is listed.
const RESTRICTED: &[&str] = &[
    "onamae.com",
    "onamae.jp",
    "gmoid.jp",
    "gmo.jp",
    "muumuu-domain.com",
    "value-domain.com",
    "xdomain.ne.jp",
    "xserver-domain.com",
    "namecheap.com",
    "godaddy.com",
    "domains.google",
];

pub fn restricted_url(text: &str) -> bool {
    let Ok(url) = url::Url::parse(text) else {
        return false;
    };
    let Some(host) = url.host_str() else {
        return false;
    };
    let host = host.trim_end_matches('.');
    RESTRICTED
        .iter()
        .any(|root| host == *root || host.ends_with(&format!(".{root}")))
}

/// Only inspect request fields; do not log URLs, page text or credentials.
pub fn rpc_denied(method: &str, params: &Value) -> bool {
    match method {
        "createTab" => params
            .get("url")
            .and_then(Value::as_str)
            .is_some_and(restricted_url),
        "executeCdp" => {
            matches!(
                params["method"].as_str(),
                Some("Page.navigate" | "Target.createTarget" | "Network.loadNetworkResource")
            ) && params["commandParams"]["url"]
                .as_str()
                .is_some_and(restricted_url)
        }
        _ => false,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    #[test]
    fn parsed_host_handles_case_userinfo_and_trailing_dot_without_substring_matches() {
        for target in [
            "https://ONAMAE.COM/",
            "https://navi.onamae.com./",
            "https://rakko-domain.com@onamae.com/",
            "https://%6fnamae.com/",
            "https://www.gmoid.jp/",
        ] {
            assert!(restricted_url(target), "{target}");
        }
        for target in [
            "https://rakko-domain.com/",
            "https://dash.cloudflare.com/",
            "https://admin.google.com/",
            "https://onamae.com.example.org/",
            "https://example.org/?url=https://onamae.com",
            "https://onamae.com@example.org/",
            "about:blank",
        ] {
            assert!(!restricted_url(target), "{target}");
        }
    }
    #[test]
    fn creation_and_navigation_share_the_policy_but_cleanup_is_always_available() {
        assert!(rpc_denied(
            "createTab",
            &json!({"url":"https://onamae.com"})
        ));
        for method in [
            "Page.navigate",
            "Target.createTarget",
            "Network.loadNetworkResource",
        ] {
            assert!(rpc_denied(
                "executeCdp",
                &json!({"method":method,"commandParams":{"url":"https://navi.onamae.com"}})
            ));
            assert!(!rpc_denied(
                "executeCdp",
                &json!({"method":method,"commandParams":{"url":"https://rakko-domain.com"}})
            ));
        }
        for method in ["closeTab", "getUserTabs", "getInfo"] {
            assert!(!rpc_denied(method, &json!({"url":"https://onamae.com"})));
        }
    }
}
