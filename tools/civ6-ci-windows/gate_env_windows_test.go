//go:build windows && amd64

package main

import (
	"strings"
	"testing"
)

func TestControllerGatePreservesOnlyConfigLocations(t *testing.T) {
	for _, key := range []string{"APPDATA", "GH_CONFIG_DIR", "XDG_CONFIG_HOME"} {
		t.Setenv(key, "controller-config-fixture-"+key)
	}
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(key, "not-a-real-token")
	}
	env := map[string]string{}
	for _, entry := range controllerGateEnv() {
		k, v, _ := strings.Cut(entry, "=")
		env[k] = v
	}
	for _, key := range []string{"APPDATA", "GH_CONFIG_DIR", "XDG_CONFIG_HOME"} {
		if env[key] != "controller-config-fixture-"+key {
			t.Fatalf("config location lost: %s", key)
		}
	}
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if _, ok := env[key]; ok {
			t.Fatal("token forwarded")
		}
	}
	for _, entry := range workerEnv(fixturePlan()) {
		if strings.Contains(entry, "controller-config-fixture") {
			t.Fatal("controller config crossed into worker")
		}
	}
}

func TestSDKProxiesRemainInsideNormalOwnedCleanup(t *testing.T) {
	env := map[string]string{}
	for _, entry := range workerEnv(fixturePlan()) {
		k, v, _ := strings.Cut(entry, "=")
		env[k] = v
	}
	// Released rustup (1.28/1.29) and the newer spelling both remain explicit;
	// neither an inherited host value nor CI=true decides how proxies are made.
	for _, key := range []string{"RUSTUP_HARDLINK_PROXIES", "RUSTUP_FORCE_HARDLINK_PROXIES"} {
		if env[key] != "1" {
			t.Fatalf("%s does not prevent uncollectable symlink proxies", key)
		}
	}
}
