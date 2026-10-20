//go:build windows && amd64

package windowsrunner

import (
	sourcepacket "cc/ci-source-2737"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSystemProofUsesPrivateTempWhenRunnerTempIsUnusable(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "private", "source-proof")
	if e := os.MkdirAll(private, 0700); e != nil {
		t.Fatal(e)
	}
	// A public fault-injection fixture, not the host SDK. If the proof selects
	// the worker's staging path, real MkdirTemp/Git proof must fail here.
	if e := os.WriteFile(filepath.Join(root, "staging"), []byte("public-unusable-worker-temp"), 0600); e != nil {
		t.Fatal(e)
	}
	env := fixedEnvironmentValues(testSeed().Plan, `C:\public\sccache.exe`)
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "TMP") || strings.EqualFold(key, "TEMP") || strings.EqualFold(key, "SystemTemp") {
			rel, e := filepath.Rel(sdkRoot, value)
			if e != nil || rel == ".." || strings.HasPrefix(rel, `..\`) {
				t.Fatal("temp left fixed SDK")
			}
			t.Setenv(key, filepath.Join(root, rel))
		}
	}
	packet := fixturePacket()
	proof, e := sourcepacket.Materialize(context.Background(), packet, packet.Pin, filepath.Join(root, "checkout"))
	if e != nil || !proof.PhysicalBlobsVerified {
		t.Fatalf("private materialization/proof failed with hostile worker temp: %v", e)
	}
	if filepath.Clean(os.TempDir()) != filepath.Clean(private) {
		t.Fatal("proof used another temporary root")
	}
	if _, e := sourcepacket.VerifyCheckout(context.Background(), packet.Pin, packet.TreeEntries, filepath.Join(root, "checkout")); e != nil {
		t.Fatalf("subsequent private proof failed: %v", e)
	}
}

func TestRunnerTempFixture(t *testing.T) {
	if os.Getenv("CC2737_TEMP_FIXTURE") != "1" {
		return
	}
	temp, e := os.MkdirTemp("", "public-runner-temp-")
	if e != nil {
		os.Exit(61)
	}
	defer os.RemoveAll(temp)
	if os.WriteFile(os.Getenv("CC2737_TEMP_RESULT"), []byte(filepath.Dir(temp)), 0600) != nil {
		os.Exit(62)
	}
}

func TestRunnerTempEnvironmentDoesNotChangeServiceProofTemp(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{filepath.Join(root, "staging"), filepath.Join(root, "private", "source-proof")} {
		if e := os.MkdirAll(path, 0700); e != nil {
			t.Fatal(e)
		}
	}
	service := fixedEnvironmentValues(testSeed().Plan, `C:\public\sccache.exe`)
	before := strings.Join(service, "\n")
	child := runnerEnvironment(append(append([]string(nil), service...), "tMp=duplicate-public-fixture", "SYSTEMTEMP=duplicate-public-fixture"))
	if strings.Join(service, "\n") != before {
		t.Fatal("child mutated service environment")
	}
	mapTemp := func(env []string, parent bool) []string {
		out := append([]string(nil), env...)
		counts := map[string]int{}
		for i, entry := range out {
			key, value, _ := strings.Cut(entry, "=")
			upper := strings.ToUpper(key)
			if upper != "TMP" && upper != "TEMP" && upper != "SYSTEMTEMP" {
				continue
			}
			counts[upper]++
			want := sdkRoot + `\staging`
			if parent {
				want = proofTempRoot
			}
			if value != want {
				t.Fatal("wrong process temp boundary")
			}
			rel, e := filepath.Rel(sdkRoot, value)
			if e != nil {
				t.Fatal(e)
			}
			mapped := filepath.Join(root, rel)
			out[i] = key + "=" + mapped
			if parent {
				t.Setenv(key, mapped)
			}
		}
		if counts["TMP"] != 1 || counts["TEMP"] != 1 || counts["SYSTEMTEMP"] != map[bool]int{true: 1, false: 0}[parent] {
			t.Fatal("duplicate/missing temp variables")
		}
		return out
	}
	mapTemp(service, true)
	child = mapTemp(child, false)
	result := filepath.Join(root, "child-temp.txt")
	child = append(child, "CC2737_TEMP_FIXTURE=1", "CC2737_TEMP_RESULT="+result)
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := runTree(ctx, exe, syscall.EscapeArg(exe)+" -test.run=^TestRunnerTempFixture$", root, child, "")
	if r.err != nil || r.exit != 0 || !r.drained {
		t.Fatalf("public temp child failed: %v", r.err)
	}
	got, e := os.ReadFile(result)
	if e != nil || string(got) != filepath.Join(root, "staging") {
		t.Fatal("child did not create its temp under worker staging")
	}
	if filepath.Clean(os.TempDir()) != filepath.Join(root, "private", "source-proof") {
		t.Fatal("child altered parent proof temp")
	}
}

func TestProofTempIsOutsideRunnerWritableGrants(t *testing.T) {
	for _, args := range runnerAccessCommands() {
		if args[1] != "/grant:r" || (!strings.HasSuffix(args[2], "M") && !strings.HasSuffix(args[2], "F")) {
			continue
		}
		rel, e := filepath.Rel(args[0], proofTempRoot)
		if e != nil {
			t.Fatal(e)
		}
		if rel != ".." && !strings.HasPrefix(rel, `..\`) {
			t.Fatal("runner can modify proof temp or an ancestor")
		}
	}
}
