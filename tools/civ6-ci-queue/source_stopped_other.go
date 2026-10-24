//go:build !windows

package main

import "errors"

func verifySourceProcessStopped(id ProcID) error {
	if id == (ProcID{}) {
		return nil
	}
	return errors.New("Windows source process proof requires its Windows controller")
}
