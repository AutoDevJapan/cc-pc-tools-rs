// pc-ci runs the repo-only runner admission hook and the Windows Cargo shim.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const repository = "AutoDevJapan/cc-pc-tools-rs"

type repo struct {
	FullName string `json:"full_name"`
}
type event struct {
	Repository  repo   `json:"repository"`
	Ref         string `json:"ref"`
	After       string `json:"after"`
	PullRequest *struct {
		Head struct {
			Repo repo   `json:"repo"`
			Ref  string `json:"ref"`
			SHA  string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
}

func source(e event, kind, sha, ref string) (string, string, error) {
	if e.Repository.FullName != repository {
		return "", "", fmt.Errorf("runner accepts only its registered repository")
	}
	switch kind {
	case "pull_request":
		if e.PullRequest == nil || e.PullRequest.Head.Repo.FullName != repository {
			return "", "", fmt.Errorf("fork or missing pull request rejected")
		}
		return e.PullRequest.Head.Ref, e.PullRequest.Head.SHA, nil
	case "push":
		if !strings.HasPrefix(e.Ref, "refs/heads/") || e.After != sha {
			return "", "", fmt.Errorf("non-branch or mismatched push rejected")
		}
		return strings.TrimPrefix(e.Ref, "refs/heads/"), sha, nil
	case "workflow_dispatch":
		if !strings.HasPrefix(ref, "refs/heads/") {
			return "", "", fmt.Errorf("dispatch requires a branch")
		}
		return strings.TrimPrefix(ref, "refs/heads/"), sha, nil
	default:
		return "", "", fmt.Errorf("unsupported runner event")
	}
}

func hook() error {
	if os.Getenv("GITHUB_REPOSITORY") != repository {
		return fmt.Errorf("wrong repository")
	}
	f, err := os.Open(os.Getenv("GITHUB_EVENT_PATH"))
	if err != nil {
		return err
	}
	defer f.Close()
	var e event
	if err = json.NewDecoder(f).Decode(&e); err != nil {
		return err
	}
	branch, sha, err := source(e, os.Getenv("GITHUB_EVENT_NAME"), os.Getenv("GITHUB_SHA"), os.Getenv("GITHUB_REF"))
	if err != nil {
		return err
	}
	token := os.Getenv("CI_JOB_TOKEN")
	if token == "" {
		return fmt.Errorf("job-scoped read token is required")
	}
	request, err := http.NewRequest("GET", "https://api.github.com/repos/"+repository+"/git/ref/heads/"+url.PathEscape(branch), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(request)
	if err != nil {
		return fmt.Errorf("branch head check unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("branch head check returned %d", response.StatusCode)
	}
	var head struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err = json.NewDecoder(response.Body).Decode(&head); err != nil {
		return err
	}
	if sha == "" || head.Object.SHA != sha {
		return fmt.Errorf("obsolete branch revision rejected")
	}
	fmt.Printf("Accepted current repository revision %s\n", sha)
	return nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		if err := hook(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	gate := `D:\CC\Tools\codex-jobs\cargo_gate.exe`
	c := exec.Command(gate, os.Args[1:]...)
	hideConsole(c)
	c.Env = os.Environ()
	c.Env = append(c.Env, `CODEX_JOBS_CARGO_LOCK_DIR=D:\CC\Tools\codex-jobs`, `CODEX_JOBS_REAL_CARGO=D:\CC\Tools\rustup\toolchains\stable-x86_64-pc-windows-msvc\bin\cargo.exe`)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Cargo subcommands resolve this shim from the isolated runner PATH.
	if !filepath.IsAbs(gate) {
		panic("absolute gate required")
	}
	if err := c.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			os.Exit(e.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "Cargo gate unavailable")
		os.Exit(1)
	}
}
