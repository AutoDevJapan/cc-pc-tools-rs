package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"time"
)

const installedLauncher = "D:/CC/Tools/codex-progress-tui/codex-launch.exe"
const maxProtocolFrame = 2 << 20

var errInputSource = errors.New("input source unavailable")

type protocolFrame struct {
	ID     *int64          `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type frameResult struct {
	frame protocolFrame
	err   error
}

// The decoder's lifetime ends with its connection, including a caller timeout.
func decodeInputFrames(ctx context.Context, in io.Reader, results chan<- frameResult) {
	defer close(results)
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), maxProtocolFrame)
	for scanner.Scan() {
		var frame protocolFrame
		err := json.Unmarshal(scanner.Bytes(), &frame)
		if err != nil {
			err = errInputSource
		}
		select {
		case results <- frameResult{frame, err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
	if scanner.Err() != nil {
		select {
		case results <- frameResult{err: errInputSource}:
		case <-ctx.Done():
		}
	}
}

type appServerRPC struct {
	ctx         context.Context
	cancel      context.CancelFunc
	in          io.WriteCloser
	frames      <-chan frameResult
	child       inputChild
	output      io.ReadCloser
	decoderDone <-chan struct{}
	nextID      int64
	closed      bool
}

func (r *appServerRPC) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if r.closed || ctx.Err() != nil || r.ctx.Err() != nil {
		return nil, errInputSource
	}
	switch method {
	case "initialize", "thread/read", "thread/turns/list", "thread/items/list", "thread/items/read":
	default:
		return nil, errInputSource
	}
	r.nextID++
	request := struct {
		ID     int64  `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{r.nextID, method, params}
	if json.NewEncoder(r.in).Encode(request) != nil {
		return nil, errInputSource
	}
	for notifications := 0; notifications <= 16; notifications++ {
		select {
		case <-ctx.Done():
			return nil, errInputSource
		case <-r.ctx.Done():
			return nil, errInputSource
		case result, ok := <-r.frames:
			if !ok || result.err != nil {
				return nil, errInputSource
			}
			frame := result.frame
			if frame.ID == nil && frame.Method != "" {
				continue
			}
			if frame.ID == nil || *frame.ID != r.nextID || len(frame.Error) != 0 && string(frame.Error) != "null" || len(frame.Result) == 0 || string(frame.Result) == "null" {
				return nil, errInputSource
			}
			if ctx.Err() != nil || r.ctx.Err() != nil {
				return nil, errInputSource
			}
			return frame.Result, nil
		}
	}
	return nil, errInputSource
}

func (r *appServerRPC) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pinErr := r.child.PinMembers()
	_ = r.in.Close()
	grace, endGrace := context.WithTimeout(ctx, time.Second)
	if r.ctx.Err() != nil {
		endGrace()
	}
	err := r.child.Wait(grace)
	endGrace()
	if err != nil {
		_ = r.child.Stop(ctx)
	}
	if pinErr != nil {
		err = errInputSource
	}
	r.cancel()
	_ = r.output.Close()
	select {
	case <-r.decoderDone:
	case <-ctx.Done():
		err = errInputSource
	}
	if r.child.Close() != nil {
		err = errInputSource
	}
	return err
}

type inputPaths struct{ Profile, Windows string }

// Values come from the current Windows token and directory APIs, never the
// caller's home, executable search, token, endpoint or API-key variables.
func inputSourceEnvironment(paths inputPaths) []string {
	local := filepath.Join(paths.Profile, "AppData", "Local")
	return []string{
		"SYSTEMROOT=" + paths.Windows, "WINDIR=" + paths.Windows,
		"PATH=" + filepath.Join(paths.Windows, "System32") + string(filepath.ListSeparator) + paths.Windows,
		"USERPROFILE=" + paths.Profile, "HOME=" + paths.Profile,
		"APPDATA=" + filepath.Join(paths.Profile, "AppData", "Roaming"), "LOCALAPPDATA=" + local,
		"TEMP=" + filepath.Join(local, "Temp"), "TMP=" + filepath.Join(local, "Temp"),
		"CODEX_HOME=" + filepath.Join(paths.Profile, ".codex"),
	}
}

func connectInstalledAppServer(ctx context.Context) (inputRPC, error) {
	paths, err := nativeInputPaths()
	if err != nil {
		return nil, errInputSource
	}
	return connectInputCommand(ctx, installedLauncher, []string{"app-server", "--listen", "stdio://"}, inputSourceEnvironment(paths))
}

// A private Go test seam; the CLI never accepts an executable or environment.
func connectInputCommand(ctx context.Context, path string, args, env []string) (inputRPC, error) {
	life, cancel := context.WithCancel(ctx)
	child, err := startInputChild(path, args, env)
	if err != nil {
		cancel()
		return nil, errInputSource
	}
	in, out := child.Stdin(), child.Stdout()
	frames := make(chan frameResult)
	decoderDone := make(chan struct{})
	go func() { defer close(decoderDone); decodeInputFrames(life, out, frames) }()
	rpc := &appServerRPC{ctx: life, cancel: cancel, in: in, frames: frames, child: child, output: out, decoderDone: decoderDone}
	params := map[string]any{"clientInfo": map[string]string{"name": "cc_question_input_reference", "version": "1.0.0"}, "capabilities": map[string]bool{"experimentalApi": true}}
	if _, err := rpc.Call(ctx, "initialize", params); err != nil {
		_ = rpc.Close()
		return nil, errInputSource
	}
	if json.NewEncoder(in).Encode(map[string]any{"method": "initialized", "params": map[string]any{}}) != nil {
		_ = rpc.Close()
		return nil, errInputSource
	}
	return rpc, nil
}

type inputChild interface {
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	PinMembers() error
	Wait(context.Context) error
	Stop(context.Context) error
	Close() error
}
