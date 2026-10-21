package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type codeQLAdmission struct {
	RunID   int64  `json:"run_id"`
	Attempt int    `json:"attempt"`
	SHA     string `json:"sha"`
}

// Separate read-only admission: no resident bridge, credentials, request-selected
// commands or Cargo access are exposed to the guest.
func codeQLAdmissionHandler(p codeQLPlan, key string, recheck func(context.Context) error) http.Handler {
	return codeQLAdmissionHandlerForPlan(p, key, func(ctx context.Context, selected codeQLPlan) error {
		if selected != p {
			return errors.New("single-plan admission differs")
		}
		return recheck(ctx)
	})
}

func codeQLAdmissionHandlerForPlan(p codeQLPlan, key string, recheck func(context.Context, codeQLPlan) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/admit" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Product-Admission")), []byte(key)) != 1 {
			http.Error(w, "rejected", 403)
			return
		}
		var q codeQLAdmission
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&q) != nil || decoder.Decode(new(any)) != io.EOF {
			http.Error(w, "rejected", 403)
			return
		}
		selected := p
		if p.Batch != ([2]currentPRBinding{}) {
			var err error
			selected, err = selectFiniteBatchAdmission(p, q)
			if err != nil {
				http.Error(w, "rejected", 403)
				return
			}
		} else if q.RunID != p.RunID || q.Attempt != p.Attempt || q.SHA != p.SHA {
			http.Error(w, "rejected", 403)
			return
		}
		ctx, stop := context.WithTimeout(r.Context(), 35*time.Second)
		defer stop()
		if recheck(ctx, selected) != nil {
			http.Error(w, "current source unverified", 403)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func startCodeQLAdmission(ctx context.Context, p codeQLPlan, c candidate) (endpoint, key string, closeServer func() error, err error) {
	return startCodeQLAdmissionForRunner(ctx, p, c, "")
}

func startCodeQLAdmissionForRunner(ctx context.Context, p codeQLPlan, c candidate, runnerName string) (endpoint, key string, closeServer func() error, err error) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return "", "", nil, errors.New("admission listener unavailable")
	}
	secret := make([]byte, 32)
	if _, e = rand.Read(secret); e != nil {
		_ = l.Close()
		return "", "", nil, e
	}
	key = hex.EncodeToString(secret)
	childCtx, cancel := context.WithCancel(ctx)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 40 * time.Second, BaseContext: func(net.Listener) context.Context { return childCtx }}
	server.Handler = codeQLAdmissionHandlerForPlan(p, key, func(callCtx context.Context, selected codeQLPlan) error {
		candidate := c
		if p.Batch != ([2]currentPRBinding{}) {
			candidate = structCandidate(selected)
		}
		return verifyCodeQLAdmissionPlan(callCtx, candidate, selected, runnerName, get)
	})
	done := make(chan struct{})
	go func() { _ = server.Serve(l); close(done) }()
	closeServer = func() error {
		cancel()
		drainCtx, stop := context.WithTimeout(context.Background(), 45*time.Second)
		defer stop()
		e := server.Shutdown(drainCtx)
		if e != nil {
			_ = server.Close()
		}
		<-done
		return e
	}
	return "http://host.docker.internal:" + strconv.Itoa(l.Addr().(*net.TCPAddr).Port) + "/admit", key, closeServer, nil
}

func verifyCodeQLAdmissionPlan(ctx context.Context, c candidate, p codeQLPlan, runnerName string, read api) error {
	now, e := replanProductJob(ctx, c, p, read)
	if e != nil {
		return e
	}
	if now != p {
		return errors.New("live plan changed")
	}
	if p.Current != (currentPRBinding{}) {
		return awaitCurrentPRRunner(ctx, p, runnerName, read, currentPRAssignmentPause)
	}
	return nil
}

func requestCodeQLAdmission(ctx context.Context, p codeQLPlan, endpoint, key string) error {
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "http" || u.Hostname() != "host.docker.internal" || u.Path != "/admit" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(key) != 64 {
		return errors.New("admission endpoint rejected")
	}
	port, e := strconv.Atoi(u.Port())
	if e != nil || port < 1 || port > 65535 {
		return errors.New("admission port rejected")
	}
	b, _ := json.Marshal(codeQLAdmission{p.RunID, p.Attempt, p.SHA})
	r, e := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(b))
	if e != nil {
		return e
	}
	r.Header.Set("X-Product-Admission", key)
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 40 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, e := client.Do(r)
	if e != nil {
		return errors.New("live admission unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return errors.New("live admission rejected")
	}
	return nil
}
