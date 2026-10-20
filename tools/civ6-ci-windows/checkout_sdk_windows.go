//go:build windows && amd64

package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Official public SDKs only. This never obtains product source or credentials.
// The fixed-tag resolution, full commit, Git blob and public distribution
// checksums are retained before the new fixture service exists.
func prepareCheckoutFixture(p Plan) error {
	if !p.RuntimeFixture || p.Fixture == "" {
		return errors.New("synthetic checkout fixture required")
	}
	var ref struct{ Object struct{ Type, SHA string } }
	if e := api("repos/actions/checkout/git/ref/tags/v4", &ref); e != nil {
		return e
	}
	if ref.Object.Type != "commit" || !validSHA(ref.Object.SHA) {
		return errors.New("checkout v4 commit unavailable")
	}
	commit := ref.Object.SHA
	var script struct {
		SHA  string
		Size int
	}
	if e := api("repos/actions/checkout/contents/dist/index.js?ref="+commit, &script); e != nil {
		return e
	}
	if !validSHA(script.SHA) || script.Size < 1 || script.Size > 8<<20 {
		return errors.New("checkout distribution metadata rejected")
	}
	raw, e := publicDistribution("https://raw.githubusercontent.com/actions/checkout/"+commit+"/dist/index.js", 8<<20)
	if e != nil {
		return e
	}
	if len(raw) != script.Size || blobID(raw) != script.SHA {
		return errors.New("checkout distribution Git blob mismatch")
	}
	var action struct{ Content, Encoding, SHA string }
	if e = api("repos/actions/checkout/contents/action.yml?ref="+commit, &action); e != nil {
		return e
	}
	a, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(action.Content, "\n", ""))
	if e != nil || action.Encoding != "base64" || blobID(a) != action.SHA {
		return errors.New("checkout action metadata mismatch")
	}
	var metadata struct{ Runs struct{ Using string } }
	if yaml.Unmarshal(a, &metadata) != nil || metadata.Runs.Using != "node20" {
		return errors.New("fixture must use checkout's declared Node20 engine")
	}
	if e = writeNewBytes(filepath.Join(p.runtime(), "sdk", "checkout.js"), raw); e != nil {
		return e
	}
	index, e := publicDistribution("https://nodejs.org/dist/index.json", 4<<20)
	if e != nil {
		return e
	}
	var versions []struct {
		Version string
		Files   []string
	}
	if json.Unmarshal(index, &versions) != nil {
		return errors.New("official Node index rejected")
	}
	version := ""
	for _, v := range versions {
		if !strings.HasPrefix(v.Version, "v20.") || !nodeVersion(v.Version) {
			continue
		}
		for _, f := range v.Files {
			if f == "win-x64-zip" {
				version = v.Version
				break
			}
		}
		if version != "" {
			break
		}
	}
	if version == "" {
		return errors.New("official Node20 Windows distribution absent")
	}
	name := "node-" + version + "-win-x64.zip"
	sums, e := publicDistribution("https://nodejs.org/dist/"+version+"/SHASUMS256.txt", 64<<10)
	if e != nil {
		return e
	}
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name && hex64.MatchString(fields[0]) {
			want = fields[0]
		}
	}
	if want == "" {
		return errors.New("official Node checksum absent")
	}
	z, e := publicDistribution("https://nodejs.org/dist/"+version+"/"+name, 64<<20)
	if e != nil {
		return e
	}
	if digest(z) != want {
		return errors.New("Node distribution checksum mismatch")
	}
	if e = extractZip(filepath.Join(p.runtime(), "sdk", "node"), z); e != nil {
		return e
	}
	return saveNew(filepath.Join(p.Root, "checkout-sdk.json"), map[string]any{"checkout_commit": commit, "checkout_blob": script.SHA, "checkout_sha256": digest(raw), "node_version": version, "node_zip_sha256": want, "declared_engine": "node20", "synthetic_only": true})
}

func nodeVersion(s string) bool {
	if !strings.HasPrefix(s, "v20.") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return false
		}
	}
	return true
}

func fixtureNode(p Plan) (string, error) {
	b, e := os.ReadFile(filepath.Join(p.Root, "checkout-sdk.json"))
	if e != nil {
		return "", e
	}
	var s struct {
		Version string `json:"node_version"`
	}
	if json.Unmarshal(b, &s) != nil || !nodeVersion(s.Version) {
		return "", errors.New("fixed Node SDK receipt rejected")
	}
	return filepath.Join(p.runtime(), "sdk", "node", "node-"+s.Version+"-win-x64", "node.exe"), nil
}
