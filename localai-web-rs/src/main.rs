use axum::{
    body::{to_bytes, Body},
    extract::{Path, Query, Request, State},
    http::{header, HeaderMap, Method, StatusCode},
    middleware::{self, Next},
    response::{Html, IntoResponse, Json, Response},
    routing::{get, post},
    Router,
};
use serde_json::{json, Value};
use std::{process::Command, time::Duration};
use tower_http::services::ServeDir;
mod agent;
mod health_snapshot;
mod music_bridge;
use localai_web_rs::operations;
mod command_capture;
mod resident;
mod workload;

const JSON_LIMIT: usize = 2 * 1024 * 1024;
// Existing PowerShell upload validation accepts a 50 MiB image as base64 JSON.
const UPLOAD_LIMIT: usize = 70_100_000;
const RESPONSE_LIMIT: usize = 70_100_000;
const PROXY_GET_PATHS: &[&str] = &[
    "/api/models",
    "/api/minimax-status",
    "/api/vtuber-characters",
    "/api/vtuber-image",
    "/api/history",
    "/api/minimax-last-queue",
    "/api/open-folder",
    "/api/open-file",
];
const PROXY_POST_PATHS: &[&str] = &[
    "/api/upload-image",
    "/api/video-prompt",
    "/api/video-scenes",
    "/api/image-generate",
    "/api/minimax-retry",
    "/api/control",
];

#[derive(Clone)]
struct AppState {
    client: reqwest::Client,
    port: u16,
    backend: String,
    agent: std::sync::Arc<agent::Console>,
    workload: std::sync::Arc<workload::Workload>,
    operations: std::sync::Arc<operations::Operations>,
    health: std::sync::Arc<health_snapshot::HealthSnapshot>,
}

impl AppState {
    fn new(port: u16, backend_port: u16) -> Self {
        Self {
            client: reqwest::Client::builder()
                .no_proxy()
                .redirect(reqwest::redirect::Policy::none())
                .connect_timeout(Duration::from_secs(2))
                .timeout(Duration::from_secs(30))
                .build()
                .expect("create loopback HTTP client"),
            port,
            backend: format!("http://127.0.0.1:{backend_port}"),
            agent: std::sync::Arc::new(agent::Console::production()),
            workload: std::sync::Arc::new(workload::Workload::production()),
            operations: std::sync::Arc::new(operations::Operations::production()),
            health: std::sync::Arc::new(health_snapshot::HealthSnapshot::production()),
        }
    }
}

#[tokio::main]
async fn main() {
    let args: Vec<_> = std::env::args_os().collect();
    if args.get(1).is_some_and(|arg| arg == "--compact-state") {
        let Some(dir) = args.get(2).filter(|_| args.len() == 3) else {
            eprintln!("usage: localai-web-rs --compact-state <state-directory>");
            std::process::exit(2);
        };
        match agent::compact_state_file(std::path::Path::new(dir)) {
            Ok(result) => println!("{result}"),
            Err(message) => {
                eprintln!("{message}");
                std::process::exit(1);
            }
        }
        return;
    }
    if let Err(error) = resident::guard() {
        eprintln!("{error}");
        std::process::exit(78);
    }
    let port = std::env::var("LOCALAI_WEB_PORT")
        .map(|value| {
            value
                .parse::<u16>()
                .expect("LOCALAI_WEB_PORT must be a port number")
        })
        .unwrap_or(10012);
    // Bind before the initial shared-record scan to prevent duplicate scheduled starts.
    let listener = tokio::net::TcpListener::bind(("127.0.0.1", port))
        .await
        .unwrap();
    let state = AppState::new(port, 10013);
    let console = state.agent.clone();
    let workload = state.workload.clone();
    let operations = state.operations.clone();
    // Preview performs a single read-only projection; residents refresh independently
    // of browser sessions. Neither path changes shared operational records.
    if std::env::var("LOCALAI_WEB_PREVIEW").as_deref() == Ok("1") {
        let initial_operations = operations.clone();
        let _ = tokio::task::spawn_blocking(move || initial_operations.refresh()).await;
    }
    let app = app(state);

    println!("Rust Web Server listening on http://127.0.0.1:{port}");
    if std::env::var("LOCALAI_WEB_PREVIEW").as_deref() != Ok("1") {
        tokio::spawn(agent::resident(console));
        tokio::spawn(workload::resident(workload, operations.clone()));
        tokio::spawn(operations::resident(operations));
    }
    axum::serve(listener, app).await.unwrap();
}

fn app(state: AppState) -> Router {
    let mut app = Router::new()
        .route("/", get(index_html))
        .route("/progress", get(progress_page))
        .route("/music", axum::routing::any(music_proxy))
        .route("/music/*path", axum::routing::any(music_proxy))
        .route("/api/health", get(health_check))
        .route("/api/resident/restart", post(restart_resident))
        .route("/api/omb-board", get(omb_board))
        .route("/api/operations", get(operations_snapshot))
        .route("/api/resources", get(resources_snapshot))
        .route("/api/operations/workers/:id", get(operations_worker_detail))
        .route("/api/tasks", get(get_tasks))
        .route("/api/agent/workload", get(workload::handle))
        .route("/api/agent/native", get(native_activity_snapshot))
        .route("/api/agent/native/register", post(native_activity_register))
        .route("/api/agent/native/activity", post(native_activity_record))
        .route("/api/agent/native/close", post(native_activity_close))
        .route("/api/agent/native/connect", post(native_activity_connect))
        .route("/api/agent/display/:kind", get(shared_progress_display))
        .route("/api/agent/*path", axum::routing::any(agent::handle))
        .nest_service(
            "/api/attachments",
            ServeDir::new("D:\\CC\\OpenMausBot\\.openmausbot\\attachments"),
        );
    for path in PROXY_GET_PATHS {
        app = app.route(path, get(proxy_request));
    }
    for path in PROXY_POST_PATHS {
        app = app.route(path, post(proxy_request));
    }
    app.layer(middleware::from_fn_with_state(
        state.clone(),
        local_request_only,
    ))
    .with_state(state)
}

