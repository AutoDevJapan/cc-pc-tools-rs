package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const programReplayGo = "C:/Program Files/Go/bin/go.exe"

func programReplayEnv(root string) []string {
	// No inherited GOFLAGS, GOWORK, GOPATH, GOENV, proxy credentials or local
	// replacements. Module archives are fetched into a fresh owned directory.
	var env []string
	for _, key := range []string{"SystemRoot", "WINDIR", "COMSPEC"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	return append(env, "PATH=C:/Windows/System32;C:/Windows", "GOENV=off", "GOAUTH=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOFLAGS=", "CGO_ENABLED=0", "GOPRIVATE=", "GONOPROXY=", "GONOSUMDB=", "GOSUMDB=sum.golang.org", "GOPROXY=https://proxy.golang.org", "GOMAXPROCS=4", "GOPATH="+filepath.Join(root, "gopath"), "GOMODCACHE="+filepath.Join(root, "modules"), "GOCACHE="+filepath.Join(root, "cache"), "TEMP="+filepath.Join(root, "tmp"), "TMP="+filepath.Join(root, "tmp"))
}

func programReplayRun(ctx context.Context, root, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	hide(cmd)
	cmd.Dir = root
	cmd.Env = programReplayEnv(root)
	cmd.WaitDelay = time.Second
	var out, errout limitedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &errout
	if cmd.Run() != nil || out.Overflow || errout.Overflow {
		return nil, errConversationMedia
	}
	return []byte(out.String()), nil
}

func programReplayBuild(ctx context.Context, root string, e programQuestionEvidence, source, module []byte, run func(context.Context, string, string, ...string) ([]byte, error)) ([]programReplayModule, string, error) {
	if !programModule(module) || bytesHash(source) != e.ProgramSHA {
		return nil, "", errConversationMedia
	}
	for _, dir := range []string{"tmp", "out", "cache", "modules", "gopath"} {
		if os.Mkdir(filepath.Join(root, dir), 0700) != nil {
			return nil, "", errConversationMedia
		}
	}
	// Write exactly the bytes already checked against the original native add.
	for name, b := range map[string][]byte{filepath.Base(e.Program): source, "go.mod": module} {
		f, err := os.OpenFile(filepath.Join(root, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, "", errConversationMedia
		}
		_, werr := f.Write(b)
		cerr := f.Close()
		if werr != nil || cerr != nil {
			return nil, "", errConversationMedia
		}
	}
	if _, err := run(ctx, root, programReplayGo, "mod", "tidy"); err != nil {
		return nil, "", errConversationMedia
	}
	raw, err := run(ctx, root, programReplayGo, "mod", "download", "-json", "all")
	if err != nil {
		return nil, "", errConversationMedia
	}
	modules, err := programReplayDecodeModules(raw)
	if err != nil {
		return nil, "", errConversationMedia
	}
	if _, err = run(ctx, root, programReplayGo, "mod", "verify"); err != nil {
		return nil, "", errConversationMedia
	}
	binary := filepath.Join(root, "drawing.exe")
	if _, err = run(ctx, root, programReplayGo, "build", "-p", "4", "-mod=readonly", "-trimpath", "-o", binary, filepath.Base(e.Program)); err != nil {
		return nil, "", errConversationMedia
	}
	if _, err = run(ctx, root, binary, filepath.Join(root, "out")); err != nil {
		return nil, "", errConversationMedia
	}
	files, err := os.ReadDir(filepath.Join(root, "out"))
	if err != nil || len(files) != len(e.Images) {
		return nil, "", errConversationMedia
	}
	for _, image := range e.Images {
		b, _, err := conversationMediaRead(filepath.Join(root, "out", filepath.Base(image.Path)), conversationMediaLimit)
		if err != nil || int64(len(b)) != image.Size || bytesHash(b) != image.SHA {
			return nil, "", errConversationMedia
		}
	}
	sums, _, err := conversationMediaRead(filepath.Join(root, "go.sum"), 128<<10)
	if err != nil {
		return nil, "", errConversationMedia
	}
	return modules, bytesHash(sums), nil
}

func programReplayProduce(ctx context.Context, e programQuestionEvidence, source, module []byte) ([]programReplayModule, string, *programReplayOwned, error) {
	raw, err := runCommand(ctx, "D:/CC/Tools/safe-delete/safe-delete.exe", "create", "--project", "cc-pc-tools-rs", "--content", "program-question-replay")
	var owned programReplayOwned
	if err != nil || json.Unmarshal([]byte(raw), &owned) != nil || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(owned.ID) || !generatedWithin(os.TempDir(), owned.Path) || !strings.HasPrefix(filepath.Base(owned.Path), "codex-owned-") {
		return nil, "", nil, errConversationMedia
	}
	physical, err := filepath.EvalSymlinks(owned.Path)
	if err != nil {
		return nil, "", &owned, errConversationMedia
	}
	modules, sum, err := programReplayBuild(ctx, physical, e, source, module, programReplayRun)
	// Keep ownership on success and failure. The caller uses the existing
	// safe-delete plan/ordinary deletion/finish protocol in separate operations.
	return modules, sum, &owned, err
}
