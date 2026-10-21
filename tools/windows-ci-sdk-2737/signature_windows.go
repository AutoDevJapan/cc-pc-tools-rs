//go:build windows && amd64

package sdkprep

import (
	"errors"
	"os/exec"
	"strings"
)

func verifyMicrosoftSigner(file string) error {
	if strings.ContainsAny(file, "\r\n\x00'") {
		return errors.New("bootstrapper path is invalid")
	}
	powershell := `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`
	script := "$s=Get-AuthenticodeSignature -LiteralPath '" + file + "'; if($s.Status -ne 'Valid' -or $s.SignerCertificate.GetNameInfo([System.Security.Cryptography.X509Certificates.X509NameType]::SimpleName,$false) -ne 'Microsoft Corporation'){exit 73}; exit 0"
	cmd := exec.Command(powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = []string{"SystemRoot=C:\\Windows", "PATH=C:\\Windows\\System32"}
	cmd.Stdout, cmd.Stderr, cmd.Stdin = nil, nil, nil
	if err := cmd.Run(); err != nil {
		return errors.New("Microsoft Authenticode signer validation failed")
	}
	return nil
}
