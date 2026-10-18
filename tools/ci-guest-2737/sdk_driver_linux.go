package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Only called on a source-free, unconfigured SDK guest. Hash the public driver
// artifact to distinguish a damaged SDK from workflow/source-specific effects.
func probeSDKDriver(ctx context.Context) []sdkArtifactProbe {
	paths, e := filepath.Glob("/home/ci2737/.rustup/toolchains/1.97.1-x86_64-unknown-linux-gnu/lib/librustc_driver-*.so")
	if e != nil || len(paths) > 4 {
		return nil
	}
	var result []sdkArtifactProbe
	for _, path := range paths {
		if ctx.Err() != nil {
			break
		}
		name := filepath.Base(path)
		if !regexp.MustCompile(`^librustc_driver-[a-f0-9]+\.so$`).MatchString(name) {
			continue
		}
		r := sdkArtifactProbe{Name: name}
		info, e := os.Lstat(path)
		if e == nil && info.Mode().IsRegular() && info.Size() <= 1<<30 {
			f, e := os.Open(path)
			if e == nil {
				opened, se := f.Stat()
				if se == nil && os.SameFile(info, opened) {
					h := sha256.New()
					n, re := io.Copy(h, io.LimitReader(f, (1<<30)+1))
					r.Bytes = n
					if re == nil && n == info.Size() {
						r.SHA256 = hex.EncodeToString(h.Sum(nil))
						r.ReadComplete = true
					}
				}
				_ = f.Close()
			}
		}
		result = append(result, r)
	}
	return result
}
