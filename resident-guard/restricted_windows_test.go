//go:build windows

package main

import (
	"syscall"
	"testing"
)

func TestNormalUserTokenRetainsUserAndDisablesAdministrator(t *testing.T) {
	process, err := syscall.GetCurrentProcess()
	if err != nil {
		t.Fatal("own process unavailable")
	}
	var current syscall.Token
	if syscall.OpenProcessToken(process, 0x8|0x2|0x1, &current) != nil {
		t.Fatal("own token unavailable")
	}
	defer current.Close()
	originalUser, err := current.GetTokenUser()
	if err != nil {
		t.Fatal("own user unavailable")
	}
	originalSID, err := originalUser.User.Sid.String()
	if err != nil {
		t.Fatal("own SID unavailable")
	}
	restricted, err := makeLimitedToken(current)
	if err != nil {
		t.Fatal("normal user token could not be created", err)
	}
	defer restricted.Close()
	user, err := restricted.GetTokenUser()
	if err != nil {
		t.Fatal("restricted user unavailable")
	}
	sid, err := user.User.Sid.String()
	if err != nil || sid != originalSID {
		t.Fatal("restriction changed the account")
	}
	admin, err := administratorEnabled(restricted)
	_, medium, integrityErr := restrictionState(restricted)
	if err != nil || integrityErr != nil || admin || !medium {
		t.Fatal("restriction retained administrative membership or non-medium integrity")
	}
}
