//! Read-only health observations; a failed optional backend cannot hide the console.
use serde_json::{Map, Value, json};
use std::time::{Duration, Instant};

const PROBE_TIMEOUT: Duration = Duration::from_millis(1500);
const BODY_LIMIT: usize = 2 * 1024 * 1024;
const SERVICE_KEYS: [&str; 3] = ["godd", "opencodex", "docker"];

#[derive(Clone, Copy)]
enum Service {
    Godd,
    OpenCodex,
    #[cfg(test)]
    Ollama,
    #[cfg(test)]
    DeepSeek,
}

pub struct HealthSnapshot {
    started: Instant,
    endpoints: [String; 2],
    #[cfg(test)]
    docker_override: Option<Value>,
}

impl HealthSnapshot {
    pub fn production() -> Self {
        Self {
            started: Instant::now(),
            endpoints: [
                "http://127.0.0.1:10101/_godd_ao/healthz".into(),
                "http://127.0.0.1:10100/healthz".into(),
            ],
            #[cfg(test)]
            docker_override: None,
        }
    }

    // Every HTTP observation in route tests stays inside the test's own fixture.
    #[cfg(test)]
    pub fn for_test(base: &str) -> Self {
        let mut snapshot = Self::production();
        snapshot.endpoints = [
            format!("{base}/_godd_ao/healthz"),
            format!("{base}/healthz"),
        ];
        snapshot.docker_override = Some(observation(
            Err(Failure::Unavailable),
            "docker-pipe",
            "\\\\.\\pipe\\docker_engine",
        ));
        snapshot
    }

