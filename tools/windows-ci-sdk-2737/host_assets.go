package sdkprep

import (
	_ "embed"
	"encoding/xml"
	"errors"
	"path/filepath"
	"strings"
)

const ownedRootPlaceholder = "__CCSDK2737_OWNED_ROOT__"

//go:embed assets/Autounattend.xml
var setupAnswerFile []byte

//go:embed assets/stage.ps1
var setupStageScript []byte

//go:embed assets/create-seed-iso.ps1
var seedISOBuilderScript []byte

//go:embed assets/provision-vm.ps1
var provisionVMPS []byte

//go:embed assets/remove-vm-keep-vhd.ps1
var removeVMKeepVHDPS []byte

type answerDocument struct {
	Settings []struct {
		Pass string `xml:"pass,attr"`
	} `xml:"settings"`
}

func ValidateSetupAssets() error {
	var doc answerDocument
	if err := xml.Unmarshal(setupAnswerFile, &doc); err != nil {
		return errors.New("Windows answer file is malformed")
	}
	if len(doc.Settings) != 2 || doc.Settings[0].Pass != "windowsPE" || doc.Settings[1].Pass != "specialize" {
		return errors.New("Windows answer file passes are not the fixed setup passes")
	}
	text := string(setupAnswerFile)
	if !strings.Contains(text, "<AcceptEula>true</AcceptEula>") || strings.Contains(text, "oobeSystem") || strings.Contains(text, "SkipMachineOOBE") || strings.Contains(text, "UserAccounts") || strings.Contains(text, "AdministratorPassword") || strings.Contains(text, "<Password>") {
		return errors.New("Windows answer file contains an unapproved account or OOBE setting")
	}
	stage := string(setupStageScript)
	for _, required := range []string{"CCSDK2737", "LocalSystem", "WindowsCISDK2737", "StartName", "Start-Service"} {
		if !strings.Contains(stage, required) {
			return errors.New("LocalSystem service stage script is incomplete")
		}
	}
	vmScript := string(provisionVMPS)
	removeScript := string(removeVMKeepVHDPS)
	if strings.Count(vmScript, ownedRootPlaceholder) != 1 || strings.Count(removeScript, ownedRootPlaceholder) != 1 || strings.Contains(vmScript, "$PSScriptRoot") || strings.Contains(removeScript, "$PSScriptRoot") {
		return errors.New("admin broker scripts must use the explicit owned root")
	}
	for _, required := range []string{"-Generation 1", "-MemoryStartupBytes 4GB", "-Count 2", "Default Switch", "Set-VMComPort", "Microsoft Corporation", "Remove-VMNetworkAdapter", "Get-VMGpuPartitionAdapter"} {
		if !strings.Contains(vmScript, required) {
			return errors.New("Hyper-V provisioning script is missing a required constraint")
		}
	}
	if strings.Contains(vmScript, "New-VMSwitch") || strings.Contains(vmScript, "Add-VMGpuPartitionAdapter") {
		return errors.New("Hyper-V provisioning script contains a host switch or GPU operation")
	}
	return nil
}

func renderAdminPowerShellScript(template []byte, root string) ([]byte, error) {
	if !filepath.IsAbs(root) || strings.ContainsAny(root, "\r\n\x00") || strings.Count(string(template), ownedRootPlaceholder) != 1 {
		return nil, errors.New("admin script owned-root rendering rejected")
	}
	escaped := strings.ReplaceAll(root, "'", "''")
	return []byte(strings.Replace(string(template), ownedRootPlaceholder, escaped, 1)), nil
}

func guestSeedAsset(name string) ([]byte, bool) {
	switch name {
	case "Autounattend.xml":
		return append([]byte(nil), setupAnswerFile...), true
	case "stage.ps1":
		return append([]byte(nil), setupStageScript...), true
	default:
		return nil, false
	}
}
