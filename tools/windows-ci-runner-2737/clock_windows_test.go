//go:build windows && amd64

package windowsrunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServicePATHResolvesStockWindowsPowerShell(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	log := filepath.Join(root, "where.log")
	env := []string{`SystemRoot=C:\Windows`, `ComSpec=C:\Windows\System32\cmd.exe`, `PATHEXT=.COM;.EXE;.BAT;.CMD`, "PATH=" + fixedServicePath(`C:\fixture\sccache.exe`)}
	r := runTree(ctx, `C:\Windows\System32\cmd.exe`, `"C:\Windows\System32\cmd.exe" /d /s /c "where powershell.exe"`, root, env, log)
	b, e := os.ReadFile(log)
	if e != nil || r.err != nil || r.exit != 0 || !r.drained || !strings.EqualFold(strings.TrimSpace(string(b)), `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`) {
		t.Fatalf("stock Windows PowerShell unresolved: exit=%d drained=%v error=%v read=%v", r.exit, r.drained, r.err, e)
	}
	if !strings.Contains(fixedServicePath(`C:\fixture\sccache.exe`), sdkRoot+`\powershell\7;`) {
		t.Fatal("PowerShell 7 path lost")
	}
}
