package main

import (
	"errors"
	"fmt"
	"time"
)

var errConfigurationUnverified = errors.New("runner setup remains unverified")

// The operator chooses one of two fixed identities at build time. Jobs cannot
// select a host/container/port through their environment or Cargo arguments.
// Build BOTH host and guest with -ldflags '-X main.normalBuildProfile=burst-3122'.
var normalBuildProfile = "primary"

type normalProfile struct {
	container, image, listen, runner, label, targetDir string
	ephemeral                                          bool
	maxCycles                                          int
}

func normalProfileFor(name string) (normalProfile, error) {
	switch name {
	case "primary":
		return normalProfile{container: "todo2510-normal-linux", image: "todo2510-normal-linux:local", listen: "127.0.0.1:17852", runner: "cc-pc-tools-linux", label: normalLabel, targetDir: "target"}, nil
	case "burst-3122":
		return normalProfile{container: "todo3122-linux-capacity", image: "todo3122-linux-capacity:local", listen: "127.0.0.1:17862", runner: "cc-pc-tools-linux-3122", label: normalLabel, targetDir: "target-burst-3122", ephemeral: true, maxCycles: 1}, nil
	default:
		return normalProfile{}, fmt.Errorf("unsupported compiled normal runner profile %q", name)
	}
}

var compiledNormalProfile = mustNormalProfile(normalBuildProfile)

func validRegistration(p normalProfile, name, label, ephemeral string) bool {
	if label != p.label {
		return false
	}
	if p.ephemeral {
		return name == p.runner && ephemeral == "true"
	}
	return (name == p.runner && ephemeral == "false") || (name == "cc-pc-tools-linux-2511" && ephemeral == "true")
}

func superviseCycles(limit int, run func() (int, error), retry func(time.Duration)) (int, error) {
	for cycle := 1; ; cycle++ {
		code, err := run()
		if err != nil || (limit > 0 && cycle >= limit) {
			return code, err
		}
		if code != 0 {
			retry(30 * time.Second)
		}
	}
}

func mustNormalProfile(name string) normalProfile {
	p, err := normalProfileFor(name)
	if err != nil {
		panic(err)
	}
	return p
}
