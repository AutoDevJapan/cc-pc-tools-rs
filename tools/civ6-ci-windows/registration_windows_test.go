//go:build windows && amd64

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRegistrationCredentialChild(t *testing.T) {
	mode := os.Getenv("CIW_TEST_REGISTRATION")
	if mode == "" {
		return
	}
	fmt.Fprint(os.Stderr, "synthetic-credential-must-not-be-in-errors")
	switch mode {
	case "valid":
		fmt.Print("synthetic-registration-value\n")
	case "invalid":
		fmt.Print("invalid token\n")
	case "oversize":
		fmt.Print(strings.Repeat("x", 8192))
	case "failure":
		fmt.Print("synthetic-credential-must-not-be-in-errors")
		os.Exit(1)
	}
	os.Exit(0)
}

func TestRegistrationCredentialBoundedAndPrivate(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"valid", "invalid", "oversize", "failure", "expired"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if mode == "expired" {
				cancel()
			}
			calls := 0
			b, err := requestRegistrationCredential(ctx, cancel, func(ctx context.Context) *exec.Cmd {
				calls++
				c := exec.CommandContext(ctx, exe, "-test.run=^TestRegistrationCredentialChild$")
				hidden(c)
				c.Env = append(controllerGateEnv(), "CIW_TEST_REGISTRATION="+mode)
				return c
			})
			defer clear(b)
			if mode == "valid" {
				if err != nil || string(b) != "synthetic-registration-value" {
					t.Fatal("valid credential not returned")
				}
				return
			}
			if err == nil || len(b) != 0 || strings.Contains(err.Error(), "synthetic-credential") {
				t.Fatal("credential failure leaked or accepted")
			}
			if (mode == "expired" && calls != 0) || (mode != "expired" && calls != 1) {
				t.Fatal("unexpected retry or expired call")
			}
		})
	}
}
