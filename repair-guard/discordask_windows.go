package main

import (
	"path/filepath"
	"regexp"
)

// The installed Discord unattended runner, as named by the verified repair
// repository: unattended-runner/install-tick.ps1 (runtime and tick launcher)
// and unattended-runner/conversation_cli.go (default configuration).
// cc-ask/src/conversation_bridge.rs is the normal CLI entry point and names
// this existing sender installation. Qualify that executable only; neither its
// message ledgers nor its credential/config siblings become readable.
// This is not a reconstruction of the lost private registry and not a general
// grant for the directory. Only these exact leaves qualify, by name and
// physical identity. Classification reads no file contents, so the
// configuration's values and the credential file it points to stay unread.
const discordAskDirectory = "D:/CC/Tools/discord-ask"

var discordAskInstalledLeaf = regexp.MustCompile(`^(?:discord_ask\.exe|unattended-runner\.exe|discord-ask-tick-hidden\.vbs)$`)

// Names an atomic replacement may create next to the installed executable.
var discordAskUpdateLeaf = regexp.MustCompile(`^(?:unattended-runner-pr[0-9]{1,6}\.atomic-backup\.exe|unattended-runner-install-pr[0-9]{1,6}\.private\.json)$`)

func discordAskRuntime(absolute string, protected func(string) bool) bool {
	return discordAskRuntimeIn(absolute, discordAskDirectory, protected)
}

func discordAskRuntimeIn(absolute, home string, protected func(string) bool) bool {
	name := filepath.Base(absolute)
	dir := filepath.Dir(absolute)
	switch {
	case same(dir, home) && discordAskInstalledLeaf.MatchString(name):
		return runtimeLeaf(absolute, home, protected, true)
	case same(dir, home) && discordAskUpdateLeaf.MatchString(name):
		return runtimeLeaf(absolute, home, protected, false)
	case same(dir, filepath.Join(home, "unattended")) && name == "config.json":
		return runtimeLeaf(absolute, filepath.Join(home, "unattended"), protected, true)
	}
	return false
}
