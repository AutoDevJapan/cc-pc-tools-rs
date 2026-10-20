package windowsrunner

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestConfigurationFailureReachesHostWithoutRegistrationOrSecret(t *testing.T) {
	for _, result := range []processResult{
		{phase: "create", nativeError: 1314, err: errors.New("synthetic-private-error")},
		{phase: "execute", exit: 0xc0000142, drained: true, logHash: strings.Repeat("a", 64)},
	} {
		t.Run(result.phase, func(t *testing.T) {
			packet := fixturePacket()
			seed := testSeed()
			seed.Plan.Source = packet.Pin
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			host, guest := net.Pipe()
			ops := fake(seed.Plan)
			ops.configureError = &configurationError{result: result}
			h := &fakeHost{seed: seed}
			done := make(chan error, 1)
			go func() { defer guest.Close(); done <- guestSession(ctx, guest, seed, ops) }()
			ev, hostErr := HostSession(ctx, host, seed, packet, h)
			guestErr := <-done
			if hostErr == nil || guestErr == nil || ev.Stage != Failed || ev.FailureCode != "configuration-"+result.phase || ev.NativeError != result.nativeError {
				t.Fatalf("configuration failure lost: host=%v guest=%v event=%#v", hostErr, guestErr, ev)
			}
			if len(h.observations) != 2 || h.observations[0].Stage != SourceReady || !reflect.DeepEqual(h.observations[1], ev) {
				t.Fatal("failure not persisted after physical source proof")
			}
			if h.registered || h.claims != 0 || ops.configured || ops.listened || ev.RunnerID != 0 {
				t.Fatal("configuration failure authorized a registration/listener/claim")
			}
			if ev.Drained != result.drained || (ev.Exit != nil) != result.drained || (ev.Exit != nil && *ev.Exit != result.exit) {
				t.Fatal("unmeasured exit or drain fabricated")
			}
			for _, b := range ops.configureToken {
				if b != 0 {
					t.Fatal("registration token retained after failure")
				}
			}
			encoded, err := json.Marshal(ev)
			if err != nil || strings.Contains(string(encoded), "synthetic-") {
				t.Fatal("configuration output/error/token escaped into event")
			}
			select {
			case <-ops.closed:
			default:
				t.Fatal("operations left open")
			}
		})
	}
}

func TestConfigurationFailureUsesOnlyFixedMetadata(t *testing.T) {
	for _, err := range []error{
		errors.New("synthetic-private-error"),
		&configurationError{result: processResult{phase: "synthetic-private-phase", nativeError: 99, logHash: "synthetic-private-output", drained: true, exit: 99}},
	} {
		ev := configurationFailure(testSeed().Plan.Instance, err)
		if ev.FailureCode != "configuration-setup" || ev.NativeError != 0 || ev.LogSHA256 != "" || ev.Exit != nil || ev.Drained {
			t.Fatalf("unknown failure emitted unbounded/unmeasured data: %#v", ev)
		}
	}
}

func TestHostRejectsConfigurationFailureOutsideItsStageAndIdentity(t *testing.T) {
	p := testSeed().Plan
	base := configurationFailure(p.Instance, &configurationError{result: processResult{phase: "create", nativeError: 1314}})
	if !validFailure(p, Configured, 0, base) {
		t.Fatal("bounded creation failure rejected")
	}
	for _, stage := range []Stage{SourceReady, Start, Complete, RunnerExited} {
		if validFailure(p, stage, 0, base) {
			t.Fatal("failure admitted outside configuration")
		}
	}
	for name, change := range map[string]func(*Event){
		"instance":           func(e *Event) { e.Instance = "wrong" },
		"runner":             func(e *Event) { e.RunnerID = 789 },
		"stage":              func(e *Event) { e.Stage = Configured },
		"code":               func(e *Event) { e.FailureCode = "configuration-arbitrary" },
		"execute-native":     func(e *Event) { e.FailureCode = "configuration-execute" },
		"setup-native":       func(e *Event) { e.FailureCode = "configuration-setup" },
		"undrained-exit":     func(e *Event) { exit := uint32(0); e.Exit = &exit },
		"drain-without-exit": func(e *Event) { e.Drained = true; e.LogSHA256 = strings.Repeat("f", 64) },
		"exit-without-hash":  func(e *Event) { exit := uint32(1); e.Exit = &exit; e.Drained = true },
		"invalid-hash":       func(e *Event) { e.LogSHA256 = "private-output" },
	} {
		t.Run(name, func(t *testing.T) {
			ev := base
			change(&ev)
			if validFailure(p, Configured, 0, ev) {
				t.Fatal("malformed failure accepted")
			}
		})
	}
	if validFailure(p, Configured, 789, base) {
		t.Fatal("configuration failure accepted after registration")
	}
	for _, code := range []string{"process", "timeout", "drain-unconfirmed"} {
		exit := uint32(1)
		ev := Event{Instance: p.Instance, Stage: Failed, RunnerID: 789, FailureCode: code, Drained: code != "drain-unconfirmed", Exit: &exit, LogSHA256: strings.Repeat("f", 64), NativeError: 5}
		if validFailure(p, RunnerExited, 789, ev) {
			t.Fatal("native configuration error accepted as listener failure")
		}
	}
}
