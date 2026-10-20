package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsOfflineSeedReplacementPreservesAttributesAndACL(t *testing.T) {
	for _, mode := range []string{"writable", "readonly", "missing-source", "directory"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			helper := filepath.Join(root, "helper")
			if err := os.Mkdir(helper, 0700); err != nil {
				t.Fatal(err)
			}
			seed := filepath.Join(helper, "seed.json")
			old, next := []byte("public-old-fixture"), []byte("public-new-fixture")
			if mode == "directory" {
				if err := os.Mkdir(seed, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(seed, old, 0600); err != nil {
					t.Fatal(err)
				}
				if mode != "writable" {
					if err := os.Chmod(seed, 0400); err != nil {
						t.Fatal(err)
					}
					defer os.Chmod(seed, 0600)
				}
			}
			if mode != "missing-source" {
				if err := os.WriteFile(filepath.Join(root, "seed.json"), next, 0600); err != nil {
					t.Fatal(err)
				}
			}
			want := "$false"
			if mode == "readonly" || mode == "writable" {
				want = "$true"
			}
			// Execute the SAME replacement fragment used inside the offline
			// staging flight, against public files only. No mount/broker/VM.
			script := "$ErrorActionPreference='Stop'\n$root=" + psQuote(root) + "\n$helper=" + psQuote(helper) + "\n" + `
$path=Join-Path $helper 'seed.json'
$attributes=(Get-Item -LiteralPath $path).Attributes
$acl=(Get-Acl -LiteralPath $path).Sddl
$replaced=$false
try {
` + windowsOfflineSeedReplacementScript() + `
 $replaced=$true
} catch {}
if($replaced -ne ` + want + `){exit 42}
if((Get-Item -LiteralPath $path).Attributes -ne $attributes){exit 43}
if((Get-Acl -LiteralPath $path).Sddl -ne $acl){exit 44}
`
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			c := command(ctx, `C:\Program Files\PowerShell\7\pwsh.exe`, "-NoProfile", "-NonInteractive", "-Command", script)
			if err := c.Run(); err != nil {
				t.Fatalf("seed replacement result/attributes/ACL mismatch: %v", err)
			}
			if mode == "directory" {
				entries, err := os.ReadDir(seed)
				if err != nil || len(entries) != 0 {
					t.Fatal("directory accepted or modified")
				}
			} else {
				wantBytes := old
				if mode != "missing-source" {
					wantBytes = next
				}
				got, err := os.ReadFile(seed)
				if err != nil || !bytes.Equal(got, wantBytes) {
					t.Fatal("seed content mismatch")
				}
			}
		})
	}
}
