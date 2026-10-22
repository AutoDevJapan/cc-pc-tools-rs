package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestTechnicalRetriesUseVerifiedScopeAndAtomicCLI(t *testing.T) {
	r, c := fixture(t)
	root := t.TempDir()
	xRoot := t.TempDir()
	since := c.Unix() - 3600
	db := fmt.Sprintf(`{"projects":{"cc-pc-tools-rs":%q,"other":%q,"x-bookmarks":%q},"items":[
	{"id":1,"project":"cc-pc-tools-rs","status":"wait","wait_ready":{"condition":"retry-time","since":%d,"not_before":%d}},
	{"id":2,"project":"cc-pc-tools-rs","status":"block","wait_ready":{"condition":"retry-time","since":%d,"not_before":%d}},
	{"id":3,"project":"other","status":"wait","wait_ready":{"condition":"retry-time","since":%d,"not_before":%d}},
	{"id":4,"project":"cc-pc-tools-rs","status":"wait","wait_ready":{"condition":"chatgpt-plus","since":%d,"not_before":%d}},
	{"id":5,"project":"cc-pc-tools-rs","status":"wait","wait_ready":{"condition":"retry-time","since":%d,"not_before":%d}},
	{"id":6,"project":"x-bookmarks","status":"wait","wait_ready":{"condition":"retry-time","since":%d,"not_before":%d}},
	{"id":7,"project":"x-bookmarks","status":"block","owner_stopped":true,"wait_ready":{"condition":"retry-time","since":%d,"not_before":%d}},
	{"id":8,"project":"x-bookmarks","status":"wait","wait_ready":{"condition":"retry-time","since":%d,"not_before":%d}},
	{"id":9,"project":"x-bookmarks","status":"wait","wait_ready":{"condition":"chatgpt-plus","since":%d,"not_before":%d}}]}`,
		root, root, xRoot, since, c.Unix(), since, c.Unix(), since, c.Unix(), since, c.Unix(), since, c.Unix()+600,
		since, c.Unix(), since, c.Unix(), since, c.Unix()+600, since, c.Unix())
	if err := os.WriteFile(r.TodoPath, []byte(db), 0600); err != nil {
		t.Fatal(err)
	}
	for _, proof := range []string{`{"client_resource":false}`, `{"client_resource":true}`, `{"client_resource":true,"classification_unverified":true}`, `{}`} {
		var resumed []string
		r.Run = func(_ context.Context, exe string, args ...string) (string, error) {
			if filepath.Base(exe) == "scrub391.exe" {
				return proof, nil
			}
			if len(args) != 4 || args[0] != "resume-ready" || args[2] != "--condition" || args[3] != "retry-time" {
				t.Fatal("unexpected command")
			}
			resumed = append(resumed, args[1])
			return fmt.Sprintf(`{"id":%s,"resumed":true}`, args[1]), nil
		}
		var h Health
		r.releaseRetries(context.Background(), &h)
		if proof == `{"client_resource":false}` {
			if !reflect.DeepEqual(resumed, []string{"1", "6"}) || h.Retried != 2 {
				t.Fatal("due repair was not released", resumed, h)
			}
		} else if len(resumed) != 0 || h.Retried != 0 {
			t.Fatal("unverified project was released")
		}
	}
}

func TestFailedTechnicalRetryDoesNotStarveLaterItems(t *testing.T) {
	r, c := fixture(t)
	root := t.TempDir()
	body := fmt.Sprintf(`{"projects":{"cc-pc-tools-rs":%q},"items":[`, root)
	for id := 1; id <= 6; id++ {
		if id > 1 {
			body += ","
		}
		body += fmt.Sprintf(`{"id":%d,"project":"cc-pc-tools-rs","status":"wait","wait_ready":{"condition":"retry-time","since":%d,"not_before":%d}}`, id, c.Unix()-60, c.Unix())
	}
	body += "]}"
	if err := os.WriteFile(r.TodoPath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	var attempted []int
	r.Run = func(_ context.Context, exe string, args ...string) (string, error) {
		if filepath.Base(exe) == "scrub391.exe" {
			return `{"client_resource":false}`, nil
		}
		id, _ := strconv.Atoi(args[1])
		attempted = append(attempted, id)
		return "", errors.New("finite transient failure")
	}
	var h Health
	r.releaseRetries(context.Background(), &h)
	r.releaseRetries(context.Background(), &h)
	if len(attempted) != 8 || attempted[0] != 1 || attempted[4] != 5 || attempted[5] != 6 {
		t.Fatal("later retries starved", attempted)
	}
}