async fn index_html() -> impl IntoResponse {
    (
        [
            (header::CACHE_CONTROL, "no-store"),
            (header::CONTENT_SECURITY_POLICY, "frame-ancestors 'none'"),
            (header::X_FRAME_OPTIONS, "DENY"),
            (header::X_CONTENT_TYPE_OPTIONS, "nosniff"),
        ],
        Html(localai_web_rs::console_page::index()),
    )
}

async fn progress_page() -> impl IntoResponse {
    index_html().await
}

async fn shared_progress_display(
    State(state): State<AppState>,
    Path(kind): Path<String>,
    request: Request,
) -> Response {
    if !matches!(kind.as_str(), "todo" | "jobs" | "handoff") {
        return agent::handle(State(state), request).await;
    }
    let snapshot = state.operations.snapshot();
    if !snapshot["fetchedAt"].is_u64() {
        return agent::cached_display_response(Err("作業状況を取得中です。まもなく再試行します。"));
    }
    if snapshot["protectionMode"] == "masked_metadata" {
        return agent::cached_display_response(
            operations::metadata_display_snapshot(&snapshot, &kind)
                .map_err(|_| "進捗の元記録を確認できません。前回の表示を保持します。"),
        );
    }
    agent::handle(State(state), request).await
}

async fn operations_snapshot(State(state): State<AppState>) -> Json<Value> {
    Json(state.operations.snapshot())
}
async fn resources_snapshot(State(state): State<AppState>) -> impl IntoResponse {
    (
        [(header::CACHE_CONTROL, "no-store")],
        Json(state.operations.resource_snapshot()),
    )
}

async fn native_activity_snapshot(State(state): State<AppState>) -> impl IntoResponse {
    (
        [(header::CACHE_CONTROL, "no-store")],
        Json(state.operations.native_snapshot()),
    )
}
fn native_activity_response(result: Result<Value, &'static str>) -> Response {
    match result {
        Ok(value) => ([(header::CACHE_CONTROL, "no-store")], Json(value)).into_response(),
        Err(code) => {
            let status = if code == "native_store_unavailable" || code == "native_store_full" {
                StatusCode::SERVICE_UNAVAILABLE
            } else if code == "native_binding_changed" || code == "native_event_conflict" {
                StatusCode::CONFLICT
            } else {
                StatusCode::BAD_REQUEST
            };
            (
                status,
                [(header::CACHE_CONTROL, "no-store")],
                Json(json!({"ok":false,"source":"native","error":code})),
            )
                .into_response()
        }
    }
}
async fn native_activity_register(
    State(state): State<AppState>,
    Json(request): Json<localai_web_rs::native_activity::Register>,
) -> Response {
    let operations = state.operations.clone();
    native_activity_response(
        tokio::task::spawn_blocking(move || operations.register_native(request))
            .await
            .unwrap_or(Err("native_store_unavailable")),
    )
}
async fn native_activity_record(
    State(state): State<AppState>,
    Json(request): Json<localai_web_rs::native_activity::Activity>,
) -> Response {
    let operations = state.operations.clone();
    native_activity_response(
        tokio::task::spawn_blocking(move || operations.record_native(request))
            .await
            .unwrap_or(Err("native_store_unavailable")),
    )
}
async fn native_activity_close(
    State(state): State<AppState>,
    Json(request): Json<localai_web_rs::native_activity::Close>,
) -> Response {
    let operations = state.operations.clone();
    native_activity_response(
        tokio::task::spawn_blocking(move || operations.close_native(request))
            .await
            .unwrap_or(Err("native_store_unavailable")),
    )
}

async fn native_activity_connect(
    State(state): State<AppState>,
    Json(request): Json<localai_web_rs::native_activity::Connect>,
) -> Response {
    let operations = state.operations.clone();
    native_activity_response(
        tokio::task::spawn_blocking(move || operations.connect_native(request))
            .await
            .unwrap_or(Err("native_runtime_source_unavailable")),
    )
}

#[derive(serde::Deserialize)]
struct HistoryPage {
    #[serde(default)]
    offset: usize,
}
async fn operations_worker_detail(
    State(state): State<AppState>,
    Path(id): Path<String>,
    Query(page): Query<HistoryPage>,
) -> Response {
    let board = state.operations.clone();
    match tokio::task::spawn_blocking(move || board.worker_detail(&id, page.offset)).await {
        Ok(Ok(value)) => Json(value).into_response(),
        Ok(Err(code)) => json_error(
            StatusCode::from_u16(code).unwrap(),
            "作業の詳細を取得できません",
        ),
        Err(_) => json_error(
            StatusCode::SERVICE_UNAVAILABLE,
            "作業の詳細を取得できません",
        ),
    }
}

