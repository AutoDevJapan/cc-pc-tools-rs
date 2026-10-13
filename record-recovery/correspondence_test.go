package main

import "testing"

func TestLocationHintsDoNotExpandOperationalScope(t *testing.T) {
	home := "D:/CC/Tools/discord-ask"
	for _, path := range []string{"D:/CC/Tools/discord-ask/state.json", "D:/CC/Work/original2467-api-1/report.json"} {
		if !operationalEvidenceHint(path, home) {
			t.Fatal("authorized operational location excluded")
		}
	}
	for _, path := range []string{"D:/CC/Tools/discord-ask2/private", "D:/CC/Tools/discord-ask/../other/private", "D:/CC/Work/unrelated/staging", "D:/CC/Work/original12467-api/report.json", "D:/CC/Work/original24670-api/report.json"} {
		if operationalEvidenceHint(path, home) {
			t.Fatal("text hint expanded source scope")
		}
	}
}
