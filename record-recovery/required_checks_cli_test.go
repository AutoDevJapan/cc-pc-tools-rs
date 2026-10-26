package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

func TestRequiredChecksMetadataMainHelper(t *testing.T) {
	if os.Getenv("CC_REQUIRED_CHECKS_MAIN_HELPER") != "1" {
		return
	}
	os.Args = []string{"record-recovery", "required-checks-metadata", "--source=synthetic-forbidden-source"}
	main()
	t.Fatal("main did not exit")
}

func TestRequiredChecksMetadataMainDispatchRefusesArguments(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestRequiredChecksMetadataMainHelper$")
	cmd.Env = append(os.Environ(), "CC_REQUIRED_CHECKS_MAIN_HELPER=1")
	hideCommand(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 2 || stderr.Len() != 0 {
		t.Fatalf("unexpected main result: %v", err)
	}
	var response obj
	if json.Unmarshal(output, &response) != nil || response["code"] != "required_checks_invalid_arguments" {
		t.Fatal("main did not select the required-checks metadata command")
	}
}
