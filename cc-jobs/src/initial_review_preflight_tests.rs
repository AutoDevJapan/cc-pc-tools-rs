use super::*;
use std::sync::Mutex;
struct Probe {
    key: bool,
    calls: Mutex<Vec<Vec<String>>>,
}
impl Runtime for Probe {
    fn mono(&self) -> f64 {
        0.
    }
    fn epoch(&self) -> f64 {
        100000.
    }
    fn env(&self) -> BTreeMap<String, String> {
        BTreeMap::from([("APPDATA".into(), "synthetic".into())])
    }
    fn call(&self, c: Call) -> io::Result<Reply> {
        self.calls.lock().unwrap().push(c.argv.clone());
        let raw = if c.argv.iter().any(|a| a == "config") {
            json!({})
        } else {
            json!({"accounts":[{"type":if self.key{"api-key"}else{"codex"},"health":"Healthy",
                "needsReauth":false,"creditsAfterLimit":false,
                "quota":{"weeklyPercent":10.,"weeklyResetAt":200000.}}]})
        };
        Ok(Reply {
            code: 0,
            stdout: raw.to_string().into_bytes(),
            stderr: vec![],
        })
    }
    fn spawn(&self, _: &Spawn) -> io::Result<Box<dyn Running>> {
        panic!("never execute a model")
    }
    fn spawn_supervisor(&self, _: &Path, _: &Path, _: &Path, _: &str) -> io::Result<bool> {
        panic!("never create a job")
    }
}
#[test]
fn api_key_only_never_reaches_quota_refresh_and_oauth_is_a_positive_control() {
    for key in [true, false] {
        let p = Probe {
            key,
            calls: Mutex::new(vec![]),
        };
        let inventory = normal_quota_inventory(&p, true, true);
        let calls = p.calls.lock().unwrap();
        assert_eq!(
            calls
                .iter()
                .filter(|c| c.iter().any(|a| a == "--refresh"))
                .count(),
            if key { 0 } else { 2 }
        );
        assert_eq!(inventory["openai"]["verified"] == true, !key);
    }
}
