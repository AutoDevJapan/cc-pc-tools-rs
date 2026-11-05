package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func connectFixture(t *testing.T, s *Server) (string, *http.Cookie) {
	t.Helper()
	w := perform(s, "GET", "/connect", "", "", nil, "")
	if w.Code != 200 {
		t.Fatal("connection page unavailable")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing browser-bound cookie")
	}
	id, secret, ok := strings.Cut(cookies[0].Value, ".")
	if !ok || !strings.Contains(w.Body.String(), id) || strings.Contains(w.Body.String(), secret) {
		t.Fatal("public connection number exposed its browser secret")
	}
	return id, cookies[0]
}
func TestConnectedPCSignInBindsApprovalToOneBrowser(t *testing.T) {
	s := testServer(t)
	id, cookie := connectFixture(t, s)
	if perform(s, "POST", "/connect/finish", "", "", cookie, testOrigin).Code != 401 {
		t.Fatal("unapproved connection signed in")
	}
	if perform(s, "POST", "/api/v1/connect/"+id+"/approve", "", testSync, nil, "").Code != 401 {
		t.Fatal("sync credential can approve owner sign in")
	}
	if perform(s, "POST", "/api/v1/connect/"+id+"/approve", "", testAdmin, nil, "").Code != 200 {
		t.Fatal("registered PC could not approve connection")
	}
	copy := *cookie
	copy.Value = id + ".wrong-secret"
	if perform(s, "POST", "/connect/finish", "", "", &copy, testOrigin).Code != 401 {
		t.Fatal("connection number alone signed in another browser")
	}
	if perform(s, "POST", "/connect/finish", "", "", cookie, "https://elsewhere.example").Code != 403 {
		t.Fatal("cross-origin connection accepted")
	}
	w := perform(s, "POST", "/connect/finish", "", "", cookie, testOrigin)
	if w.Code != 303 {
		t.Fatal("approved owner sign in failed")
	}
	owner := false
	for _, c := range w.Result().Cookies() {
		if c.Name == "shared-todo-session" && c.HttpOnly && c.Secure {
			owner = true
		}
	}
	if !owner {
		t.Fatal("owner session not set securely")
	}
	if perform(s, "POST", "/connect/finish", "", "", cookie, testOrigin).Code != 401 {
		t.Fatal("connection approval reused")
	}
}
func TestConnectionExpiresAndRestartDoesNotTrustOldNumber(t *testing.T) {
	s := testServer(t)
	id, cookie := connectFixture(t, s)
	if perform(s, "POST", "/api/v1/connect/"+id+"/approve", "", testAdmin, nil, "").Code != 200 {
		t.Fatal("approval fixture failed")
	}
	restarted, err := New(s.Store, s.Config)
	if err != nil {
		t.Fatal(err)
	}
	if perform(restarted, "POST", "/connect/finish", "", "", cookie, testOrigin).Code != 401 {
		t.Fatal("restart trusted an obsolete pending connection")
	}
	s.now = func() time.Time { return time.Now().Add(6 * time.Minute) }
	if perform(s, "POST", "/api/v1/connect/"+id+"/approve", "", testAdmin, nil, "").Code != 404 {
		t.Fatal("expired browser approval accepted")
	}
	if perform(s, "POST", "/connect/finish", "", "", cookie, testOrigin).Code != 401 {
		t.Fatal("expired connection signed in")
	}
}
