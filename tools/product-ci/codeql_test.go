package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func codeQLFixture(t *testing.T, change func(map[string]any)) api {
	t.Helper()
	c := candidates[0]
	r := map[string]any{"id": c.ID, "full_name": "AutoDevJapan/opaque-fixture", "private": true, "fork": false, "owner": map[string]any{"login": "AutoDevJapan"}, "permissions": map[string]bool{"admin": true}, "default_branch": "main"}
	data := map[string]any{
		fmt.Sprintf("repositories/%d", c.ID):                            r,
		"repos/AutoDevJapan/opaque-fixture/pulls/1002":                  map[string]any{"state": "open", "head": map[string]any{"sha": c.SHA, "ref": "candidate", "repo": r}, "base": map[string]any{"repo": r}},
		"repos/AutoDevJapan/opaque-fixture/git/ref/heads/candidate":     map[string]any{"object": map[string]string{"sha": c.SHA}},
		"repos/AutoDevJapan/opaque-fixture/git/ref/heads/main":          map[string]any{"object": map[string]string{"sha": strings.Repeat("a", 40)}},
		"repos/AutoDevJapan/opaque-fixture/code-scanning/default-setup": map[string]any{"state": "configured", "runner_type": "labeled", "runner_label": codeQLLabel(c.ID)},
		"repos/AutoDevJapan/opaque-fixture/actions/runs/7":              map[string]any{"id": 7, "run_attempt": 2, "head_sha": c.SHA, "head_branch": "candidate", "event": "dynamic", "path": "dynamic/github-code-scanning/codeql", "status": "queued", "repository": r, "head_repository": r},
	}
	for _, sha := range []string{c.SHA, strings.Repeat("a", 40)} {
		data["repos/AutoDevJapan/opaque-fixture/git/commits/"+sha] = map[string]any{"tree": map[string]string{"sha": strings.Repeat("c", 40)}}
	}
	if change != nil {
		change(data)
	}
	return func(_ context.Context, p string, out any) error {
		v, ok := data[p]
		if !ok {
			return fmt.Errorf("unexpected test endpoint")
		}
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		return json.Unmarshal(b, out)
	}
}

func TestCodeQLPlanAdmission(t *testing.T) {
	c := candidates[0]
	for _, tc := range []struct {
		name, key string
		value     any
	}{
		{"wrong run", "id", 8}, {"wrong workflow", "path", ".github/workflows/arbitrary.yml"}, {"wrong event", "event", "pull_request_target"}, {"finished", "status", "completed"}, {"obsolete source", "head_sha", strings.Repeat("b", 40)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := codeQLFixture(t, func(d map[string]any) {
				d["repos/AutoDevJapan/opaque-fixture/actions/runs/7"].(map[string]any)[tc.key] = tc.value
			})
			if _, e := planCodeQL(context.Background(), c, 7, read); e == nil {
				t.Fatal("unsafe run admitted")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"fork", func(d map[string]any) { d[fmt.Sprintf("repositories/%d", c.ID)].(map[string]any)["fork"] = true }},
		{"foreign head", func(d map[string]any) {
			d["repos/AutoDevJapan/opaque-fixture/actions/runs/7"].(map[string]any)["head_repository"] = map[string]any{"id": 99}
		}},
		{"hosted route", func(d map[string]any) {
			d["repos/AutoDevJapan/opaque-fixture/code-scanning/default-setup"].(map[string]any)["runner_type"] = "standard"
		}},
		{"shared label", func(d map[string]any) {
			d["repos/AutoDevJapan/opaque-fixture/code-scanning/default-setup"].(map[string]any)["runner_label"] = "cc-pc-tools-linux"
		}},
		{"stale candidate", func(d map[string]any) {
			d["repos/AutoDevJapan/opaque-fixture/git/ref/heads/candidate"].(map[string]any)["object"] = map[string]string{"sha": strings.Repeat("f", 40)}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := planCodeQL(context.Background(), c, 7, codeQLFixture(t, tc.mutate)); e == nil {
				t.Fatal("unsafe plan admitted")
			}
		})
	}
	p, e := planCodeQL(context.Background(), c, 7, codeQLFixture(t, nil))
	if e != nil || p.Role != "candidate" || p.SHA != c.SHA {
		t.Fatalf("valid candidate rejected: %v", e)
	}
	p, e = planCodeQL(context.Background(), c, 7, codeQLFixture(t, func(d map[string]any) {
		r := d["repos/AutoDevJapan/opaque-fixture/actions/runs/7"].(map[string]any)
		r["head_sha"] = strings.Repeat("a", 40)
		r["head_branch"] = "main"
	}))
	if e != nil || p.Role != "setup-validation" || p.Candidate != c.SHA {
		t.Fatalf("setup evidence not separated: %v", e)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "opaque-fixture") {
		t.Fatal("repository name leaked into host evidence")
	}
}

func TestCodeQLHookRunBinding(t *testing.T) {
	p := codeQLPlan{Repo: "AutoDevJapan/opaque-fixture", SHA: strings.Repeat("a", 40), RunID: 7, Attempt: 2}
	if e := checkCodeQLHook(p, p.Repo, p.SHA, "7", "2"); e != nil {
		t.Fatal(e)
	}
	for _, v := range [][4]string{{"Other/fork", p.SHA, "7", "2"}, {p.Repo, strings.Repeat("b", 40), "7", "2"}, {p.Repo, p.SHA, "8", "2"}, {p.Repo, p.SHA, "7", "1"}} {
		if e := checkCodeQLHook(p, v[0], v[1], v[2], v[3]); e == nil {
			t.Fatal("mismatched job hook admitted")
		}
	}
}

func TestCodeQLIsolationRejectsPrivilegeAndMounts(t *testing.T) {
	i := isolation{}
	i.Config.User = "1001:1001"
	i.Config.Image = codeQLSDK
	i.Config.Labels = map[string]string{"cc.todo": "2648"}
	init := true
	pids := int64(512)
	h := &i.HostConfig
	h.Init = &init
	h.ReadonlyRootfs = true
	h.NetworkMode = "bridge"
	h.NanoCPUs = 2e9
	h.Memory = 8 << 30
	h.PidsLimit = &pids
	h.CapDrop = []string{"ALL"}
	h.SecurityOpt = []string{"no-new-privileges"}
	h.Tmpfs = map[string]string{"/runner": "uid=1001", "/work": "uid=1001", "/home/runner": "uid=1001", "/tmp": "noexec,uid=1001"}
	encode := func() []byte { b, _ := json.Marshal([]isolation{i}); return b }
	if e := codeQLIsolation(encode()); e != nil {
		t.Fatal(e)
	}
	h.Binds = []string{"/private:/work"}
	if codeQLIsolation(encode()) == nil {
		t.Fatal("host bind admitted")
	}
	h.Binds = nil
	h.Privileged = true
	if codeQLIsolation(encode()) == nil {
		t.Fatal("privilege admitted")
	}
	h.Privileged = false
	h.DeviceRequests = []json.RawMessage{json.RawMessage(`{"Capabilities":[["gpu"]]}`)}
	if codeQLIsolation(encode()) == nil {
		t.Fatal("GPU request admitted")
	}
	h.DeviceRequests = nil
	init = false
	if codeQLIsolation(encode()) == nil {
		t.Fatal("missing reaper admitted")
	}
}
