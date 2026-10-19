package main

import (
	"os"
	"runtime/debug"
	"strings"
	"testing"
)

func TestPR148HelperBuiltPair(t *testing.T) {
	host, guest := os.Getenv("CC_PR148_PAIR_HOST"), os.Getenv("CC_PR148_PAIR_GUEST")
	if host == "" || guest == "" {
		t.Skip("explicit same-source built helper pair required")
	}
	h, e := os.ReadFile(host)
	if e != nil {
		t.Fatal(e)
	}
	g, e := os.ReadFile(guest)
	if e != nil {
		t.Fatal(e)
	}
	sha, revision, e := pr148HelperPairBytes(h, g)
	if e != nil || sha != digest(h) || len(revision) != 40 {
		t.Fatalf("built pair: %v", e)
	}
	t.Logf("controller bytes=%d sha256=%s guest bytes=%d sha256=%s source=%s", len(h), sha, len(g), digest(g), revision)
}

func TestPR148HelperPairSourceBinding(t *testing.T) {
	makeInfo := func() *debug.BuildInfo {
		return &debug.BuildInfo{GoVersion: "go1.25.0", Main: debug.Module{Path: "cc/product-ci"}, Settings: []debug.BuildSetting{{Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: strings.Repeat("a", 40)}, {Key: "vcs.modified", Value: "false"}, {Key: "GOOS", Value: "linux"}, {Key: "GOARCH", Value: "amd64"}}}
	}
	for _, bad := range []string{"", "revision", "dirty", "missing", "os", "arch", "module", "go-version", "dependencies", "absent"} {
		t.Run(bad, func(t *testing.T) {
			h, g := makeInfo(), makeInfo()
			switch bad {
			case "revision":
				g.Settings[1].Value = strings.Repeat("b", 40)
			case "dirty":
				g.Settings[2].Value = "true"
			case "missing":
				g.Settings = g.Settings[3:]
			case "os":
				g.Settings[3].Value = "windows"
			case "arch":
				g.Settings[4].Value = "arm64"
			case "module":
				g.Main.Path = "other"
			case "go-version":
				g.GoVersion = "other"
			case "dependencies":
				g.Deps = []*debug.Module{{Path: "other", Version: "v1.0.0"}}
			case "absent":
				g = nil
			}
			_, e := pr148CompareHelperBuilds(h, g)
			if (e != nil) != (bad != "") {
				t.Fatalf("pair %q: %v", bad, e)
			}
		})
	}
}
