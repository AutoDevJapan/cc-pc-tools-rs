//go:build !windows

package main

import (
	"context"
	"errors"
)

func fileBusy(error) bool { return false }

func runBroker(b broker) error                 { return errors.New("CC-AdminBroker requires Windows") }
func startTask(context.Context) error          { return errors.New("CC-AdminBroker requires Windows") }
func runScript(context.Context, string) result { return result{Exit: 125, Stderr: "Windows required"} }