async fn restart_resident() -> Response {
    if std::env::var("LOCALAI_WEB_PREVIEW").as_deref() == Ok("1") {
        return json_error(
            StatusCode::CONFLICT,
            "確認用サーバーは常駐処理を再起動しません",
        );
    }
    match tokio::task::spawn_blocking(resident::ensure_scheduled_launcher).await {
        Ok(Ok(())) => {
            // The running scheduled watcher checks health every ten seconds. Exiting
            // leaves only that clean launcher to create the replacement, including when
            // schtasks /Run ignored an already-running task.
            tokio::spawn(async {
                tokio::time::sleep(Duration::from_millis(500)).await;
                std::process::exit(0);
            });
            Json(json!({"ok":true,"launcher":"scheduled-task"})).into_response()
        }
        _ => json_error(
            StatusCode::SERVICE_UNAVAILABLE,
            "定期タスクを起動できなかったため、コンソールの再起動を再試行します",
        ),
    }
}

async fn music_proxy(State(state): State<AppState>, request: Request) -> Response {
    music_bridge::handle(state.client, request).await
}

fn json_error(status: StatusCode, message: &str) -> Response {
    (status, Json(json!({"error": message}))).into_response()
}

fn local_headers(headers: &HeaderMap, port: u16, shell_navigation: bool) -> bool {
    // Reject ambiguous authorities/origins before considering the static shell.
    if headers.get_all(header::HOST).iter().count() != 1
        || headers.get_all(header::ORIGIN).iter().count() > 1
        || headers.get_all("sec-fetch-site").iter().count() > 1
    {
        return false;
    }
    let Some(host) = headers
        .get(header::HOST)
        .and_then(|value| value.to_str().ok())
    else {
        return false;
    };
    if host != format!("127.0.0.1:{port}") && host != format!("localhost:{port}") {
        return false;
    }
    if let Some(origin) = headers.get(header::ORIGIN) {
        if origin.to_str().ok() != Some(format!("http://{host}").as_str()) {
            return false;
        }
    }
    if let Some(site) = headers.get("sec-fetch-site") {
        if !(matches!(site.to_str().ok(), Some("same-origin" | "none"))
            || (shell_navigation && matches!(site.to_str().ok(), Some("same-site" | "cross-site"))))
        {
            return false;
        }
    }
    true
}

fn static_shell_navigation(request: &Request) -> bool {
    // Only the compiled, data-free shell can be entered from another site.
    // APIs, frames, fetch/XHR and WebSocket handshakes retain the strict policy.
    let headers = request.headers();
    request.method() == Method::GET
        && matches!(request.uri().path(), "/" | "/progress")
        && !headers.contains_key(header::ORIGIN)
        && !headers.contains_key(header::UPGRADE)
        && [
            ("sec-fetch-mode", "navigate"),
            ("sec-fetch-dest", "document"),
        ]
        .into_iter()
        .all(|(name, expected)| {
            headers.get_all(name).iter().count() == 1
                && headers.get(name).and_then(|value| value.to_str().ok()) == Some(expected)
        })
}

async fn local_request_only(
    State(state): State<AppState>,
    request: Request,
    next: Next,
) -> Response {
    if !local_headers(
        request.headers(),
        state.port,
        static_shell_navigation(&request),
    ) {
        if request.uri().path().starts_with("/api/agent/") {
            return agent::local_forbidden();
        }
        return json_error(
            StatusCode::FORBIDDEN,
            "Local AI Console requires a same-origin loopback request.",
        );
    }
    next.run(request).await
}

async fn health_check(State(state): State<AppState>) -> Response {
    let health = state
        .health
        .snapshot(&state.client, &state.backend, state.port)
        .await;
    ([(header::CACHE_CONTROL, "no-store")], Json(health)).into_response()
}

async fn proxy_request(State(state): State<AppState>, request: Request) -> Response {
    let (parts, body) = request.into_parts();
    let path = parts.uri.path();
    let mut content_type = None;
    let mut bytes = None;
    if parts.method == Method::POST {
        let is_json = parts
            .headers
            .get(header::CONTENT_TYPE)
            .and_then(|value| value.to_str().ok())
            .is_some_and(|value| {
                value
                    .split(';')
                    .next()
                    .unwrap_or("")
                    .trim()
                    .eq_ignore_ascii_case("application/json")
            });
        if !is_json {
            return json_error(
                StatusCode::UNSUPPORTED_MEDIA_TYPE,
                "Content-Type must be application/json.",
            );
        }
        let limit = if path == "/api/upload-image" {
            UPLOAD_LIMIT
        } else {
            JSON_LIMIT
        };
        let payload = match to_bytes(body, limit).await {
            Ok(payload) => payload,
            Err(_) => {
                return json_error(
                    StatusCode::PAYLOAD_TOO_LARGE,
                    "Request body exceeds the allowed size.",
                );
            }
        };
        if path == "/api/control" {
            let value = match serde_json::from_slice::<Value>(&payload) {
                Ok(value) => value,
                Err(_) => {
                    return json_error(
                        StatusCode::BAD_REQUEST,
                        "Control request must be valid JSON.",
                    );
                }
            };
            if !matches!(
                value["action"].as_str(),
                Some("start-minimax" | "start-minimax-scenes" | "stop-local")
            ) {
                return json_error(
                    StatusCode::FORBIDDEN,
                    "This control action is not available through the MiniMax proxy.",
                );
            }
        }
        bytes = Some(payload);
        content_type = Some("application/json");
    }
    let target = parts
        .uri
        .path_and_query()
        .map(|value| value.as_str())
        .unwrap_or(path);
    forward(&state, parts.method, target, bytes, content_type).await
}

