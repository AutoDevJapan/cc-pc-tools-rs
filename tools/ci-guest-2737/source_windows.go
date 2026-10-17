package main

import (
	"archive/tar"
	"bytes"
	sourcepacket "cc/ci-source-2737"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func inspectSource(ctx context.Context) error {
	p, e := sourcepacket.Fetch(ctx, fixedSourcePin())
	if e != nil {
		return e
	}
	z, e := gzip.NewReader(bytes.NewReader(p.Archive))
	if e != nil {
		return e
	}
	defer z.Close()
	t := tar.NewReader(z)
	for {
		h, e := t.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return errors.New("source guidance unavailable")
		}
		_, name, _ := strings.Cut(h.Name, "/")
		if name != "AGENTS.md" && name != "CLAUDE.md" {
			continue
		}
		b, e := io.ReadAll(io.LimitReader(t, 4<<20))
		if e != nil || int64(len(b)) != h.Size {
			return errors.New("source guidance exceeds bound")
		}
		fmt.Printf("GUIDANCE %s sha256=%s bytes=%d\n", path.Base(name), hash(b), len(b))
		lines := strings.Split(string(b), "\n")
		selected := make(map[int]bool)
		for n, line := range lines {
			low := strings.ToLower(line)
			for _, key := range []string{"ci", "cargo", "review", "test", "secret", "credential", "runner", "timeout", "workflow", "merge", "must"} {
				if strings.Contains(low, key) {
					for k := max(0, n-1); k <= min(len(lines)-1, n+1); k++ {
						selected[k] = true
					}
					break
				}
			}
		}
		for n, line := range lines {
			if selected[n] {
				fmt.Println(line)
			}
		}
	}
	return nil
}

func sourceSmoke(ctx context.Context, root, helper string) (ret error) {
	p, e := sourcepacket.Fetch(ctx, fixedSourcePin())
	if e != nil {
		return errors.New("fixed source fetch failed")
	}
	raw, e := sourcepacket.Encode(p)
	if e != nil {
		return e
	}
	var template struct {
		Path, SHA256, Instance string
		VMID                   string `json:"source_vm"`
		VMRemoved              bool   `json:"vm_removed"`
		ProductSource          bool   `json:"product_source"`
		RunnerRegistered       bool   `json:"runner_registered"`
	}
	b, e := os.ReadFile(filepath.Join(root, "sdk-template-ready.private.json"))
	if e != nil || json.Unmarshal(b, &template) != nil || !template.VMRemoved || template.ProductSource || template.RunnerRegistered {
		return errors.New("SDK template receipt rejected")
	}
	rel, e := filepath.Rel(root, template.Path)
	if e != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.Base(template.Path) != "guest.vhdx" || !validVMID(template.VMID) || !validInstance(template.Instance) {
		return errors.New("SDK template ownership rejected")
	}
	instance := newID()
	attempt := filepath.Join(root, "source-attempt-"+instance)
	if e = os.Mkdir(attempt, 0700); e != nil {
		return e
	}
	state := vmState{Instance: template.Instance, Name: "cc-ci-2737-" + template.Instance, Root: filepath.Dir(template.Path), Pipe: `\\.\pipe\cc-ci-2737-` + template.Instance}
	if e = admin(ctx, attempt, "sdk-source-absence", vmPrelude(state)+"Assert-Absent "+psQuote(template.VMID)+"\n"); e != nil {
		return e
	}
	image, e := os.Open(template.Path)
	if e != nil {
		return e
	}
	h, e := hashReader(image)
	_ = image.Close()
	if e != nil || !strings.EqualFold(h, template.SHA256) {
		return errors.New("SDK template digest rejected")
	}
	i, e := newIdentity()
	if e != nil {
		return e
	}
	if e = prepareSeed(ctx, attempt, root, helper, instance, i); e != nil {
		return e
	}
	s, e := createVM(ctx, attempt, instance, template.Path)
	if s.ID != "" {
		defer func() {
			clean, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			ret = errors.Join(ret, cleanupVM(clean, s))
		}()
	}
	if e != nil {
		return e
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	body := "& " + psQuote(exe) + " serial " + psQuote(s.Pipe) + " " + psQuote(instance) + " " + psQuote(filepath.Join(attempt, "guest-ready.private.json")) + "\nif($LASTEXITCODE -ne 0){throw 'SERIAL_PROBE_FAILED'}\n"
	if e = admin(ctx, attempt, "vm-readiness", body); e != nil {
		return e
	}
	b, e = os.ReadFile(filepath.Join(attempt, "guest-ready.private.json"))
	var r ready
	if e != nil || json.Unmarshal(b, &r) != nil || validateReady(r, instance) != nil {
		return errors.New("source guest readiness rejected")
	}
	c, e := guestDial(i, r)
	if e != nil {
		return e
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Minute))
	if e = json.NewEncoder(c).Encode(request{"materialize-source", instance}); e != nil {
		return e
	}
	if e = writeBlob(c, raw); e != nil {
		return errors.New("source packet transfer failed")
	}
	var result sourceResult
	if e = json.NewDecoder(io.LimitReader(c, 8192)).Decode(&result); e != nil {
		return errors.New("source proof unavailable")
	}
	if result.ID != instance || result.OfficialCI {
		return errors.New("source proof identity rejected")
	}
	if e = record(attempt, "source-proof.private.json", result); e != nil {
		return e
	}
	if result.Failure != "" || result.Proof.HeadSHA != fixedSourcePin().HeadSHA || result.Proof.TreeSHA != fixedSourcePin().TreeSHA || !result.Proof.PhysicalBlobsVerified {
		return errors.New("source physical identity unverified")
	}
	// Recheck the same fixed repository/ref after the guest operation. Source
	// remains in RAM only on the host and is never printed or saved here.
	_, e = sourcepacket.Fetch(ctx, fixedSourcePin())
	return e
}
