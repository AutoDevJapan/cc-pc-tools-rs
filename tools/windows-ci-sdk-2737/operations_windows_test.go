//go:build windows && amd64

package sdkprep

import "testing"

func TestPinnedArtifactFilenamesAcceptZerosAndX64(t *testing.T) {
	for _, a := range PinnedArtifacts() {
		if _, err := artifactFilename(a); err != nil {
			t.Errorf("public %s %s filename rejected: %v", a.ID, a.Version, err)
		}
	}
}

func TestMeasuredCompilerVersionIgnoresEnvironmentVersions(t *testing.T) {
	output := []byte("VCToolsVersion=14.50.11121\nWindowsSDKVersion=10.0.26100.0\\\nMicrosoft C/C++ Compiler Version 19.50.11121 for x64")
	if got := measuredVersion(output, "19.50."); got != "19.50.11121" {
		t.Fatalf("compiler probe selected %q", got)
	}
	if got := measuredVersion([]byte("git version 2.56.0.windows.2"), "2.56.0.windows.2"); got != "2.56.0.windows.2" {
		t.Fatalf("Git probe selected %q", got)
	}
}