async fn forward(
    state: &AppState,
    method: Method,
    target: &str,
    body: Option<axum::body::Bytes>,
    content_type: Option<&str>,
) -> Response {
    let mut request = state
        .client
        .request(method, format!("{}{target}", state.backend));
    if let Some(content_type) = content_type {
        request = request.header(header::CONTENT_TYPE, content_type);
    }
    if let Some(body) = body {
        request = request.body(body);
    }
    if matches!(
        target.split('?').next(),
        Some("/api/video-prompt" | "/api/video-scenes" | "/api/image-generate")
    ) {
        request = request.timeout(Duration::from_secs(1800));
    }
    let mut upstream = match request.send().await {
        Ok(response) => response,
        Err(_) => {
            return json_error(
                StatusCode::SERVICE_UNAVAILABLE,
                "MiniMax backend is unavailable at 127.0.0.1:10013. Start LocalAI-Web.ps1 -Port 10013 -NoBrowser.",
            );
        }
    };
    let status = upstream.status();
    if status.is_redirection() {
        return json_error(
            StatusCode::BAD_GATEWAY,
            "Unexpected redirect from the local MiniMax backend.",
        );
    }
    let headers = upstream.headers().clone();
    let mut output = Vec::new();
    loop {
        match upstream.chunk().await {
            Ok(Some(chunk)) if output.len() + chunk.len() <= RESPONSE_LIMIT => {
                output.extend_from_slice(&chunk)
            }
            Ok(Some(_)) => {
                return json_error(
                    StatusCode::BAD_GATEWAY,
                    "MiniMax backend response exceeds the allowed size.",
                );
            }
            Ok(None) => break,
            Err(_) => {
                return json_error(
                    StatusCode::SERVICE_UNAVAILABLE,
                    "MiniMax backend connection was interrupted.",
                );
            }
        }
    }
    let mut response = (status, Body::from(output)).into_response();
    for name in [header::CONTENT_TYPE, header::CACHE_CONTROL] {
        if let Some(value) = headers.get(&name) {
            response.headers_mut().insert(name, value.clone());
        }
    }
    response
}

async fn omb_board() -> Json<Value> {
    let output = agent::windowless(&mut Command::new(
        "D:\\CC\\Tools\\omb-kanban\\target\\release\\omb-kanban.exe",
    ))
    .arg("board")
    .output();

    match output {
        Ok(out) => {
            let res: Value =
                serde_json::from_slice(&out.stdout).unwrap_or(json!({"error": "parse error"}));
            Json(res)
        }
        Err(_) => Json(json!({"error": "execution failed"})),
    }
}

