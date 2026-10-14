//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

func replaceFile(source, destination string) error { return os.Rename(source, destination) }

func acquireMutex(string) (func(), bool, error) {
	return nil, false, errors.New("windows_required")
}

func productionServices() []service { return nil }

type nativeBackend struct{}

func connectionRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }

func (nativeBackend) Probe(ctx context.Context, target service) probeResult {
	return probeHTTP(ctx, target, 3*time.Second)
}
func (nativeBackend) Task(context.Context, service) (taskResult, error) {
	return taskResult{}, errors.New("windows_required")
}
func (nativeBackend) Start(context.Context, service) error { return errors.New("windows_required") }

func inspectSession(context.Context, string) (sessionReport, error) {
	return sessionReport{}, errors.New("windows_required")
}

func reduceS4UPrivileges(bool, bool) (bool, int, error) { return false, 0, nil }

func launchKnownService(string) error { return errors.New("windows_required") }
