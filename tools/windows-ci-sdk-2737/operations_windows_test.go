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
