package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Pinned is a fixed absolute executable path with the SHA-256 of its bytes.
// The binaries are unsigned local builds, so the digest is the identity.
type Pinned struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func absClean(p string) bool {
	return p != "" && filepath.IsAbs(p) && filepath.Clean(p) == p && !strings.ContainsAny(p, "\x00\r\n")
}

func (p Pinned) validate() error {
	if !absClean(p.Path) || !hex64.MatchString(p.SHA256) {
		return errors.New("fixed absolute executable path and SHA-256 pin required")
	}
	return nil
}

// Verify hashes the bytes now on disk. The pin is rechecked before every
// start, so a replaced binary is rejected rather than trusted by path alone.
func (p Pinned) Verify() error {
	if e := p.validate(); e != nil {
		return e
	}
	f, e := os.Open(p.Path)
	if e != nil {
		return errors.New("pinned executable unavailable")
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > 512<<20 {
		return errors.New("pinned executable is not a bounded regular file")
	}
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return errors.New("pinned executable unreadable")
	}
	if hex.EncodeToString(h.Sum(nil)) != p.SHA256 {
		return errors.New("pinned executable bytes differ from the SHA-256 pin")
	}
	return nil
}

// HelperConfig is the operating setting of one OS helper. The runner pin is
// only meaningful for the Windows helper.
type HelperConfig struct {
	Exe            Pinned `json:"exe"`
	Seconds        int    `json:"seconds"`
	CleanupSeconds int    `json:"cleanup_seconds,omitempty"`
	RunnerVersion  string `json:"runner_version,omitempty"`
	RunnerDigest   string `json:"runner_digest,omitempty"`
	ControllerSID  string `json:"controller_sid,omitempty"`
}

// Config is the deployed operating configuration. Nothing in it names a
// repository, command, callback or URL; those are compile-time constants.
type Config struct {
	Version      int                     `json:"version"`
	Home         string                  `json:"home"`
	GH           Pinned                  `json:"gh"`
	SafeDelete   Pinned                  `json:"safe_delete"`
	Helpers      map[string]HelperConfig `json:"helpers"`
	TickSeconds  int                     `json:"tick_seconds,omitempty"`
	Continuation struct {
		Enabled        bool   `json:"enabled"`
		ReviewedCommit string `json:"reviewed_commit,omitempty"`
	} `json:"continuation"`
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var sidPattern = regexp.MustCompile(`^S-1-5-21-[0-9]+-[0-9]+-[0-9]+-[0-9]+$`)
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func (c Config) validate() error {
	if c.Version != 1 || !absClean(c.Home) {
		return errors.New("queue configuration scope invalid")
	}
	if c.GH.validate() != nil || c.SafeDelete.validate() != nil {
		return errors.New("GitHub reader and safe-delete pins required")
	}
	if c.TickSeconds != 0 && (c.TickSeconds < 5 || c.TickSeconds > 40) {
		return errors.New("tick seconds must be 5..40")
	}
	if c.Continuation.Enabled && !shaPattern.MatchString(c.Continuation.ReviewedCommit) {
		return errors.New("continuation requires the reviewed source commit")
	}
	if len(c.Helpers) > 2 {
		return errors.New("only the Linux and Windows helpers exist")
	}
	for name, h := range c.Helpers {
		if name != "Linux" && name != "Windows" {
			return errors.New("unknown helper OS")
		}
		if h.Exe.validate() != nil || h.Seconds < 60 || h.Seconds > 14400 || (h.CleanupSeconds != 0 && (h.CleanupSeconds < 60 || h.CleanupSeconds > 14400)) {
			return errors.New("helper pin or deadline invalid")
		}
		if name == "Windows" && (!sidPattern.MatchString(h.ControllerSID) || !versionPattern.MatchString(h.RunnerVersion) || !hex64.MatchString(h.RunnerDigest)) {
			return errors.New("Windows helper requires controller SID and official runner pin")
		}
	}
	return nil
}

// LoadConfig is strict: unknown fields, trailing data and oversized files are
// rejected so a stray key can never become behaviour.
func LoadConfig(path string) (Config, error) {
	var c Config
	f, e := os.Open(path)
	if e != nil {
		return c, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 64<<10+1))
	if e != nil || len(b) > 64<<10 {
		return c, errors.New("configuration unreadable or too large")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF {
		return c, errors.New("configuration rejected")
	}
	return c, c.validate()
}
