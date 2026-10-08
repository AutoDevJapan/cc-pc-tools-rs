package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const extensionID = "hehggadaopoacecdllhhajmbjkdcmajg"

type report struct {
	CheckedAt             string   `json:"checked_at"`
	Ready                 bool     `json:"ready"`
	ExtensionInstalled    bool     `json:"extension_installed"`
	ExtensionEnabled      bool     `json:"extension_enabled"`
	ManifestValid         bool     `json:"manifest_valid"`
	HostBinaryPresent     bool     `json:"host_binary_present"`
	DesktopPipeConfigured bool     `json:"desktop_pipe_configured"`
	DesktopPipePresent    bool     `json:"desktop_pipe_present"`
	Connected             bool     `json:"connected"`
	Category              string   `json:"category"`
	Problems              []string `json:"problems"`
}

func (r *report) finish() {
	r.Ready = r.ExtensionInstalled && r.ExtensionEnabled && r.ManifestValid && r.HostBinaryPresent && r.Connected
	switch {
	case !r.ExtensionInstalled:
		r.Category = "extension_not_installed_in_selected_profile"
	case !r.ExtensionEnabled:
		r.Category = "extension_disabled_or_files_missing"
	case !r.ManifestValid:
		r.Category = "native_registration_invalid"
	case !r.HostBinaryPresent:
		r.Category = "native_binary_missing"
	case !r.Connected:
		r.Category = "extension_transport_disconnected"
	default:
		r.Category = "ready"
	}
}

func inventoryConnected(raw []byte) (bool, error) {
	var entries []struct {
		Pipe       string            `json:"pipe"`
		Instance   string            `json:"instance"`
		Generation string            `json:"generation"`
		Tabs       []json.RawMessage `json:"tabs"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Pipe != "" && e.Instance != "" && e.Generation != "" && e.Tabs != nil {
			return true, nil
		}
	}
	return false, nil
}

func checkExtension(root string, r *report) {
	var state struct {
		Profile struct {
			LastUsed string `json:"last_used"`
		} `json:"profile"`
	}
	b, err := os.ReadFile(filepath.Join(root, "Local State"))
	if err != nil || json.Unmarshal(b, &state) != nil {
		r.Problems = append(r.Problems, "profile_state_unreadable")
		return
	}
	profile := state.Profile.LastUsed
	if profile == "" || filepath.Base(profile) != profile || profile == "." || profile == ".." || strings.ContainsAny(profile, `/\`) {
		r.Problems = append(r.Problems, "profile_selection_invalid")
		return
	}
	var prefs struct {
		Extensions struct {
			Settings map[string]struct {
				Path           string            `json:"path"`
				State          *int              `json:"state"`
				DisableReasons []json.RawMessage `json:"disable_reasons"`
			} `json:"settings"`
		} `json:"extensions"`
	}
	b, err = os.ReadFile(filepath.Join(root, profile, "Secure Preferences"))
	if err != nil || json.Unmarshal(b, &prefs) != nil {
		r.Problems = append(r.Problems, "extension_state_unreadable")
		return
	}
	e, ok := prefs.Extensions.Settings[extensionID]
	r.ExtensionInstalled = ok
	if !ok {
		return
	}
	// A state field is absent in current Chrome; explicit disable reasons still apply.
	r.ExtensionEnabled = (e.State == nil || *e.State == 1) && len(e.DisableReasons) == 0
	if e.Path == "" || filepath.IsAbs(e.Path) {
		r.ExtensionEnabled = false
		return
	}
	clean := filepath.Clean(e.Path)
	if strings.HasPrefix(clean, "..") {
		r.ExtensionEnabled = false
		return
	}
	if _, err := os.Stat(filepath.Join(root, profile, "Extensions", clean, "manifest.json")); err != nil {
		r.ExtensionEnabled = false
	}
}

func checkManifest(path string, r *report) {
	var m struct {
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		Path    string   `json:"path"`
		Origins []string `json:"allowed_origins"`
	}
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &m) != nil {
		return
	}
	origin := false
	for _, o := range m.Origins {
		if o == "chrome-extension://"+extensionID+"/" {
			origin = true
		}
	}
	r.ManifestValid = m.Name == "com.openai.codexextension" && m.Type == "stdio" && origin && filepath.IsAbs(m.Path)
	if r.ManifestValid {
		if fi, err := os.Stat(m.Path); err == nil && !fi.IsDir() && fi.Size() > 0 {
			r.HostBinaryPresent = true
		}
	}
}

func desktopReference(config []byte) (bool, string) {
	section := regexp.MustCompile(`(?ms)^\[mcp_servers\.node_repl\.env\]\s*\r?\n(.*?)(?:^\[|\z)`).FindSubmatch(config)
	if len(section) < 2 {
		return false, ""
	}
	if !regexp.MustCompile(`(?m)^SKY_CUA_NATIVE_PIPE\s*=\s*"1"\s*$`).Match(section[1]) {
		return false, ""
	}
	m := regexp.MustCompile(`(?m)^SKY_CUA_NATIVE_PIPE_DIRECTORY\s*=\s*'([^']*)'\s*$`).FindSubmatch(section[1])
	if len(m) < 2 {
		return true, ""
	}
	return true, string(m[1])
}

func writeReport(path string, r report) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".connection-status-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		os.Remove(name)
		return err
	}
	if err = f.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err = os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func main() {
	output := flag.String("output", "D:/CC/Tools/owner-chrome/connection-status.json", "health report; no page contents or account identifiers")
	cli := flag.String("owner-chrome", "D:/CC/Tools/owner-chrome/owner_chrome.exe", "installed tab inventory CLI")
	flag.Parse()
	r := report{CheckedAt: time.Now().UTC().Format(time.RFC3339), Problems: []string{}}
	collect(&r)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	cmd := exec.CommandContext(ctx, *cli, "inventory")
	cmd.WaitDelay = 2 * time.Second
	hidden(cmd)
	b, err := cmd.Output()
	cancel()
	if err != nil {
		r.Problems = append(r.Problems, "inventory_failed_or_timed_out")
	} else if r.Connected, err = inventoryConnected(b); err != nil {
		r.Problems = append(r.Problems, "inventory_response_invalid")
	}
	r.finish()
	if err := writeReport(*output, r); err != nil {
		os.Exit(2)
	}
	if !r.Ready {
		os.Exit(1)
	}
}
