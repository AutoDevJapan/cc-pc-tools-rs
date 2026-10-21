package main

import "testing"

func healthySDKSeal() sdkResult {
	return sdkResult{
		DiskSynced: true,
		RustProbe: []rustProbeResult{
			{Name: "compiler", CompilerSHA256: sdkCompilerSHA},
			{Name: "verbose", Version: sdkVersion, CommitHash: "8bab26f4f68e0e26f0bb7960be334d5b520ea452", CommitDate: "2026-07-14"},
			{Name: "cachekey", CacheKey: "202607148bab"},
		},
		RustDriver: []sdkArtifactProbe{{Name: sdkDriverName, Bytes: sdkDriverBytes, SHA256: sdkDriverSHA, ReadComplete: true}},
	}
}

func TestSDKSealRejectsMeasuredTruncatedDriverAndCompilerSignal(t *testing.T) {
	good := healthySDKSeal()
	if e := validatePublicSDKSeal(good); e != nil {
		t.Fatal("official component control rejected", e)
	}
	for _, failure := range []string{"measured-sdk", "driver-only", "signal-only", "sync-unproved", "timeout", "source", "registration", "missing-probe", "missing-version", "wrong-version"} {
		t.Run(failure, func(t *testing.T) {
			r := healthySDKSeal()
			switch failure {
			case "measured-sdk", "driver-only":
				// Actual source-free SDK observation, 2026-10-19: rustc itself
				// matched, but the loaded public driver was truncated.
				r.RustDriver[0].Bytes = 130023424
				r.RustDriver[0].SHA256 = "36e52d4528b072286adb32afb084b7cb6593cfd7defbbf7fa4b81c33668705ab"
			}
			switch failure {
			case "measured-sdk", "signal-only":
				r.RustProbe[1].Exit = -1
				r.RustProbe[1].Signal = 7
			case "sync-unproved":
				r.DiskSynced = false
			case "timeout":
				r.RustProbe[1].TimedOut = true
			case "source":
				r.ProductSource = true
			case "registration":
				r.RunnerRegistered = true
			case "missing-probe":
				r.RustProbe = r.RustProbe[:2]
			case "missing-version":
				r.RustProbe[1].Version = ""
			case "wrong-version":
				r.RustProbe[1].Version = "rustc 1.97.0 (8bab26f4f 2026-07-14)"
			}
			if validatePublicSDKSeal(r) == nil {
				t.Fatal("unusable/unpersisted SDK accepted", failure)
			}
		})
	}
}
