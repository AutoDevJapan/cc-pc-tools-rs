//go:build windows

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func launchKnownService(id string) error {
	if !validServiceID(id) {
		return errors.New("service_id_invalid")
	}
	release, acquired, err := acquireMutex("Global\\CC-ResidentGuard-Launch-" + id)
	if err != nil {
		return errors.New("service_launch_mutex_unavailable")
	}
	if !acquired {
		return nil
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	user, err := os.UserHomeDir()
	if err != nil {
		return errors.New("service_profile_unavailable")
	}
	executable := systemFile("wscript.exe")
	arguments := []string{}
	working := ""
	switch id {
	case "operations":
		// Keep this task attached to the server for its whole lifetime. The
		// original VBS uses an asynchronous Run, which leaves no action to
		// supervise once the launcher returns.
		executable = "D:/CC/Tools/localai-web-rs/target/release/localai-web-rs.exe"
		working = "D:/CC/Tools/localai-web-rs/target/release"
		arguments = nil
	case "opencodex":
		arguments = []string{"/b", "/nologo", filepath.Join(user, ".opencodex", "opencodex-service-launcher.vbs")}
	case "godd_ao":
		arguments = []string{"/b", "/nologo", filepath.Join(user, ".opencodex", "godd-ao", "semantic-proxy-launcher.vbs")}
	case "opencodex_watchdog":
		executable = "D:/CC/Tools/ocx-health-watchdog/ocx_health_watchdog.exe"
		arguments = []string{"watch"}
		working = "D:/CC/Tools/ocx-health-watchdog"
	}
	if id != "opencodex_watchdog" {
		var target service
		for _, definition := range productionServices() {
			if definition.ID == id {
				target = definition
				break
			}
		}
		observed := (nativeBackend{}).Probe(ctx, target)
		if observed.Healthy || observed.Port != "closed" {
			return nil
		}
		if target.Dependency != "" {
			var dependency service
			for _, definition := range productionServices() {
				if definition.ID == target.Dependency {
					dependency = definition
					break
				}
			}
			for !(nativeBackend{}).Probe(ctx, dependency).Healthy {
				select {
				case <-ctx.Done():
					return errors.New("service_dependency_deadline")
				case <-time.After(time.Second):
				}
			}
		}
		// Repeat the listener check immediately before launching. Another
		// healthy/starting instance is retained rather than force-replaced.
		if (nativeBackend{}).Probe(ctx, target).Port != "closed" {
			return nil
		}
	}
	// Persistent wrappers retain the existing service lifetime; they add no
	// restart loop. The finite startup checks above precede this one launch.
	command := exec.Command(executable, arguments...)
	command.Dir = working
	return runServiceProcess(command)
}

func runServiceProcess(command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Run(); err != nil {
		return errors.New("service_process_failed")
	}
	return nil
}