async fn get_tasks() -> Json<Value> {
    let output = agent::windowless(&mut Command::new("docker"))
        .args([
            "exec",
            "omb-task-db",
            "psql",
            "-U",
            "taskadmin",
            "-d",
            "shared_tasks",
            "-t",
            "-A",
            "-c",
            "SELECT json_agg(t) FROM tasks t;",
        ])
        .output();

    match output {
        Ok(out) => {
            let res: Value = serde_json::from_slice(&out.stdout).unwrap_or(json!([]));
            Json(res)
        }
        Err(_) => Json(json!([])),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tower::ServiceExt;

    fn request(method: Method, path: &str, body: impl Into<Body>) -> Request {
        Request::builder()
            .method(method)
            .uri(path)
            .header(header::HOST, "127.0.0.1:10012")
            .header(header::CONTENT_TYPE, "application/json")
            .body(body.into())
            .unwrap()
    }

    async fn test_backend(router: Router) -> (AppState, tokio::task::JoinHandle<()>) {
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let mut state = AppState::new(10012, listener.local_addr().unwrap().port());
        state.health =
            std::sync::Arc::new(health_snapshot::HealthSnapshot::for_test(&state.backend));
        let task = tokio::spawn(async move {
            axum::serve(listener, router).await.unwrap();
        });
        (state, task)
    }

    #[tokio::test]
    async fn native_tool_receipts_are_separate_and_closed_sources_leave_live() {
        let root = tempfile::tempdir().unwrap();
        let mut state = AppState::new(10012, 10013);
        state.operations = std::sync::Arc::new(operations::Operations::new(
            root.path().into(),
            root.path().join("skills"),
        ));
        let router = app(state);
        let id = "00000000-0000-7000-8000-000000000001";
        let send = |path: &str, value: Value| {
            let mut req = request(Method::POST, path, Body::from(value.to_string()));
            req.headers_mut()
                .insert(header::CONTENT_TYPE, "application/json".parse().unwrap());
            router.clone().oneshot(req)
        };
        let bound = send(
            "/api/agent/native/register",
            json!({"session_id":id,"todo_id":1,"role":"work"}),
        )
        .await
        .unwrap();
        assert_eq!(
            bound.status(),
            StatusCode::OK,
            "registration is metadata, not a fabricated CC job"
        );
        let data: Value =
            serde_json::from_slice(&to_bytes(bound.into_body(), JSON_LIMIT).await.unwrap())
                .unwrap();
        let generation = data["generation"].as_u64().unwrap();
        assert!(data["executionKey"]
            .as_str()
            .unwrap()
            .starts_with("native-"));
        let bad = send(
            "/api/agent/native/activity",
            json!({"session_id":id,"generation":generation,"status":"working"}),
        )
        .await
        .unwrap();
        assert_eq!(bad.status(), StatusCode::UNPROCESSABLE_ENTITY);
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_millis() as u64;
        let observed = send("/api/agent/native/activity", json!({"session_id":id,"generation":generation,"actual_at_ms":now,"event_id":"runtime-result-1","tool":"exec_command","kind":"test","executed":true,"exit_code":0})).await.unwrap();
        assert_eq!(observed.status(), StatusCode::OK);
        let live = router
            .clone()
            .oneshot(request(Method::GET, "/api/agent/native", Body::empty()))
            .await
            .unwrap();
        let live: Value =
            serde_json::from_slice(&to_bytes(live.into_body(), JSON_LIMIT).await.unwrap()).unwrap();
        assert_eq!(live["sessions"].as_array().unwrap().len(), 1);
        assert_eq!(live["sessions"][0]["source"], "native");
        let ended = send(
            "/api/agent/native/close",
            json!({"session_id":id,"generation":generation,"ended_at_ms":now,"outcome":"success"}),
        )
        .await
        .unwrap();
        assert_eq!(ended.status(), StatusCode::OK);
        let view = router
            .oneshot(request(Method::GET, "/api/agent/native", Body::empty()))
            .await
            .unwrap();
        assert_eq!(view.status(), StatusCode::OK);
        let data: Value =
            serde_json::from_slice(&to_bytes(view.into_body(), JSON_LIMIT).await.unwrap()).unwrap();
        assert_eq!(data["sessions"], json!([]));
        assert_eq!(data["bindings"][0]["outcome"], "success");
        assert!(!data.to_string().contains(id));
    }

    #[tokio::test]
    async fn rejects_external_browser_requests_and_rebinding_hosts() {
        let app = app(AppState::new(10012, 10013));
        for (name, value) in [
            ("host", "attacker.example:10012"),
            ("origin", "https://attacker.example"),
            ("origin", "http://127.0.0.1:10013"),
            ("origin", "null"),
            ("sec-fetch-site", "cross-site"),
            ("sec-fetch-site", "same-site"),
        ] {
            let mut req = request(Method::GET, "/api/open-folder", Body::empty());
            req.headers_mut().insert(name, value.parse().unwrap());
            assert_eq!(
                app.clone().oneshot(req).await.unwrap().status(),
                StatusCode::FORBIDDEN
            );
        }
        let req = Request::builder().uri("/").body(Body::empty()).unwrap();
        assert_eq!(
            app.oneshot(req).await.unwrap().status(),
            StatusCode::FORBIDDEN
        );
    }

    #[tokio::test]
    async fn accepts_same_origin_and_keeps_unknown_apis_private() {
        let app = app(AppState::new(10012, 10013));
        let mut req = request(Method::GET, "/", Body::empty());
        req.headers_mut()
            .insert(header::ORIGIN, "http://127.0.0.1:10012".parse().unwrap());
        req.headers_mut()
            .insert("sec-fetch-site", "same-origin".parse().unwrap());
        let res = app.clone().oneshot(req).await.unwrap();
        assert_eq!(res.status(), StatusCode::OK);
        assert!(res
            .headers()
            .get(header::ACCESS_CONTROL_ALLOW_ORIGIN)
            .is_none());
        for path in ["/api/private", "/api/shared-todo", "/api/chat"] {
            assert_eq!(
                app.clone()
                    .oneshot(request(Method::GET, path, Body::empty()))
                    .await
                    .unwrap()
                    .status(),
                StatusCode::NOT_FOUND
            );
        }
    }

    fn navigation(method: Method, path: &str, site: &str) -> Request {
        let mut req = request(method, path, Body::empty());
        for (name, value) in [
            ("sec-fetch-mode", "navigate"),
            ("sec-fetch-dest", "document"),
            ("sec-fetch-site", site),
        ] {
            req.headers_mut().insert(name, value.parse().unwrap());
        }
        req
    }

    #[tokio::test]
    async fn shared_progress_waits_for_snapshot_without_scanning_the_live_store() {
        let mut request = navigation(Method::GET, "/api/agent/display/jobs", "same-origin");
        request
            .headers_mut()
            .insert(header::HOST, "localhost:10012".parse().unwrap());
        let response = app(AppState::new(10012, 10013))
            .oneshot(request)
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::SERVICE_UNAVAILABLE);
        let body = to_bytes(response.into_body(), RESPONSE_LIMIT)
            .await
            .unwrap();
        assert!(std::str::from_utf8(&body)
            .unwrap()
            .contains("作業状況を取得中"));
        let envelope: Value = serde_json::from_slice(&body).unwrap();
        assert_eq!(envelope["version"], 1);
        assert_eq!(envelope["ok"], false);
        assert_eq!(envelope["error"]["code"], "progress_snapshot_unavailable");
    }

    #[tokio::test]
    async fn resources_endpoint_reads_cache_and_keeps_the_existing_loopback_boundary() {
        let app = app(AppState::new(10012, 10013));
        for _ in 0..2 {
            let response = app
                .clone()
                .oneshot(request(Method::GET, "/api/resources", Body::empty()))
                .await
                .unwrap();
            assert_eq!(response.status(), StatusCode::OK);
            assert_eq!(response.headers()[header::CACHE_CONTROL], "no-store");
            let value: Value = serde_json::from_slice(
                &to_bytes(response.into_body(), RESPONSE_LIMIT)
                    .await
                    .unwrap(),
            )
            .unwrap();
            assert_eq!(value["schemaVersion"], 1);
            assert_eq!(value["state"], "collecting");
            assert!(value["sampledAt"].is_null());
            assert_eq!(value["processes"], json!([]));
        }
        let mut external = request(Method::GET, "/api/resources", Body::empty());
        external
            .headers_mut()
            .insert(header::HOST, "outside.example".parse().unwrap());
        assert_eq!(
            app.oneshot(external).await.unwrap().status(),
            StatusCode::FORBIDDEN
        );
    }

    #[tokio::test]
    async fn progress_navigation_serves_the_dedicated_processing_page() {
        let mut request = navigation(Method::GET, "/progress", "none");
        request
            .headers_mut()
            .insert(header::HOST, "localhost:10012".parse().unwrap());
        let response = app(AppState::new(10012, 10013))
            .oneshot(request)
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        assert!(response.headers().get(header::LOCATION).is_none());
        assert_eq!(response.headers()[header::CACHE_CONTROL], "no-store");
    }

    #[tokio::test]
    async fn top_level_navigation_serves_only_the_compiled_shell() {
        let app = app(AppState::new(10012, 10013));
        for host in ["localhost:10012", "127.0.0.1:10012"] {
            for site in ["none", "same-origin", "same-site", "cross-site"] {
                let mut req = navigation(Method::GET, "/", site);
                req.headers_mut()
                    .insert(header::HOST, host.parse().unwrap());
                let res = app.clone().oneshot(req).await.unwrap();
                assert_eq!(res.status(), StatusCode::OK, "{host} {site}");
                assert_eq!(res.headers()[header::CACHE_CONTROL], "no-store");
                assert_eq!(res.headers()[header::X_FRAME_OPTIONS], "DENY");
                assert_eq!(
                    res.headers()[header::CONTENT_SECURITY_POLICY],
                    "frame-ancestors 'none'"
                );
                assert!(res
                    .headers()
                    .get(header::ACCESS_CONTROL_ALLOW_ORIGIN)
                    .is_none());
                let body = to_bytes(res.into_body(), RESPONSE_LIMIT).await.unwrap();
                // No operational records are read or interpolated into this document.
                assert_eq!(
                    body.as_ref(),
                    localai_web_rs::console_page::index().as_bytes()
                );
            }
        }
    }

    #[tokio::test]
    async fn shell_exception_never_authorizes_apis_frames_xhr_or_websockets() {
        let app = app(AppState::new(10012, 10013));
        for method in [Method::GET, Method::POST, Method::HEAD, Method::OPTIONS] {
            for path in [
                "/api/operations",
                "/api/agent/todo/list",
                "/api/resident/restart",
                "/music",
                "/api/attachments/test",
            ] {
                let res = app
                    .clone()
                    .oneshot(navigation(method.clone(), path, "cross-site"))
                    .await
                    .unwrap();
                assert_eq!(res.status(), StatusCode::FORBIDDEN, "{method} {path}");
                assert!(res
                    .headers()
                    .get(header::ACCESS_CONTROL_ALLOW_ORIGIN)
                    .is_none());
            }
        }
        for (name, value) in [
            ("host", "attacker.example:10012"),
            ("host", "localhost.attacker.example:10012"),
            ("host", "localhost:10013"),
            ("origin", "https://attacker.example"),
            ("origin", "http://127.0.0.1:10012"),
            ("origin", "null"),
            ("sec-fetch-dest", "iframe"),
            ("sec-fetch-dest", "frame"),
            ("sec-fetch-dest", "empty"),
            ("sec-fetch-mode", "cors"),
            ("sec-fetch-mode", "no-cors"),
            ("sec-fetch-mode", "websocket"),
            ("sec-fetch-site", "invalid"),
            ("upgrade", "websocket"),
        ] {
            let mut req = navigation(Method::GET, "/", "cross-site");
            req.headers_mut()
                .insert(header::HOST, "localhost:10012".parse().unwrap());
            req.headers_mut().insert(name, value.parse().unwrap());
            assert_eq!(
                app.clone().oneshot(req).await.unwrap().status(),
                StatusCode::FORBIDDEN,
                "{name} {value}"
            );
        }
        for missing in ["host", "sec-fetch-mode", "sec-fetch-dest"] {
            let mut req = navigation(Method::GET, "/", "cross-site");
            req.headers_mut().remove(missing);
            assert_eq!(
                app.clone().oneshot(req).await.unwrap().status(),
                StatusCode::FORBIDDEN,
                "missing {missing}"
            );
        }
        for duplicate in [
            "host",
            "origin",
            "sec-fetch-mode",
            "sec-fetch-dest",
            "sec-fetch-site",
        ] {
            let mut req = navigation(Method::GET, "/", "cross-site");
            req.headers_mut()
                .append(duplicate, "invalid".parse().unwrap());
            assert_eq!(
                app.clone().oneshot(req).await.unwrap().status(),
                StatusCode::FORBIDDEN,
                "duplicate {duplicate}"
            );
        }
        for method in [Method::POST, Method::HEAD, Method::OPTIONS] {
            assert_eq!(
                app.clone()
                    .oneshot(navigation(method, "/", "cross-site"))
                    .await
                    .unwrap()
                    .status(),
                StatusCode::FORBIDDEN
            );
        }
    }

    #[tokio::test]
    async fn api_origin_host_and_cli_contracts_remain_strict() {
        let app = app(AppState::new(10012, 10013));
        for method in [Method::GET, Method::POST] {
            for (name, value) in [
                ("host", "attacker.example:10012"),
                ("origin", "https://attacker.example"),
                ("origin", "http://localhost:10012"),
                ("origin", "null"),
                ("sec-fetch-site", "cross-site"),
                ("sec-fetch-site", "same-site"),
            ] {
                let mut req = request(method.clone(), "/api/operations", Body::empty());
                req.headers_mut().insert(name, value.parse().unwrap());
                assert_eq!(
                    app.clone().oneshot(req).await.unwrap().status(),
                    StatusCode::FORBIDDEN,
                    "{method} {name} {value}"
                );
            }
        }
        // CLI requests have no Fetch Metadata or Origin; same-origin browser GETs do.
        for browser in [false, true] {
            let mut req = request(Method::GET, "/api/operations", Body::empty());
            if browser {
                req.headers_mut()
                    .insert(header::ORIGIN, "http://127.0.0.1:10012".parse().unwrap());
                req.headers_mut()
                    .insert("sec-fetch-site", "same-origin".parse().unwrap());
            }
            let res = app.clone().oneshot(req).await.unwrap();
            assert_eq!(res.status(), StatusCode::OK);
            assert!(res
                .headers()
                .get(header::ACCESS_CONTROL_ALLOW_ORIGIN)
                .is_none());
            let body = to_bytes(res.into_body(), RESPONSE_LIMIT).await.unwrap();
            let value: Value = serde_json::from_slice(&body).unwrap();
            assert!(value.is_object());
        }
    }

    #[tokio::test]
    async fn validates_control_and_limits_request_body() {
        let app = app(AppState::new(10012, 10013));
        for (body, status) in [
            ("{\"action\":\"start-k3s\"}", StatusCode::FORBIDDEN),
            ("{}", StatusCode::FORBIDDEN),
            ("invalid", StatusCode::BAD_REQUEST),
        ] {
            assert_eq!(
                app.clone()
                    .oneshot(request(Method::POST, "/api/control", body))
                    .await
                    .unwrap()
                    .status(),
                status
            );
        }
        let mut req = request(Method::POST, "/api/control", "{}");
        req.headers_mut()
            .insert(header::CONTENT_TYPE, "text/plain".parse().unwrap());
        assert_eq!(
            app.clone().oneshot(req).await.unwrap().status(),
            StatusCode::UNSUPPORTED_MEDIA_TYPE
        );
        let req = request(
            Method::POST,
            "/api/video-scenes",
            vec![b'a'; JSON_LIMIT + 1],
        );
        assert_eq!(
            app.oneshot(req).await.unwrap().status(),
            StatusCode::PAYLOAD_TOO_LARGE
        );
    }

    #[tokio::test]
    async fn proxies_json_and_binary_without_forwarding_sensitive_headers() {
        async fn echo(request: Request) -> Response {
            assert_eq!(request.uri().query(), Some("sample=abc%2Fdef"));
            assert!(request.headers().get(header::AUTHORIZATION).is_none());
            assert!(request.headers().get(header::COOKIE).is_none());
            let payload = to_bytes(request.into_body(), JSON_LIMIT).await.unwrap();
            assert_eq!(payload, "{\"action\":\"start-minimax-scenes\"}");
            (StatusCode::ACCEPTED, Json(json!({"ok": true}))).into_response()
        }
        let backend = Router::new().route("/api/control", post(echo)).route(
            "/api/vtuber-image",
            get(|| async {
                (
                    [(header::CONTENT_TYPE, "image/png")],
                    vec![137u8, 80, 78, 71, 0, 255],
                )
            }),
        );
        let (state, task) = test_backend(backend).await;
        let app = app(state);
        let mut req = request(
            Method::POST,
            "/api/control?sample=abc%2Fdef",
            "{\"action\":\"start-minimax-scenes\"}",
        );
        req.headers_mut()
            .insert(header::AUTHORIZATION, "Bearer test".parse().unwrap());
        req.headers_mut()
            .insert(header::COOKIE, "sample=test".parse().unwrap());
        assert_eq!(
            app.clone().oneshot(req).await.unwrap().status(),
            StatusCode::ACCEPTED
        );
        let response = app
            .oneshot(request(Method::GET, "/api/vtuber-image", Body::empty()))
            .await
            .unwrap();
        assert_eq!(response.headers()[header::CONTENT_TYPE], "image/png");
        assert_eq!(
            to_bytes(response.into_body(), 32).await.unwrap().as_ref(),
            &[137, 80, 78, 71, 0, 255]
        );
        task.abort();
    }

    #[tokio::test]
    async fn health_merges_real_services_with_rust_identity() {
        let (state, task) = test_backend(
            Router::new()
                .route(
                    "/_godd_ao/healthz",
                    get(|| async {
                        Json(json!({"service":"godd-ao-semantic-proxy","status":"ok"}))
                    }),
                )
                .route(
                    "/healthz",
                    get(|| async { Json(json!({"service":"opencodex","status":"unhealthy"})) }),
                ),
        )
        .await;
        let response = app(state)
            .oneshot(request(Method::GET, "/api/health", Body::empty()))
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        let value: Value =
            serde_json::from_slice(&to_bytes(response.into_body(), JSON_LIMIT).await.unwrap())
                .unwrap();
        assert_eq!(value["port"], 10012);
        assert_eq!(value["backend"], "Rust/Axum");
        assert_eq!(value["status"], "ok");
        assert_eq!(value["opencodex"]["online"], false);
        assert_eq!(value["godd"]["online"], true);
        task.abort();
    }

    #[tokio::test]
    async fn health_keys_are_unique_for_case_insensitive_powershell_clients() {
        let (state, task) = test_backend(Router::new().route(
            "/api/health",
            get(|| async {
                Json(json!({"port":10013,"godd":{"online":true},"opencodex":{"online":false}}))
            }),
        ))
        .await;
        let response = app(state)
            .oneshot(request(Method::GET, "/api/health", Body::empty()))
            .await
            .unwrap();
        let value: Value =
            serde_json::from_slice(&to_bytes(response.into_body(), JSON_LIMIT).await.unwrap())
                .unwrap();
        let object = value.as_object().unwrap();
        let normalized: std::collections::HashSet<_> =
            object.keys().map(|key| key.to_ascii_lowercase()).collect();
        assert_eq!(normalized.len(), object.len());
        assert!(object.get("OpenCodex").is_none());
        assert!(object.get("Auto").is_none());
        assert!(object["opencodex"].is_object());
        task.abort();
    }

    #[tokio::test]
    async fn health_failure_returns_200_with_real_independent_services_and_null_unknowns() {
        let backend = Router::new()
            .route("/api/health", get(|| async {
                (StatusCode::INTERNAL_SERVER_ERROR, Json(json!({"error":"Path argument is null"})))
            }))
            .route("/_godd_ao/healthz", get(|| async {
                Json(json!({"service":"godd-ao-semantic-proxy","status":"ok","version":"fixture"}))
            }))
            .route("/healthz", get(|| async {
                Json(json!({"service":"opencodex","status":"ok"}))
            }))
            .route("/api/tags", get(|| async {
                Json(json!({"models":[]}))
            }));
        let (state, task) = test_backend(backend).await;
        let response = app(state)
            .oneshot(request(Method::GET, "/api/health", Body::empty()))
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        assert_eq!(response.headers()[header::CACHE_CONTROL], "no-store");
        let value: Value =
            serde_json::from_slice(&to_bytes(response.into_body(), JSON_LIMIT).await.unwrap())
                .unwrap();
        assert_eq!(value["console"]["online"], true);
        assert_eq!(value["port"], 10012);
        assert_eq!(value["backend"], "Rust/Axum");
        assert_eq!(value["partial"], true);
        assert!(value.get("legacyBackend").is_none());
        assert_eq!(value["godd"]["online"], true);
        assert_eq!(value["opencodex"]["online"], true);
        assert!(value.get("ollama").is_none());
        assert!(value.get("deepseek").is_none());
        assert!(value["docker"]["online"].is_null());
        assert!(value["error"].is_null());
        task.abort();
    }

    #[tokio::test]
    async fn upload_allows_base64_bodies_larger_than_the_normal_json_limit() {
        let backend = Router::new().route(
            "/api/upload-image",
            post(|request: Request| async {
                let body = to_bytes(request.into_body(), UPLOAD_LIMIT).await.unwrap();
                assert_eq!(body.len(), JSON_LIMIT + 1);
                Json(json!({"ok": true}))
            }),
        );
        let (state, task) = test_backend(backend).await;
        let req = request(
            Method::POST,
            "/api/upload-image",
            vec![b'a'; JSON_LIMIT + 1],
        );
        assert_eq!(
            app(state).oneshot(req).await.unwrap().status(),
            StatusCode::OK
        );
        task.abort();
    }

    #[tokio::test]
    async fn preserves_backend_failures_and_rejects_redirects() {
        let backend = Router::new()
            .route(
                "/api/minimax-status",
                get(|| async {
                    (
                        StatusCode::CONFLICT,
                        Json(json!({"error": "Queue is busy"})),
                    )
                }),
            )
            .route(
                "/api/history",
                get(|| async {
                    (
                        StatusCode::FOUND,
                        [(header::LOCATION, "http://example.invalid/")],
                    )
                }),
            );
        let (state, task) = test_backend(backend).await;
        let app = app(state);
        let res = app
            .clone()
            .oneshot(request(Method::GET, "/api/minimax-status", Body::empty()))
            .await
            .unwrap();
        assert_eq!(res.status(), StatusCode::CONFLICT);
        let value: Value =
            serde_json::from_slice(&to_bytes(res.into_body(), JSON_LIMIT).await.unwrap()).unwrap();
        assert_eq!(value["error"], "Queue is busy");
        let res = app
            .oneshot(request(Method::GET, "/api/history", Body::empty()))
            .await
            .unwrap();
        assert_eq!(res.status(), StatusCode::BAD_GATEWAY);
        assert!(res.headers().get(header::LOCATION).is_none());
        task.abort();
    }

    #[tokio::test]
    async fn unavailable_backend_returns_explicit_json_503() {
        let closed = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let state = AppState::new(10012, closed.local_addr().unwrap().port());
        drop(closed);
        let response = app(state)
            .oneshot(request(Method::GET, "/api/minimax-status", Body::empty()))
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::SERVICE_UNAVAILABLE);
        let value: Value =
            serde_json::from_slice(&to_bytes(response.into_body(), JSON_LIMIT).await.unwrap())
                .unwrap();
        assert!(value["error"].as_str().unwrap().contains("10013"));
    }
}
