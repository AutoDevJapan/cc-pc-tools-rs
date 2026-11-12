//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnedCargoTwoLinksSameObjectHeldBeforeConsume(t *testing.T) {
	root := physicalSoftwareFixture(t)
	a := filepath.Join(root, "out.exe")
	b := filepath.Join(root, "dependency.exe")
	if e := os.WriteFile(a, []byte("owned compiled fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Link(a, b); e != nil {
		t.Fatal(e)
	}
	calls := 0
	if !withPinnedCargoOutput(a, b, softwareIdentity{}, func(raw []byte, id softwareIdentity) bool {
		calls++
		if !bytes.Equal(raw, []byte("owned compiled fixture")) || id.FileID == "" {
			t.Fatal("wrong pinned object")
		}
		for _, p := range []string{a, b} {
			if os.WriteFile(p, []byte("mutate"), 0600) == nil {
				t.Fatal("alias writer not denied")
			}
			if os.Rename(p, p+".moved") == nil {
				t.Fatal("alias rename not denied")
			}
		}
		return true
	}) || calls != 1 {
		t.Fatal("owned same producer output rejected")
	}
	if e := os.WriteFile(a, []byte("after release"), 0600); e != nil {
		t.Fatal("lease leaked")
	}
}

func TestCargoLinkWrongIdentityExtraAliasAndMissingNeverRead(t *testing.T) {
	for _, fault := range []string{"wrong_id", "extra", "missing", "different"} {
		t.Run(fault, func(t *testing.T) {
			root := physicalSoftwareFixture(t)
			a := filepath.Join(root, "out.exe")
			b := filepath.Join(root, "alias.exe")
			if e := os.WriteFile(a, []byte("owned"), 0600); e != nil {
				t.Fatal(e)
			}
			if e := os.Link(a, b); e != nil {
				t.Fatal(e)
			}
			expected := softwareIdentity{}
			switch fault {
			case "wrong_id":
				expected = softwareIdentity{Volume: "1", FileID: "other"}
			case "extra":
				if e := os.Link(a, a+".third"); e != nil {
					t.Fatal(e)
				}
			case "missing":
				b += "missing"
			case "different":
				if e := os.Remove(b); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(b, []byte("other"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if withPinnedCargoOutput(a, b, expected, func([]byte, softwareIdentity) bool { t.Fatal("invalid build output consumed"); return true }) {
				t.Fatal("invalid link set admitted")
			}
		})
	}
}

func TestAskArtifactCopyCreateNewAndSameHeldReadback(t *testing.T) {
	root := physicalSoftwareFixture(t)
	path := filepath.Join(root, "candidate.exe")
	raw := []byte("owned software artifact")
	id, e := createAskArtifactCopy(path, raw)
	if e != nil || id.FileID == "" {
		t.Fatal(e, id)
	}
	if _, e = createAskArtifactCopy(path, []byte("overwrite")); e == nil {
		t.Fatal("existing candidate changed")
	}
	got, e := os.ReadFile(path)
	if e != nil || !bytes.Equal(got, raw) {
		t.Fatal("readback mismatch")
	}
	var out bytes.Buffer
	if runAskBuildArtifact([]string{"--export-ask-cargo-output", "other"}, &out) != 2 || out.Len() != 0 {
		t.Fatal("caller changed fixed purpose")
	}
}

func TestProducerReceiptBindsOutputAndExportWithoutOverwrite(t *testing.T) {
	root := physicalSoftwareFixture(t)
	path := filepath.Join(root, "receipt.json")
	r := askArtifactResult{Source: askBuildSource, ProducerExit: 0, ProducerReaped: true, Bytes: 16, SHA: "owned-digest", OutputIdentity: &softwareIdentity{"1", "2", "3"}, Identity: &softwareIdentity{"1", "4", "5"}, ExportCreated: true}
	if e := writeAskBuildReceipt(path, r); e != nil {
		t.Fatal(e)
	}
	if e := writeAskBuildReceipt(path, r); e == nil {
		t.Fatal("producer receipt overwritten")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var got struct {
		Result   askArtifactResult
		ProofSHA string `json:"source_proof_sha256"`
		Inputs   int    `json:"source_inputs"`
	}
	if json.Unmarshal(b, &got) != nil || got.Result.SHA != r.SHA || *got.Result.OutputIdentity != *r.OutputIdentity || *got.Result.Identity != *r.Identity || !got.Result.ProducerReaped || got.Inputs != 207 || got.ProofSHA != askBuildProofSHA {
		t.Fatal("producer/output/receipt binding lost")
	}
}

func TestReceiptFailureOrExpiryBeforePublishPreservesPendingNotSuccess(t *testing.T) {
	for _, fault := range []string{"sync_fail", "after_sync_fail", "expiry", "collision", "normal"} {
		t.Run(fault, func(t *testing.T) {
			root := physicalSoftwareFixture(t)
			p := filepath.Join(root, "receipt.json")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := askArtifactResult{Qualified: true, Reason: "fixed_software_exported", Source: askBuildSource}
			if fault == "collision" {
				if e := os.WriteFile(p, []byte("existing"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			hook := func(step string) error {
				if fault == "sync_fail" && step == "before_sync" || fault == "after_sync_fail" && step == "after_sync" {
					return errors.New("owned IO fault")
				}
				if fault == "expiry" && step == "after_sync" {
					cancel()
				}
				return nil
			}
			err := writeAskBuildReceiptContext(ctx, p, r, hook)
			if fault == "normal" {
				if err != nil {
					t.Fatal(err)
				}
				if _, e := os.Stat(p); e != nil {
					t.Fatal(e)
				}
				return
			}
			if err == nil {
				t.Fatal("fault published success")
			}
			if fault == "collision" {
				if b, e := os.ReadFile(p); e != nil || string(b) != "existing" {
					t.Fatal("existing receipt clobbered")
				}
			} else if _, e := os.Stat(p); !os.IsNotExist(e) {
				t.Fatal("failed/expired success marker exists")
			}
			if _, e := os.Stat(p + ".pending"); e != nil {
				t.Fatal("pending recovery bytes lost")
			}
		})
	}
}
