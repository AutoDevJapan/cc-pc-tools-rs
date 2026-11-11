package wrapper_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// The test binary doubles as the two harmless children. Their exact arguments
// and exit status cross the same WScript.Shell.Run boundary as production.
func TestMain(m *testing.M) {
	if home := os.Getenv("HIDDEN_WRAPPER_FIXTURE"); home != "" && len(os.Args) > 1 {
		name := ""
		switch os.Args[1] {
		case "check":
			name = "check"
		case "resume-ready":
			name = "resume"
		}
		if name != "" {
			body, _ := json.Marshal(os.Args[1:])
			if os.WriteFile(filepath.Join(home, name+".json"), body, 0600) != nil {
				os.Exit(90)
			}
			code, err := strconv.Atoi(os.Getenv("HIDDEN_WRAPPER_" + strings.ToUpper(name) + "_EXIT"))
			if err != nil {
				os.Exit(91)
			}
			os.Exit(code)
		}
	}
	os.Exit(m.Run())
}

func runWrapper(t *testing.T, missingCheck, missingResume bool, checkExit, resumeExit int) (int, string) {
	t.Helper()
	home := t.TempDir()
	source, err := os.ReadFile("../check-hidden.vbs")
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	check, resume := exe, exe
	if missingCheck {
		check = filepath.Join(home, "missing-check.exe")
	}
	if missingResume {
		resume = filepath.Join(home, "missing-resume.exe")
	}
	body := string(source)
	for old, replacement := range map[string]string{
		`D:\CC\Tools\chatgpt-web\chatgpt_web.exe`: check,
		`D:\CC\Tools\todo\todo.exe`:               resume,
	} {
		if strings.Count(body, old) != 1 {
			t.Fatalf("expected one existing child path %q", old)
		}
		body = strings.ReplaceAll(body, old, replacement)
	}
	path := filepath.Join(home, "check-hidden.vbs")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "cscript.exe"), "//B", "//NoLogo", path)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	command.Env = append(os.Environ(), "HIDDEN_WRAPPER_FIXTURE="+home,
		"HIDDEN_WRAPPER_CHECK_EXIT="+strconv.Itoa(checkExit),
		"HIDDEN_WRAPPER_RESUME_EXIT="+strconv.Itoa(resumeExit))
	output, err := command.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("host did not launch: %v (%s)", err, output)
		}
	}
	return command.ProcessState.ExitCode(), home
}

func childArgs(t *testing.T, home, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(home, name+".json"))
	if err != nil {
		t.Fatalf("%s child was not observed: %v", name, err)
	}
	return string(body)
}

func noResume(t *testing.T, home string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(home, "resume.json")); !os.IsNotExist(err) {
		t.Fatalf("resume must not run after failed check: %v", err)
	}
}

func TestMissingCheckIsFailure(t *testing.T) {
	code, home := runWrapper(t, true, false, 0, 0)
	if code != 1 {
		t.Fatalf("missing checker must return 1, got %d", code)
	}
	noResume(t, home)
}

func TestFailedCheckKeepsItsCodeAndDoesNotResume(t *testing.T) {
	code, home := runWrapper(t, false, false, 3, 0)
	if args := childArgs(t, home, "check"); args != `["check"]` {
		t.Fatal(args)
	}
	if code != 3 {
		t.Fatalf("failed check must return 3, got %d", code)
	}
	noResume(t, home)
}

func TestSuccessfulCheckResumesOriginalWithExactArguments(t *testing.T) {
	code, home := runWrapper(t, false, false, 0, 0)
	if args := childArgs(t, home, "check"); args != `["check"]` {
		t.Fatal(args)
	}
	if args := childArgs(t, home, "resume"); args != `["resume-ready","1974","--condition","chatgpt-plus"]` {
		t.Fatal(args)
	}
	if code != 0 {
		t.Fatalf("successful children returned %d", code)
	}
}

func TestMissingResumeIsFailure(t *testing.T) {
	code, home := runWrapper(t, false, true, 0, 0)
	childArgs(t, home, "check")
	if code != 1 {
		t.Fatalf("missing resume must return 1, got %d", code)
	}
}

func TestFailedResumeKeepsItsCode(t *testing.T) {
	code, home := runWrapper(t, false, false, 0, 2)
	childArgs(t, home, "check")
	childArgs(t, home, "resume")
	if code != 2 {
		t.Fatalf("failed resume must return 2, got %d", code)
	}
}
