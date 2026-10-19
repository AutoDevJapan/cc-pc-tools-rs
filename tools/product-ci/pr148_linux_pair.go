package main

import (
	"bytes"
	"debug/buildinfo"
	"errors"
	"os"
	"reflect"
	"runtime/debug"
	"strings"
)

func pr148BuildRevision(info *debug.BuildInfo) (string, error) {
	if info == nil {
		return "", errors.New("helper build metadata absent")
	}
	settings := map[string]string{}
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}
	r := settings["vcs.revision"]
	if len(r) != 40 || strings.Trim(r, "0123456789abcdef") != "" || settings["vcs.modified"] != "false" || settings["vcs"] != "git" {
		return "", errors.New("helpers require a committed clean Git source")
	}
	return r, nil
}

func pr148HelperPair(guest []byte) (controllerSHA, revision string, ret error) {
	self, e := os.Executable()
	if e != nil {
		return "", "", e
	}
	controller, e := os.ReadFile(self)
	if e != nil {
		return "", "", e
	}
	return pr148HelperPairBytes(controller, guest)
}

func pr148HelperPairBytes(controller, guest []byte) (controllerSHA, revision string, ret error) {
	if len(guest) < 64 || !bytes.Equal(guest[:4], []byte{127, 'E', 'L', 'F'}) {
		return "", "", errors.New("Linux helper ELF required")
	}
	h, he := buildinfo.Read(bytes.NewReader(controller))
	g, ge := buildinfo.Read(bytes.NewReader(guest))
	if he != nil || ge != nil {
		return "", "", errors.New("paired helper build metadata unavailable")
	}
	hr, e := pr148CompareHelperBuilds(h, g)
	if e != nil {
		return "", "", e
	}
	return digest(controller), hr, nil
}

func pr148CompareHelperBuilds(h, g *debug.BuildInfo) (string, error) {
	hr, he := pr148BuildRevision(h)
	gr, ge := pr148BuildRevision(g)
	if he != nil || ge != nil || hr != gr || h.Main.Path != g.Main.Path || h.GoVersion != g.GoVersion || !reflect.DeepEqual(h.Deps, g.Deps) {
		return "", errors.New("controller and guest must use the same clean committed source and dependencies")
	}
	settings := map[string]string{}
	for _, s := range g.Settings {
		settings[s.Key] = s.Value
	}
	if settings["GOOS"] != "linux" || settings["GOARCH"] != "amd64" {
		return "", errors.New("Linux amd64 guest helper required")
	}
	return hr, nil
}