    pub async fn snapshot(&self, client: &reqwest::Client, _backend: &str, port: u16) -> Value {
        let (godd, opencodex, docker) = tokio::join!(
            service_snapshot(None, client, &self.endpoints[0], Service::Godd),
            service_snapshot(None, client, &self.endpoints[1], Service::OpenCodex),
            self.docker_snapshot(None),
        );
        let services = [godd, opencodex, docker];
        let partial = services
            .iter()
            .any(|service| !service["online"].is_boolean());
        let mut health = serde_json::Map::new();
        for (key, value) in SERVICE_KEYS.into_iter().zip(services) {
            health.insert(key.into(), value);
        }
        health.insert("port".into(), json!(port));
        health.insert("status".into(), json!("ok"));
        health.insert("backend".into(), json!("Rust/Axum"));
        health.insert("checkedAt".into(), json!(checked_at()));
        health.insert("partial".into(), json!(partial));
        health.insert(
            "console".into(),
            json!({"online":true,"status":"ok","statusText":"稼働中","port":port,
            "pid":std::process::id(),"uptimeSeconds":self.started.elapsed().as_secs()}),
        );
        Value::Object(health)
    }
    async fn docker_snapshot(&self, previous: Option<&Value>) -> Value {
        if let Some(value) = legacy_service(previous) {
            return value;
        }
        #[cfg(test)]
        if let Some(value) = &self.docker_override {
            return merge_details(previous, value.clone());
        }
        merge_details(
            previous,
            observation(
                docker_ping().await,
                "docker-pipe",
                "\\\\.\\pipe\\docker_engine",
            ),
        )
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Failure {
    Unavailable,
    Timeout,
    Http(u16),
    InvalidResponse,
    BodyTooLarge,
    ReadFailed,
}

fn checked_at() -> i64 {
    chrono::Utc::now().timestamp_millis()
}

fn observation(result: Result<Value, Failure>, source: &str, endpoint: &str) -> Value {
    let result = result.and_then(|value| {
        let online = value.get("online").and_then(Value::as_bool);
        online
            .map(|online| (value, online))
            .ok_or(Failure::InvalidResponse)
    });
    let mut value = match result {
        Ok((value, online)) => {
            let mut value = value;
            value["online"] = json!(online);
            value["status"] = json!(if online { "ok" } else { "error" });
            value["statusText"] = json!(if online {
                "稼働中"
            } else {
                "健康APIが異常を報告しています"
            });
            value
        }
        Err(error) => {
            let (reason, text) = match error {
                Failure::Unavailable => ("unavailable", "接続先に到達できません（状態未取得）"),
                Failure::Timeout => ("timeout", "確認が時間内に終わりませんでした（状態未取得）"),
                Failure::Http(_) => ("http-status", "健康APIを取得できません（状態未取得）"),
                Failure::InvalidResponse => (
                    "invalid-response",
                    "応答の形式やサービス名を確認できません（状態未取得）",
                ),
                Failure::BodyTooLarge => {
                    ("body-too-large", "応答が大きすぎるため状態を取得できません")
                }
                Failure::ReadFailed => ("read-failed", "応答を読み取れません（状態未取得）"),
            };
            let mut value =
                json!({"online":null,"status":"unknown","statusText":text,"reason":reason});
            if let Failure::Http(status) = error {
                value["httpStatus"] = json!(status);
            }
            value
        }
    };
    value["source"] = json!(source);
    value["endpoint"] = json!(endpoint);
    value["checkedAt"] = json!(checked_at());
    value
}

// Only an explicit boolean from a successful backend is an observed service state.
fn legacy_service(previous: Option<&Value>) -> Option<Value> {
    let mut object = previous?.as_object()?.clone();
    let online = take_key(&mut object, "online")?.as_bool()?;
    object.insert("online".into(), json!(online));
    for key in [
        "source",
        "checkedAt",
        "status",
        "statusText",
        "endpoint",
        "modelCount",
        "httpStatus",
    ] {
        if let Some(value) = take_key(&mut object, key) {
            object.insert(key.into(), value);
        }
    }
    object.entry("source").or_insert(json!("legacy-backend"));
    object.entry("checkedAt").or_insert(json!(checked_at()));
    object
        .entry("status")
        .or_insert(json!(if online { "ok" } else { "offline" }));
    object.entry("statusText").or_insert(json!(if online {
        "稼働中"
    } else {
        "起動していません"
    }));
    Some(Value::Object(object))
}

fn merge_details(previous: Option<&Value>, observed: Value) -> Value {
    let mut value = previous
        .and_then(Value::as_object)
        .cloned()
        .unwrap_or_default();
    // Keep descriptive legacy fields, but never carry a previous failure or count
    // into a fresh observation that could not confirm it.
    for key in [
        "online",
        "status",
        "statusText",
        "reason",
        "error",
        "source",
        "endpoint",
        "checkedAt",
        "httpStatus",
        "modelCount",
    ] {
        take_key(&mut value, key);
    }
    value.extend(
        observed
            .as_object()
            .expect("health observation object")
            .clone(),
    );
    Value::Object(value)
}

fn take_key(object: &mut Map<String, Value>, canonical: &str) -> Option<Value> {
    let keys: Vec<_> = object
        .keys()
        .filter(|key| key.eq_ignore_ascii_case(canonical))
        .cloned()
        .collect();
    let mut value = object.remove(canonical);
    for key in keys {
        let other = object.remove(&key);
        if value.is_none() {
            value = other;
        }
    }
    value
}

async fn service_snapshot(
    previous: Option<&Value>,
    client: &reqwest::Client,
    endpoint: &str,
    kind: Service,
) -> Value {
    if let Some(value) = legacy_service(previous) {
        return value;
    }
    let result = read_json(client, endpoint, PROBE_TIMEOUT)
        .await
        .and_then(|value| service_details(kind, &value));
    merge_details(previous, observation(result, "loopback-http", endpoint))
}

fn service_details(kind: Service, value: &Value) -> Result<Value, Failure> {
    match kind {
        Service::Godd | Service::OpenCodex => {
            let expected = match kind {
                Service::Godd => "godd-ao-semantic-proxy",
                _ => "opencodex",
            };
            if value["service"] != expected {
                return Err(Failure::InvalidResponse);
            }
            let mut details = json!({"online":reported_online(value)?, "service":expected});
            for key in ["version", "pid", "port", "uptime"] {
                if let Some(field) = value.get(key) {
                    details[key] = field.clone();
                }
            }
            Ok(details)
        }
        #[cfg(test)]
        Service::Ollama => {
            let models = value["models"].as_array().ok_or(Failure::InvalidResponse)?;
            if models.iter().any(|model| {
                !["name", "model"]
                    .iter()
                    .any(|key| model[*key].as_str().is_some_and(|name| !name.is_empty()))
            }) {
                return Err(Failure::InvalidResponse);
            }
            Ok(json!({"online":true,"modelCount":models.len()}))
        }
        #[cfg(test)]
        Service::DeepSeek => Ok(json!({"online":reported_online(value)?})),
    }
}

fn reported_online(value: &Value) -> Result<bool, Failure> {
    match value["status"].as_str() {
        Some("ok") => Ok(true),
        Some("error" | "unhealthy" | "loading") => Ok(false),
        _ => Err(Failure::InvalidResponse),
    }
}

async fn read_json(
    client: &reqwest::Client,
    endpoint: &str,
    deadline: Duration,
) -> Result<Value, Failure> {
    let request = async {
        let mut response = client
            .get(endpoint)
            .timeout(deadline)
            .send()
            .await
            .map_err(|error| {
                if error.is_timeout() {
                    Failure::Timeout
                } else {
                    Failure::Unavailable
                }
            })?;
        if !response.status().is_success() {
            return Err(Failure::Http(response.status().as_u16()));
        }
        if response
            .content_length()
            .is_some_and(|size| size > BODY_LIMIT as u64)
        {
            return Err(Failure::BodyTooLarge);
        }
        let mut body = Vec::new();
        while let Some(chunk) = response.chunk().await.map_err(|error| {
            if error.is_timeout() {
                Failure::Timeout
            } else {
                Failure::ReadFailed
            }
        })? {
            if body.len() + chunk.len() > BODY_LIMIT {
                return Err(Failure::BodyTooLarge);
            }
            body.extend_from_slice(&chunk);
        }
        serde_json::from_slice(&body).map_err(|_| Failure::InvalidResponse)
    };
    tokio::time::timeout(deadline, request)
        .await
        .unwrap_or(Err(Failure::Timeout))
}

#[cfg(windows)]
async fn docker_ping() -> Result<Value, Failure> {
    use tokio::{
        io::{AsyncReadExt, AsyncWriteExt},
        net::windows::named_pipe::ClientOptions,
    };
    let request = async {
        let mut pipe = ClientOptions::new()
            .open("\\\\.\\pipe\\docker_engine")
            .map_err(|_| Failure::Unavailable)?;
        // HEAD needs no body and cannot launch Docker, change a context or enumerate containers.
        pipe.write_all(b"HEAD /_ping HTTP/1.1\r\nHost: docker\r\nConnection: close\r\n\r\n")
            .await
            .map_err(|_| Failure::ReadFailed)?;
        let mut headers = Vec::new();
        let mut chunk = [0; 1024];
        loop {
            let read = pipe
                .read(&mut chunk)
                .await
                .map_err(|_| Failure::ReadFailed)?;
            if read == 0 {
                return Err(Failure::InvalidResponse);
            }
            headers.extend_from_slice(&chunk[..read]);
            if headers.len() > 8192 {
                return Err(Failure::BodyTooLarge);
            }
            if headers.windows(4).any(|bytes| bytes == b"\r\n\r\n") {
                return docker_headers(&headers);
            }
        }
    };
    tokio::time::timeout(PROBE_TIMEOUT, request)
        .await
        .unwrap_or(Err(Failure::Timeout))
}

#[cfg(not(windows))]
async fn docker_ping() -> Result<Value, Failure> {
    Err(Failure::Unavailable)
}

fn docker_headers(bytes: &[u8]) -> Result<Value, Failure> {
    let headers = std::str::from_utf8(bytes).map_err(|_| Failure::InvalidResponse)?;
    if !headers.contains("\r\n\r\n") {
        return Err(Failure::InvalidResponse);
    }
    let status = headers.lines().next().unwrap_or("");
    if !status.starts_with("HTTP/1.") {
        return Err(Failure::InvalidResponse);
    }
    let code = status
        .split_whitespace()
        .nth(1)
        .and_then(|code| code.parse::<u16>().ok())
        .ok_or(Failure::InvalidResponse)?;
    if code != 200 {
        return Err(Failure::Http(code));
    }
    let mut details = json!({"online":true});
    for line in headers.lines().skip(1).take_while(|line| !line.is_empty()) {
        if let Some((name, value)) = line.split_once(':') {
            if name.eq_ignore_ascii_case("api-version") {
                let version = value.trim();
                if !version.is_empty()
                    && version.len() < 32
                    && version
                        .bytes()
                        .all(|byte| byte.is_ascii_digit() || byte == b'.')
                {
                    details["apiVersion"] = json!(version);
                }
            }
        }
    }
    Ok(details)
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::{Json, Router, http::StatusCode, response::IntoResponse, routing::get};
    use tokio::io::AsyncWriteExt;

    struct Fixture {
        base: String,
        task: tokio::task::JoinHandle<()>,
    }

    impl Drop for Fixture {
        fn drop(&mut self) {
            self.task.abort();
        }
    }

    async fn fixture(router: Router) -> Fixture {
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let base = format!("http://{}", listener.local_addr().unwrap());
        let task = tokio::spawn(async move {
            axum::serve(listener, router).await.unwrap();
        });
        Fixture { base, task }
    }

    fn client() -> reqwest::Client {
        reqwest::Client::builder()
            .no_proxy()
            .redirect(reqwest::redirect::Policy::none())
            .build()
            .unwrap()
    }

    #[test]
    fn verifies_service_identity_and_never_invents_missing_model_counts() {
        assert_eq!(
            service_details(Service::Godd, &json!({"service":"opencodex","status":"ok"})),
            Err(Failure::InvalidResponse),
        );
        assert_eq!(
            service_details(
                Service::OpenCodex,
                &json!({"service":"opencodex","status":"unknown"})
            ),
            Err(Failure::InvalidResponse),
        );
        assert_eq!(
            service_details(Service::Ollama, &json!({})),
            Err(Failure::InvalidResponse)
        );
        assert_eq!(
            service_details(Service::Ollama, &json!({"models":[3]})),
            Err(Failure::InvalidResponse)
        );
        assert_eq!(
            service_details(Service::Ollama, &json!({"models":[]})).unwrap(),
            json!({"online":true,"modelCount":0})
        );
        assert_eq!(
            service_details(
                Service::Ollama,
                &json!({"models":[{"name":"fixture-model"}]})
            )
            .unwrap()["modelCount"],
            1
        );
        assert_eq!(
            service_details(Service::DeepSeek, &json!({"status":"loading"})).unwrap()["online"],
            false
        );
        assert_eq!(
            service_details(Service::DeepSeek, &json!({"status":"unknown"})),
            Err(Failure::InvalidResponse)
        );
    }

    #[test]
    fn unobserved_services_remain_null_and_old_failures_do_not_replace_new_health() {
        for unobserved in [json!({}), json!({"online":null}), json!({"online":"true"})] {
            let value = observation(Ok(unobserved), "loopback-http", "fixture");
            assert!(value["online"].is_null());
            assert_eq!(value["reason"], "invalid-response");
        }
        let previous = json!({"online":null,"status":"failed","error":"previous failure","reason":"timeout","httpStatus":500,"modelCount":0,"model":"installed"});
        let unknown = merge_details(
            Some(&previous),
            observation(Err(Failure::Timeout), "loopback-http", "fixture"),
        );
        assert!(unknown["online"].is_null());
        assert!(unknown["modelCount"].is_null());
        assert!(unknown["error"].is_null());
        assert!(unknown["httpStatus"].is_null());
        assert_eq!(unknown["model"], "installed");
        let healthy = merge_details(
            Some(&previous),
            observation(
                Ok(json!({"online":true,"modelCount":2})),
                "loopback-http",
                "fixture",
            ),
        );
        assert_eq!(healthy["online"], true);
        assert_eq!(healthy["modelCount"], 2);
        assert!(healthy["reason"].is_null());
        assert!(healthy["error"].is_null());
        for failure in [
            Failure::Unavailable,
            Failure::Http(500),
            Failure::InvalidResponse,
            Failure::ReadFailed,
        ] {
            let service = observation(Err(failure), "loopback-http", "fixture");
            assert!(service["online"].is_null());
            assert_eq!(service["status"], "unknown");
            assert!(service["checkedAt"].as_i64().is_some());
        }
    }

    #[test]
    fn docker_head_requires_a_complete_successful_http_response() {
        assert_eq!(
            docker_headers(b"HTTP/1.1 200 OK\r\nApi-Version: 1.51\r\n\r\n").unwrap(),
            json!({"online":true,"apiVersion":"1.51"})
        );
        assert_eq!(
            docker_headers(b"HTTP/1.1 500 Error\r\n\r\n"),
            Err(Failure::Http(500))
        );
        assert_eq!(
            docker_headers(b"HTTP/1.1 200 OK\r\n"),
            Err(Failure::InvalidResponse)
        );
        assert_eq!(
            docker_headers(b"not HTTP\r\n\r\n"),
            Err(Failure::InvalidResponse)
        );
    }

    #[tokio::test]
    async fn backend_errors_invalid_json_and_redirects_are_partial_observations() {
        for (status, body, reason) in [
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                "upstream detail must remain private",
                "http-status",
            ),
            (StatusCode::OK, "not JSON", "invalid-response"),
            (StatusCode::OK, "[]", "invalid-response"),
            (
                StatusCode::OK,
                "{\"error\":\"upstream detail must remain private\"}",
                "invalid-response",
            ),
            (StatusCode::FOUND, "redirect", "http-status"),
        ] {
            let server = fixture(Router::new().route(
                "/api/health",
                get(move || async move {
                    (
                        status,
                        [("location", "http://external.invalid/health")],
                        body,
                    )
                        .into_response()
                }),
            ))
            .await;
            let value = HealthSnapshot::for_test(&server.base)
                .snapshot(&client(), &server.base, 10012)
                .await;
            assert_eq!(value["status"], "ok");
            assert_eq!(value["console"]["online"], true);
            assert_eq!(value["console"]["pid"], std::process::id());
            assert_eq!(value["port"], 10012);
            assert_eq!(value["partial"], true);
            let _ = reason;
            assert!(value.get("legacyBackend").is_none());
            assert!(value["legacyBackend"]["online"].is_null());
            assert!(!value.to_string().contains("upstream detail"));
            for key in SERVICE_KEYS {
                assert!(value[key]["online"].is_null(), "{key}");
            }
        }
    }

    #[tokio::test]
    async fn health_queries_only_current_services() {
        use std::sync::{
            Arc,
            atomic::{AtomicUsize, Ordering},
        };
        let calls = Arc::new(AtomicUsize::new(0));
        let trap = calls.clone();
        let router = Router::new()
            .route(
                "/_godd_ao/healthz",
                get(|| async { Json(json!({"service":"godd-ao-semantic-proxy","status":"ok"})) }),
            )
            .route(
                "/healthz",
                get(|| async { Json(json!({"service":"opencodex","status":"ok"})) }),
            )
            .fallback(get(move || {
                let trap = trap.clone();
                async move {
                    trap.fetch_add(1, Ordering::SeqCst);
                    Json(json!({"status":"ok"}))
                }
            }));
        let server = fixture(router).await;
        let value = HealthSnapshot::for_test(&server.base)
            .snapshot(&client(), &server.base, 10012)
            .await;
        assert_eq!(value["godd"]["online"], true);
        assert_eq!(value["opencodex"]["online"], true);
        assert_eq!(calls.load(Ordering::SeqCst), 0);
        for retired in ["ollama", "deepseek", "legacyBackend"] {
            assert!(value.get(retired).is_none());
        }
    }
    #[tokio::test]
    async fn oversized_health_response_is_not_used_as_service_evidence() {
        let server =
            fixture(Router::new().route("/health", get(|| async { "x".repeat(BODY_LIMIT + 1) })))
                .await;
        let result = read_json(&client(), &format!("{}/health", server.base), PROBE_TIMEOUT).await;
        assert_eq!(result, Err(Failure::BodyTooLarge));
    }

    #[tokio::test]
    async fn deadline_includes_waiting_for_headers_and_the_response_body() {
        let server = fixture(Router::new().route(
            "/health",
            get(|| async { std::future::pending::<Json<Value>>().await }),
        ))
        .await;
        assert_eq!(
            tokio::time::timeout(
                Duration::from_secs(3),
                read_json(
                    &client(),
                    &format!("{}/health", server.base),
                    Duration::from_millis(50)
                )
            )
            .await
            .unwrap(),
            Err(Failure::Timeout),
        );
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let base = format!("http://{}", listener.local_addr().unwrap());
        let task = tokio::spawn(async move {
            let (mut stream, _) = listener.accept().await.unwrap();
            stream
                .write_all(b"HTTP/1.1 200 OK\r\nContent-Length: 20\r\n\r\n{")
                .await
                .unwrap();
            std::future::pending::<()>().await;
        });
        let body_server = Fixture { base, task };
        assert_eq!(
            tokio::time::timeout(
                Duration::from_secs(3),
                read_json(
                    &client(),
                    &format!("{}/health", body_server.base),
                    Duration::from_millis(50)
                )
            )
            .await
            .unwrap(),
            Err(Failure::Timeout),
        );
    }
}
