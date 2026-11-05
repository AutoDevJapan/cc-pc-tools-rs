package httpapi

import (
	"compress/gzip"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"autodevjapan/shared-todo-cloud/internal/store"
)

type Config struct {
	AdminToken      string
	SyncToken       string
	PublicURL       string
	SecureCookies   bool
	DepartmentOrder []string
}
type Server struct {
	Store           *store.Store
	Config          Config
	mux             *http.ServeMux
	now             func() time.Time
	connectMu       sync.Mutex
	connectRequests map[string]browserConnect
}

func New(s *store.Store, c Config) (*Server, error) {
	if s == nil || len(c.AdminToken) < 32 || len(c.SyncToken) < 32 {
		return nil, errors.New("store and independent admin/sync tokens of at least 32 characters are required")
	}
	if sameSecret(c.AdminToken, c.SyncToken) {
		return nil, errors.New("admin and sync tokens must be different")
	}
	if c.PublicURL != "" {
		u, e := url.Parse(c.PublicURL)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && u.Scheme != "http") || (c.SecureCookies && u.Scheme != "https") {
			return nil, errors.New("invalid public origin")
		}
	}
	a := &Server{Store: s, Config: c, mux: http.NewServeMux(), now: time.Now, connectRequests: make(map[string]browserConnect)}
	a.routes()
	return a, nil
}
func sameSecret(a, b string) bool {
	aa := sha256.Sum256([]byte(a))
	bb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(aa[:], bb[:]) == 1
}
func nonce() string {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasPrefix(r.URL.Path, "/api/") && !s.apiAuthorized(r) {
		jsonError(w, http.StatusUnauthorized, "authentication_required")
		return
	}
	s.mux.ServeHTTP(w, r)
}
func (s *Server) apiAuthorized(r *http.Request) bool {
	t, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}
	return sameSecret(t, s.Config.SyncToken) || sameSecret(t, s.Config.AdminToken)
}
func (s *Server) adminAuthorized(r *http.Request) bool {
	t, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && sameSecret(t, s.Config.AdminToken)
}
func (s *Server) session(r *http.Request) (string, bool) {
	c, e := r.Cookie("shared-todo-session")
	if e != nil {
		return "", false
	}
	payload, signature, ok := strings.Cut(c.Value, ".")
	if !ok {
		return "", false
	}
	b, e := base64.RawURLEncoding.DecodeString(payload)
	if e != nil {
		return "", false
	}
	mac := hmac.New(sha256.New, []byte("session:"+s.Config.AdminToken))
	mac.Write([]byte(payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !sameSecret(signature, expected) {
		return "", false
	}
	expires, random, ok := strings.Cut(string(b), ":")
	if !ok {
		return "", false
	}
	deadline, e := strconv.ParseInt(expires, 10, 64)
	if e != nil || s.now().Unix() >= deadline || random == "" {
		return "", false
	}
	return c.Value, true
}
func (s *Server) setSession(w http.ResponseWriter) {
	expires := s.now().Add(7 * 24 * time.Hour)
	payload := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(expires.Unix(), 10) + ":" + nonce()))
	mac := hmac.New(sha256.New, []byte("session:"+s.Config.AdminToken))
	mac.Write([]byte(payload))
	value := payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{Name: "shared-todo-session", Value: value, Path: "/", HttpOnly: true, Secure: s.Config.SecureCookies, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: 7 * 24 * 3600})
}
func (s *Server) csrf(session string) string {
	m := hmac.New(sha256.New, []byte("csrf:"+s.Config.AdminToken))
	m.Write([]byte(session))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func (s *Server) validOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	u, e := url.Parse(origin)
	if e != nil || u.Host == "" {
		return false
	}
	if s.Config.PublicURL != "" {
		expected, _ := url.Parse(s.Config.PublicURL)
		return u.Scheme == expected.Scheme && strings.EqualFold(u.Host, expected.Host)
	}
	return strings.EqualFold(u.Host, r.Host) && (u.Scheme == "http" || u.Scheme == "https")
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<20)
	d := json.NewDecoder(r.Body)
	d.UseNumber()
	if e := d.Decode(v); e != nil {
		jsonError(w, http.StatusBadRequest, "invalid_json")
		return false
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		jsonError(w, http.StatusBadRequest, "one_json_document_required")
		return false
	}
	return true
}
func jsonReply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(v)
}
func jsonError(w http.ResponseWriter, status int, code string) {
	jsonReply(w, status, map[string]any{"error": code})
}
func result(w http.ResponseWriter, v any, e error) {
	if e == nil {
		jsonReply(w, http.StatusOK, v)
		return
	}
	var se *store.Error
	if errors.As(e, &se) {
		status := http.StatusInternalServerError
		switch se.Code {
		case store.CodeConflict:
			status = http.StatusConflict
		case store.CodeLocked:
			status = http.StatusLocked
		case store.CodeNotFound:
			status = http.StatusNotFound
		case store.CodeInvalid:
			status = http.StatusBadRequest
			if se.Reason == "document_too_large" {
				status = http.StatusRequestEntityTooLarge
			}
		}
		jsonReply(w, status, se)
		return
	}
	jsonError(w, http.StatusInternalServerError, "storage_operation_failed")
}
func num(r *http.Request, key string, def int64) int64 {
	v, e := strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	if e != nil {
		return def
	}
	return v
}
func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if _, _, e := s.Store.Metadata(); e != nil {
			jsonError(w, http.StatusServiceUnavailable, "storage_unavailable")
			return
		}
		jsonReply(w, http.StatusOK, map[string]any{"ok": true, "service": "shared-todo"})
	})
	s.mux.HandleFunc("GET /api/v1/state", compressed(func(w http.ResponseWriter, r *http.Request) { v, e := s.Store.Snapshot(); result(w, v, e) }))
	s.mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		revision, stateID, err := s.Store.Version()
		result(w, map[string]any{"revision": revision, "stateId": stateID}, err)
	})
	s.mux.HandleFunc("GET /api/v1/items", func(w http.ResponseWriter, r *http.Request) {
		limit, e := queryLimit(r)
		if e != nil {
			jsonError(w, 400, "invalid_limit")
			return
		}
		v, e := s.Store.ListSummaries(store.ListOptions{Status: r.URL.Query().Get("status"), Project: r.URL.Query().Get("project"), Search: r.URL.Query().Get("q"), After: num(r, "after", 0), Limit: limit})
		result(w, v, e)
	})
	s.mux.HandleFunc("GET /api/v1/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if e != nil || id < 1 {
			jsonError(w, 400, "invalid_task_id")
			return
		}
		v, e := s.Store.Item(id)
		result(w, v, e)
	})
	s.mux.HandleFunc("GET /api/v1/changes", compressed(func(w http.ResponseWriter, r *http.Request) {
		after := num(r, "after", 0)
		if after < 0 {
			jsonError(w, 400, "invalid_revision")
			return
		}
		limit, e := queryLimit(r)
		if e != nil {
			jsonError(w, 400, "invalid_limit")
			return
		}
		v, e := s.Store.Changes(uint64(after), limit)
		result(w, v, e)
	}))
	s.mux.HandleFunc("GET /api/v1/operations/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.Operation(r.PathValue("id"))
		result(w, v, e)
	})
	s.mux.HandleFunc("POST /api/v1/lease/acquire", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			ClientID  string `json:"clientId"`
			RequestID string `json:"requestId"`
		}
		if !decode(w, r, &q) {
			return
		}
		v, e := s.Store.Acquire(q.ClientID, q.RequestID)
		result(w, v, e)
	})
	s.mux.HandleFunc("POST /api/v1/lease/renew", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			LeaseID string `json:"leaseId"`
		}
		if !decode(w, r, &q) {
			return
		}
		v, e := s.Store.Renew(q.LeaseID)
		result(w, v, e)
	})
	s.mux.HandleFunc("POST /api/v1/lease/release", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			LeaseID string `json:"leaseId"`
		}
		if !decode(w, r, &q) {
			return
		}
		e := s.Store.Release(q.LeaseID)
		result(w, map[string]bool{"ok": e == nil}, e)
	})
	s.mux.HandleFunc("POST /api/v1/commit", func(w http.ResponseWriter, r *http.Request) {
		var q store.CommitRequest
		if !decode(w, r, &q) {
			return
		}
		v, e := s.Store.Commit(q)
		result(w, v, e)
	})
	s.mux.HandleFunc("PUT /api/v1/assets/{sha}", func(w http.ResponseWriter, r *http.Request) {
		b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 128<<20))
		if e != nil {
			jsonError(w, 413, "asset_too_large")
			return
		}
		v, e := s.Store.PutAsset(store.Asset{SHA256: r.PathValue("sha"), OriginalPath: r.URL.Query().Get("path"), Kind: r.URL.Query().Get("kind"), Bytes: b})
		result(w, v, e)
	})
	s.mux.HandleFunc("GET /api/v1/assets/{sha}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.Asset(r.PathValue("sha"))
		if e != nil {
			result(w, nil, e)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="asset.bin"`)
		w.Header().Set("X-Content-SHA256", v.SHA256)
		w.WriteHeader(200)
		_, _ = w.Write(v.Bytes)
	})
	s.mux.HandleFunc("GET /api/v1/assets/resolve", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.ResolveAsset(r.URL.Query().Get("path"))
		result(w, v, e)
	})
	s.mux.HandleFunc("PUT /api/v1/imports/{id}/chunks/{index}", func(w http.ResponseWriter, r *http.Request) {
		index, e := strconv.Atoi(r.PathValue("index"))
		if e != nil || index < 0 {
			jsonError(w, 400, "invalid_chunk_index")
			return
		}
		b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, store.MaxChunkBytes+1))
		if e != nil || len(b) > store.MaxChunkBytes {
			jsonError(w, 413, "chunk_too_large")
			return
		}
		e = s.Store.ImportChunk(r.PathValue("id"), index, b)
		result(w, map[string]any{"ok": e == nil, "index": index, "bytes": len(b)}, e)
	})
	s.mux.HandleFunc("POST /api/v1/imports/{id}/finalize", func(w http.ResponseWriter, r *http.Request) {
		var q store.FinalizeRequest
		if !decode(w, r, &q) {
			return
		}
		v, e := s.Store.Finalize(r.PathValue("id"), q)
		result(w, v, e)
	})
	s.mux.HandleFunc("POST /api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		if !s.adminAuthorized(r) {
			jsonError(w, 401, "admin_authentication_required")
			return
		}
		s.setSession(w)
		jsonReply(w, 200, map[string]bool{"ok": true})
	})
	s.mux.HandleFunc("GET /api/v1/allocations", compressed(func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.Registry("task-allocations")
		result(w, v, e)
	}))
	s.mux.HandleFunc("PUT /api/v1/allocations", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Document json.RawMessage `json:"document"`
			Previous string          `json:"previousSHA256"`
		}
		if !decode(w, r, &q) {
			return
		}
		if _, e := parseAllocations(q.Document); e != nil {
			jsonError(w, 400, "invalid_allocations")
			return
		}
		v, e := s.Store.PutRegistry("task-allocations", q.Document, q.Previous)
		if e != nil {
			result(w, nil, e)
			return
		}
		jsonReply(w, 200, map[string]string{"sha256": v.SHA256})
	})
	s.uiRoutes()
	s.connectRoutes()
}

func queryLimit(r *http.Request) (int, error) {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return 100, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || n > store.MaxLimit {
		return 0, errors.New("invalid limit")
	}
	return n, nil
}

type gzipWriter struct {
	http.ResponseWriter
	body *gzip.Writer
}

func (w gzipWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func compressed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		accepts := false
		for _, field := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
			parts := strings.Split(strings.TrimSpace(field), ";")
			if parts[0] != "gzip" {
				continue
			}
			accepts = true
			for _, parameter := range parts[1:] {
				if k, v, ok := strings.Cut(strings.TrimSpace(parameter), "="); ok && k == "q" {
					quality, err := strconv.ParseFloat(v, 64)
					accepts = err == nil && quality > 0
				}
			}
		}
		if !accepts {
			next(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		body := gzip.NewWriter(w)
		defer body.Close()
		next(gzipWriter{w, body}, r)
	}
}

func (s *Server) uiRoutes() {
	s.mux.HandleFunc("GET /", s.dashboard)
	s.mux.HandleFunc("GET /login", s.login)
	s.mux.HandleFunc("POST /login", s.signin)
	s.mux.HandleFunc("POST /logout", s.signout)
	s.mux.HandleFunc("GET /assets/app.css", s.css)
	s.mux.HandleFunc("POST /tasks", s.saveTask)
	s.mux.HandleFunc("GET /tasks/{id}/history", s.history)
	s.mux.HandleFunc("GET /tasks/{id}/spec", s.specification)
}

func parseRevision(v string) (uint64, error) {
	n, e := strconv.ParseUint(v, 10, 64)
	if e != nil {
		return 0, fmt.Errorf("invalid revision")
	}
	return n, nil
}
