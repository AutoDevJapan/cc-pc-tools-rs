package main

import (
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/pressci"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

func takeNormalCILock(path string) (*os.File, error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	var over syscall.Overlapped
	if ok, _, _ := lockFileProc.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&over))); ok == 0 {
		_ = f.Close()
		return nil, errors.New("another normal CI job owns admission")
	}
	return f, nil
}

// This gate-owned path boots only the immutable, source-free SDK. It never
// fetches a product, requests a GitHub token or starts/registers a runner.
func executePublicSDKProbe(ctx context.Context, root, sdkRoot, helper string, jobs int) (ret error) {
	return executeSDKProbeFor(ctx, root, sdkRoot, helper, jobs, fixedSourcePin())
}
func pressSDKProbePin() sourcepacket.Pin {
	return sourcepacket.Pin{Product: sourcepacket.PressProduct, Workflow: "local-checks.yml", HeadSHA: pressci.Head, TreeSHA: pressci.Tree, WorkflowSHA256: "935f455d7f7ecdf225f39a2a335a3d3a0ccc6d832d9a1c70fcf46e8906c9aa62"}
}
func executeSDKProbeFor(ctx context.Context, root, sdkRoot, helper string, jobs int, pin sourcepacket.Pin) (ret error) {
	lock, e := takeNormalCILock(`D:\CC\Tools\product-ci\todo2737-normal-ci.lock`)
	if e != nil {
		return e
	}
	defer lock.Close()
	id := newID()
	attempt := filepath.Join(root, "sdk-probe-"+id)
	if e = os.Mkdir(attempt, 0700); e != nil {
		return e
	}
	intentPath := `D:\CC\Tools\product-ci\todo2737-normal-ci.intent.private.json`
	f, e := os.OpenFile(intentPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("normal CI recovery intent already exists")
	}
	e = json.NewEncoder(f).Encode(map[string]any{"instance": id, "attempt": attempt, "mode": "public-sdk-probe", "product_source": false, "runner_registered": false})
	if e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	if e != nil {
		return e
	}
	var vm vmState
	defer func() {
		gone := false
		if vm.ID != "" {
			clean, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
			defer cancel()
			ce := cleanupVM(clean, vm)
			ret = errors.Join(ret, ce)
			gone = ce == nil
		} else {
			// The constructor writes its durable intent before it can create a VM.
			_, se := os.Lstat(filepath.Join(attempt, "vm-intent.private.json"))
			gone = os.IsNotExist(se)
		}
		ce := record(attempt, "sdk-probe-cleanup.private.json", map[string]any{"vm_gone": gone, "product_source": false, "runner_registered": false, "official_ci_success": false})
		ret = errors.Join(ret, ce)
		if gone && ce == nil {
			ret = errors.Join(ret, os.Remove(intentPath))
		}
		if !gone {
			ret = errors.Join(ret, &unsafeVMLease{State: vm, Root: attempt, Instance: id})
		}
	}()
	var ident identity
	var readyState ready
	vm, ident, readyState, e = bootSDKForSource(ctx, attempt, sdkRoot, helper, id, pin, jobs, 8)
	if e != nil {
		return e
	}
	c, e := guestDial(ident, readyState)
	if e != nil {
		return e
	}
	defer c.Close()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-finished:
		}
	}()
	_ = c.SetDeadline(time.Now().Add(90 * time.Second))
	if pin.Product == sourcepacket.PressProduct {
		_ = c.SetDeadline(time.Now().Add(16 * time.Minute))
	}
	if e = json.NewEncoder(c).Encode(request{"probe-public-sdk", id}); e != nil {
		return e
	}
	var p publicSDKProbe
	limit := int64(32768)
	if pin.Product == sourcepacket.PressProduct {
		limit = 2 << 20
	}
	if e = json.NewDecoder(io.LimitReader(c, limit)).Decode(&p); e != nil {
		return errors.New("public SDK probe frame unavailable")
	}
	if e = record(attempt, "public-sdk-probe.private.json", p); e != nil {
		return e
	}
	if p.ID != id || p.ProductSource || p.RunnerRegistered || p.Failure != "" || len(p.Rust) != 3 || len(p.Driver) > 4 {
		return errors.New("public SDK probe identity rejected")
	}
	if pin.Product == sourcepacket.PressProduct && (p.PressSDK == nil || p.PressSDK.OS != "ubuntu-24.04" || p.Rust[1].Version != p.PressSDK.Rust) {
		return errors.New("Press SDK runtime receipt rejected")
	}
	return nil
}
