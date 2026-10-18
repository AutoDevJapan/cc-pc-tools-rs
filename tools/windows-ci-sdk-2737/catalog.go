package sdkprep

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// Pins are from official release metadata and Microsoft component IDs. The
// Build Tools bootstrapper is intentionally not listed as a hash-verified
// payload here; its Authenticode signer/version must be checked in the guest.
const rustChannelManifestSHA256 = "03569b1886ceb5c05276b50c8431ab111de944cd6140fe1fa7d821dd8e0f29cf"

func VerifyRustChannelManifest(data []byte) error {
	if len(data) == 0 || len(data) > 8<<20 {
		return errors.New("Rust channel manifest size is invalid")
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != rustChannelManifestSHA256 || !strings.Contains(string(data), "version = \"1.97.1 ") {
		return errors.New("Rust channel manifest does not match the pinned release")
	}
	return nil
}

type PackagePin struct {
	ID      PackageID
	Version string
}

var packagePins = [...]PackagePin{
	{ID: GitForWindows, Version: "2.56.0.2"},
	{ID: PowerShell7, Version: "7.6.6"},
	{ID: RustToolchain, Version: "1.97.1"},
	{ID: MSVCBuildTools, Version: "14.50.18"},
	{ID: WindowsSDK, Version: "10.0.26100.6901"},
	{ID: Sccache, Version: "0.18.0"},
}

var pinnedArtifacts = [...]Artifact{
	{ID: GitForWindows, Version: "2.56.0.2", URL: "https://github.com/git-for-windows/git/releases/download/v2.56.0.windows.2/Git-2.56.0.2-64-bit.exe", SHA256: "52188f917b378f00c70ec136bcf090005f30d44fbc4eba0bce759cc6592d60f6"},
	{ID: PowerShell7, Version: "7.6.6", URL: "https://github.com/PowerShell/PowerShell/releases/download/v7.6.6/PowerShell-7.6.6-win-x64.msi", SHA256: "958838ff55091e1c8705d89efed0cc7e8245a3a6ef6c0ccfae20015227108ad8"},
	{ID: RustToolchain, Version: "1.97.1", URL: "https://static.rust-lang.org/dist/2026-07-16/cargo-1.97.1-x86_64-pc-windows-msvc.tar.gz", SHA256: "5c023e6f3d9eb8bec511bfcda889668b1be6981459d1b40be0bf4444ab059563"},
	{ID: RustToolchain, Version: "1.97.1", URL: "https://static.rust-lang.org/dist/2026-07-16/clippy-1.97.1-x86_64-pc-windows-msvc.tar.gz", SHA256: "c1d14a9ae79ec35f04b3ae07b383b5902971ea76b54fc8b571d663880b5d9799"},
	{ID: RustToolchain, Version: "1.97.1", URL: "https://static.rust-lang.org/dist/2026-07-16/rustc-1.97.1-x86_64-pc-windows-msvc.tar.gz", SHA256: "b45f488437194f810076ed26cdcc78bf916d40be8f0a55c183f406c909c1af3c"},
	{ID: RustToolchain, Version: "1.97.1", URL: "https://static.rust-lang.org/dist/2026-07-16/rust-std-1.97.1-x86_64-pc-windows-msvc.tar.gz", SHA256: "ea544e8d86c9978987a91bad66a0750003840cb995c54972f34fa69343fce74b"},
	{ID: RustToolchain, Version: "1.97.1", URL: "https://static.rust-lang.org/dist/2026-07-16/rustfmt-1.97.1-x86_64-pc-windows-msvc.tar.gz", SHA256: "ed2217b8490b833c4748b29846fd04d8fe25ea9bb826137041d404166abc8ef1"},
	{ID: Sccache, Version: "0.18.0", URL: "https://github.com/mozilla/sccache/releases/download/v0.18.0/sccache-v0.18.0-x86_64-pc-windows-msvc.zip", SHA256: "8965c74d5e8a225244f741e18ad2f3f504f48228dc1bac948fc22761a348363d"},
}

// PinnedPackages returns a copy so callers cannot mutate the accepted set.
func PinnedPackages() []PackagePin {
	return append([]PackagePin(nil), packagePins[:]...)
}

func PinnedArtifacts() []Artifact {
	return append([]Artifact(nil), pinnedArtifacts[:]...)
}

func pinnedVersion(id PackageID) (string, bool) {
	for _, pin := range packagePins {
		if pin.ID == id {
			return pin.Version, true
		}
	}
	return "", false
}
