package httpapi

import (
	"html/template"
	"net/http"
	"strings"
	"time"
)

type browserConnect struct {
	Secret   string
	Expires  time.Time
	Approved bool
}

var connectPage = template.Must(template.New("connect").Parse(`<!doctype html><html lang="ja"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>共有ToDoに接続</title><link rel="stylesheet" href="/assets/app.css"></head><body><main class="login"><h1>共有ToDoに接続</h1><p>登録済みのPCから、この画面への接続を確認してください。</p><p>接続番号</p><code id="connect-id">{{.}}</code><form method="post" action="/connect/finish"><button class="primary">接続を確認</button></form><p class="muted">この画面は5分間有効です。接続番号だけではサインインできません。</p><a href="/login">パスワードでサインイン</a></main></body></html>`))

func (s *Server) connectRoutes() {
	s.mux.HandleFunc("GET /connect", s.startConnect)
	s.mux.HandleFunc("POST /connect/finish", s.finishConnect)
	s.mux.HandleFunc("POST /api/v1/connect/{id}/approve", s.approveConnect)
}
func (s *Server) startConnect(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.session(r); ok {
		http.Redirect(w, r, "/", 303)
		return
	}
	s.connectMu.Lock()
	defer s.connectMu.Unlock()
	for id, entry := range s.connectRequests {
		if !entry.Expires.After(s.now()) {
			delete(s.connectRequests, id)
		}
	}
	if len(s.connectRequests) >= 64 {
		http.Error(w, "時間をおいてから、接続をやり直してください。", 429)
		return
	}
	id, secret := nonce(), nonce()
	s.connectRequests[id] = browserConnect{Secret: secret, Expires: s.now().Add(5 * time.Minute)}
	http.SetCookie(w, &http.Cookie{Name: "shared-todo-connect", Value: id + "." + secret, Path: "/connect", HttpOnly: true, Secure: s.Config.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	formPageHeaders(w)
	_ = connectPage.Execute(w, id)
}
func (s *Server) approveConnect(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		jsonError(w, 401, "admin_authentication_required")
		return
	}
	s.connectMu.Lock()
	defer s.connectMu.Unlock()
	id := r.PathValue("id")
	entry, ok := s.connectRequests[id]
	if !ok || !entry.Expires.After(s.now()) {
		jsonError(w, 404, "connection_not_found")
		return
	}
	entry.Approved = true
	s.connectRequests[id] = entry
	jsonReply(w, 200, map[string]bool{"ok": true})
}
func (s *Server) finishConnect(w http.ResponseWriter, r *http.Request) {
	if !s.validOrigin(r) {
		http.Error(w, "接続先を確認してください。", 403)
		return
	}
	cookie, err := r.Cookie("shared-todo-connect")
	if err != nil {
		http.Error(w, "接続を最初からやり直してください。", 401)
		return
	}
	id, secret, ok := strings.Cut(cookie.Value, ".")
	if !ok {
		http.Error(w, "接続を確認できません。", 401)
		return
	}
	s.connectMu.Lock()
	entry, exists := s.connectRequests[id]
	valid := exists && entry.Expires.After(s.now()) && entry.Approved && sameSecret(secret, entry.Secret)
	if valid {
		delete(s.connectRequests, id)
	}
	s.connectMu.Unlock()
	if !valid {
		http.Error(w, "登録済みのPCで接続を確認してから、もう一度このボタンを押してください。", 401)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "shared-todo-connect", Path: "/connect", MaxAge: -1, HttpOnly: true, Secure: s.Config.SecureCookies, SameSite: http.SameSiteLaxMode})
	s.setSession(w)
	http.Redirect(w, r, "/", 303)
}
