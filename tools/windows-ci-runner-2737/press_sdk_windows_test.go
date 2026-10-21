package windowsrunner

import (
	"context"
	"strings"
	"testing"
)

func TestPressSDKUsesRustupProxiesWithoutAlphaCompilerFallback(t *testing.T) {
	p := pressDeadlinePlan("rust-v2-https.yml", "rust-v2-https-windows-2022")
	path := ""
	for _, entry := range fixedEnvironmentValues(p, `C:\public\sccache.exe`) {
		if strings.HasPrefix(entry, "PATH=") {
			path = strings.TrimPrefix(entry, "PATH=")
		}
	}
	if strings.Contains(path, `rust-1.97.1\bin`) || !strings.Contains(path, `cargo-home\bin`) || !strings.Contains(path, `externals\node24\bin`) {
		t.Fatal("Press did not select its rustup proxies")
	}
	if servicePathFor(testSeed().Plan, `C:\public\sccache.exe`) != fixedServicePath(`C:\public\sccache.exe`) {
		t.Fatal("alpha toolchain changed")
	}
	// The real host test binary is not the fixed SYSTEM guest service. It must
	// reject before download, filesystem mutation or process installation.
	if preparePressWindowsSDK(context.Background(), p, nil) == nil {
		t.Fatal("host was admitted as disposable guest")
	}
}
