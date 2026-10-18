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
