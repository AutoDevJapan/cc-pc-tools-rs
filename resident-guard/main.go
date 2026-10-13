package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func main() { os.Exit(entry()) }

func entry() int {
	stateDirectory := flag.String("state-dir", "D:/CC/Tools/resident-guard", "absolute guard state directory")
	fixturePort := flag.Int("fixture-port", 0, "isolated fixture service port")
	fixtureTask := flag.String("fixture-task", "", "isolated fixture task name")
	fixtureServer := flag.Bool("fixture-server", false, "serve the isolated readiness fixture")
	sessionProbe := flag.Bool("session-probe", false, "inspect a non-UI scheduler session without changing credentials")
	expectedSID := flag.String("expected-user-sid", "", "expected existing Windows user SID for the isolated session inspection")
	restrictedChild := flag.Bool("restricted-child", false, "verify a privilege-reduced child of the same S4U user")
	launchService := flag.String("launch-service", "", "launch one compiled non-UI service without duplicating a listener")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected_arguments")
		return 2
	}
	if *launchService != "" && !validServiceID(*launchService) {
		fmt.Fprintln(os.Stderr, "service_id_invalid")
		return 2
	}
	if handled, code, err := reduceS4UPrivileges(*restrictedChild, *launchService != ""); handled {
		if err != nil {
			if *sessionProbe && filepath.Base(filepath.Clean(*stateDirectory)) == "boot-probe-state-2492" && validateStateDirectory(*stateDirectory) == nil {
				_ = saveJSON(filepath.Join(*stateDirectory, "session.json"), sessionReport{Version: 1, CheckedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(), ProcessID: os.Getpid(), InspectionError: err.Error(), Services: []serviceReport{}, CLIAuth: []cliAuthStatus{}})
			}
			fmt.Fprintln(os.Stderr, "s4u_privilege_reduction_failed")
		}
		return code
	}
	if *launchService != "" {
		if *sessionProbe || *fixtureServer || *fixturePort != 0 || *fixtureTask != "" || *expectedSID != "" {
			return 2
		}
		if err := launchKnownService(*launchService); err != nil {
			fmt.Fprintln(os.Stderr, "service_launch_failed")
			return 1
		}
		return 0
	}
	if *sessionProbe {
		if *fixtureServer || *fixturePort != 0 || *fixtureTask != "" || *expectedSID == "" ||
			filepath.Base(filepath.Clean(*stateDirectory)) != "boot-probe-state-2492" {
			fmt.Fprintln(os.Stderr, "session_probe_configuration_invalid")
			return 2
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		out, err := inspectSession(ctx, *expectedSID)
		if out.FinishedAt.IsZero() {
			out.FinishedAt = time.Now().UTC()
		}
		if err != nil {
			out.InspectionError = err.Error()
		}
		if validateStateDirectory(*stateDirectory) != nil || saveJSON(filepath.Join(*stateDirectory, "session.json"), out) != nil {
			fmt.Fprintln(os.Stderr, "session_probe_failed")
			return 1
		}
		json.NewEncoder(os.Stdout).Encode(out)
		if err != nil {
			return 1
		}
		return 0
	}
	if *fixtureServer {
		if !validFixturePort(*fixturePort) {
			fmt.Fprintln(os.Stderr, "fixture_port_invalid")
			return 2
		}
		return serveFixture(*fixturePort)
	}
	definitions := productionServices()
	mutex := "Global\\CC-ResidentGuard"
	p := defaultPolicy()
	if *fixturePort != 0 || *fixtureTask != "" {
		if !validFixturePort(*fixturePort) || *fixtureTask != "CC-ResidentGuard-Fixture-2490" ||
			filepath.Base(filepath.Clean(*stateDirectory)) != "fixture-state-2490" {
			fmt.Fprintln(os.Stderr, "fixture_configuration_invalid")
			return 2
		}
		executable, err := os.Executable()
		if err != nil {
			return 2
		}
		definitions = []service{{
			ID: "fixture", Task: *fixtureTask, URL: fmt.Sprintf("http://127.0.0.1:%d/healthz", *fixturePort),
			Address: net.JoinHostPort("127.0.0.1", strconv.Itoa(*fixturePort)), Execute: executable,
			Arguments: fmt.Sprintf("--fixture-server --fixture-port %d", *fixturePort),
		}}
		mutex = "Local\\CC-ResidentGuard-Fixture-2490"
		p.FailureThreshold = 1
	}
	release, acquired, err := acquireMutex(mutex)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mutex_unavailable")
		return 1
	}
	if !acquired {
		fmt.Println(`{"skipped":"already_running"}`)
		return 0
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out, err := runCheck(ctx, *stateDirectory, definitions, nativeBackend{}, p)
	json.NewEncoder(os.Stdout).Encode(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "guard_check_failed")
		return 1
	}
	return 0
}

func validFixturePort(port int) bool {
	return port >= 20000 && port <= 65000
}

func serveFixture(port int) int {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Write([]byte(`{"status":"ok","service":"opencodex"}`))
	})
	server := http.Server{Addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), Handler: mux,
		ReadHeaderTimeout: 2 * time.Second, IdleTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second}
	if err := server.ListenAndServe(); err != nil {
		return 1
	}
	return 0
}
