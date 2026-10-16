package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

type config struct{ Browser, SHA256, LibraryPath string }

func main() {
	exe, e := os.Executable()
	if e != nil {
		os.Exit(120)
	}
	b, e := os.ReadFile(filepath.Join(filepath.Dir(exe), "chrome-sdk.json"))
	if e != nil {
		os.Exit(121)
	}
	var c config
	if json.Unmarshal(b, &c) != nil || !filepath.IsAbs(c.Browser) {
		os.Exit(122)
	}
	f, e := os.Open(c.Browser)
	if e != nil {
		os.Exit(123)
	}
	h := sha256.New()
	_, e = io.Copy(h, f)
	f.Close()
	if e != nil || hex.EncodeToString(h.Sum(nil)) != c.SHA256 {
		os.Exit(124)
	}
	args := []string{c.Browser, "--disable-gpu", "--disable-software-rasterizer", "--no-sandbox", "--disable-dev-shm-usage"}
	args = append(args, os.Args[1:]...)
	os.Exit(launch(c, args))
}
