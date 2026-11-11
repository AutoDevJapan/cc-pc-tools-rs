//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestOwnerExplicitRefreshSingleflightChild(t *testing.T) {
	home := os.Getenv("F4431_REFRESH_FIXTURE")
	if home == "" {
		return
	}
	index := os.Getenv("F4431_REFRESH_INDEX")
	repo := os.Getenv("F4431_REFRESH_REPO")
	if err := os.WriteFile(filepath.Join(home, "ready"+index), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(6 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(home, "go")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("start barrier")
		}
		time.Sleep(2 * time.Millisecond)
	}
	fetch := func(ctx context.Context, r string) (int64, string, string, bool) {
		f, e := os.OpenFile(filepath.Join(home, "fetch"+index), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		f.Close()
		if os.Getenv("F4431_REFRESH_FORCE_BOTH") == "1" {
			for {
				a, _ := os.Stat(filepath.Join(home, "fetch0"))
				b, _ := os.Stat(filepath.Join(home, "fetch1"))
				if a != nil && b != nil {
					break
				}
				select {
				case <-ctx.Done():
					return 0, "", "", false
				case <-time.After(2 * time.Millisecond):
				}
			}
		} else {
			time.Sleep(150 * time.Millisecond)
		}
		if os.Getenv("F4431_REFRESH_FAIL") == "1" {
			return 0, "", "", false
		}
		return 12345, r, strings.Split(r, "/")[0], true
	}
	query := func(ctx context.Context, _ string, a ownerSourceAccess) ownerPathVerdict {
		a.identity(repo)
		return ownerUnverified()
	}
	ok := ownerRefreshIdentityRequestWith("fixture", "", home, time.Now, query, fetch)
	want := os.Getenv("F4431_REFRESH_FAIL") != "1"
	if ok != want {
		t.Fatalf("result %v want %v", ok, want)
	}
}
func TestOwnerExplicitRefreshSingleflightTwoProcesses(t *testing.T) {
	home, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	children := []*exec.Cmd{}
	for i := 0; i < 2; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestOwnerExplicitRefreshSingleflightChild$")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		childRepo := "AutoDevJapan/singleflight-fixture"
		if os.Getenv("F4431_TEST_SEPARATE_REPO") == "1" && i == 1 {
			childRepo = "AutoDevJapan/other-singleflight-fixture"
		}
		cmd.Env = append(os.Environ(), "F4431_REFRESH_FIXTURE="+home, fmt.Sprint("F4431_REFRESH_INDEX=", i), "F4431_REFRESH_REPO="+childRepo, "F4431_REFRESH_FORCE_BOTH="+os.Getenv("F4431_TEST_FORCE_BOTH"))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, cmd)
	}
	defer func() {
		for _, c := range children {
			if c.ProcessState == nil {
				c.Process.Kill()
				c.Wait()
			}
		}
	}()
	deadline := time.Now().Add(6 * time.Second)
	for {
		_, a := os.Stat(filepath.Join(home, "ready0"))
		_, b := os.Stat(filepath.Join(home, "ready1"))
		if a == nil && b == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("readiness")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err := os.WriteFile(filepath.Join(home, "go"), []byte("go"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, c := range children {
		if err := c.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	matches, _ := filepath.Glob(filepath.Join(home, "fetch*"))
	wantFetches := 1
	if os.Getenv("F4431_TEST_SEPARATE_REPO") == "1" {
		wantFetches = 2
	}
	if len(matches) != wantFetches {
		t.Fatalf("same canonical repository fetched %d times; want 1", len(matches))
	}
}

func TestOwnerExplicitRefreshFailureIsShared(t *testing.T) {
	t.Setenv("F4431_REFRESH_FAIL", "1")
	t.Setenv("F4431_TEST_SEPARATE_REPO", "")
	TestOwnerExplicitRefreshSingleflightTwoProcesses(t)
}
func TestOwnerExplicitRefreshDifferentRepositoriesStaySeparate(t *testing.T) {
	t.Setenv("F4431_REFRESH_FAIL", "")
	t.Setenv("F4431_TEST_SEPARATE_REPO", "1")
	TestOwnerExplicitRefreshSingleflightTwoProcesses(t)
}
