package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
)

const installRoot = "D:/CC/Tools/codex-progress-tui"

type manifest struct {
	Version         string `json:"upstream_version"`
	CodexSHA        string `json:"codex_sha256"`
	CollectorSHA    string `json:"collector_sha256"`
	CodeModeHostSHA string `json:"code_mode_host_sha256"`
}

var valueOptions = map[string]bool{
	"-c": true, "--config": true, "--enable": true, "--disable": true,
	"--remote": true, "--remote-auth-token-env": true, "-i": true, "--image": true,
	"-m": true, "--model": true, "--local-provider": true, "-p": true, "--profile": true,
	"-s": true, "--sandbox": true, "-C": true, "--cd": true, "--add-dir": true,
	"-a": true, "--ask-for-approval": true,
}

var commands = map[string]bool{
	"exec": true, "e": true, "app-server": true, "archive": true, "apply": true,
	"cloud": true, "completion": true, "debug": true, "delete": true, "doctor": true,
	"exec-server": true, "features": true, "help": true, "login": true, "logout": true,
	"mcp": true, "plugin": true, "sandbox": true, "unarchive": true, "update": true,
	"remote-control": true, "export": true, "logs": true,
	"review": true, "app": true, "queue": true, "migrate-rollouts": true,
	"tcp-tunnel": true, "execpolicy": true, "responses-api-proxy": true,
	"stdio-to-uds": true, "a": true, "cloud-tasks": true,
}

func interactive(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--help" || arg == "-h" || arg == "--version" || arg == "-V" {
			return false
		}
		if arg == "--" {
			return true
		}
		if valueOptions[arg] {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if arg == "resume" || arg == "fork" || arg == "agents" {
			return true
		}
		return !commands[arg]
	}
	return true
}

func digest(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, 1024*1024*1024+1))
	if err != nil || n > 1024*1024*1024 {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func officialPackage(appData string) string {
	return filepath.Join(appData, "npm", "node_modules", "@openai", "codex")
}

func officialBinary(pkg string) string {
	paths := []string{
		filepath.Join(pkg, "node_modules", "@openai", "codex-win32-x64", "vendor", "x86_64-pc-windows-msvc", "bin", "codex.exe"),
		filepath.Join(filepath.Dir(pkg), "codex-win32-x64", "vendor", "x86_64-pc-windows-msvc", "bin", "codex.exe"),
		filepath.Join(pkg, "vendor", "x86_64-pc-windows-msvc", "bin", "codex.exe"),
	}
	for _, path := range paths {
		if stat, err := os.Stat(path); err == nil && stat.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

func progressBinary(root, pkg string) string {
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil || len(data) > 4096 {
		return ""
	}
	var m manifest
	if json.Unmarshal(data, &m) != nil || m.Version == "" || len(m.CodexSHA) != 64 || len(m.CollectorSHA) != 64 || len(m.CodeModeHostSHA) != 64 {
		return ""
	}
	packageData, err := os.ReadFile(filepath.Join(pkg, "package.json"))
	if err != nil || len(packageData) > 64*1024 {
		return ""
	}
	var version struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(packageData, &version) != nil || version.Version != m.Version {
		return ""
	}
	path := filepath.Join(root, "codex.exe")
	collector := filepath.Join(filepath.Dir(root), "agent-progress", "agent-progress.exe")
	if digest(path) != strings.ToLower(m.CodexSHA) || digest(collector) != strings.ToLower(m.CollectorSHA) {
		return ""
	}
	if digest(filepath.Join(root, "codex-code-mode-host.exe")) != strings.ToLower(m.CodeModeHostSHA) {
		return ""
	}
	return path
}

func choose(args []string, root, pkg string, disabled bool) (string, bool) {
	if interactive(args) && !disabled {
		if path := progressBinary(root, pkg); path != "" {
			return path, true
		}
	}
	return officialBinary(pkg), false
}

func main() {
	pkg := officialPackage(os.Getenv("APPDATA"))
	path, enabled := choose(os.Args[1:], installRoot, pkg, os.Getenv("CC_AGENT_PROGRESS_DISABLED") == "1")
	if path == "" {
		_, _ = io.WriteString(os.Stderr, "Codexの実行ファイルが見つかりません。\n")
		os.Exit(2)
	}
	cmd := exec.Command(path, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "CODEX_MANAGED_PACKAGE_ROOT="+pkg, "CODEX_MANAGED_BY_NPM=1")
	if enabled {
		cmd.Env = append(cmd.Env, "CC_AGENT_PROGRESS=1")
	} else {
		cmd.Env = append(cmd.Env, "CC_AGENT_PROGRESS=0")
	}
	// Windows broadcasts console Ctrl+C to this wrapper and its child. Keep
	// the wrapper alive while Codex handles the event and drains its own work.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			os.Exit(exit.ExitCode())
		}
		os.Exit(2)
	}
}
