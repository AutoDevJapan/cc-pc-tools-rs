//go:build windows && amd64

package main

import (
	"errors"
	"os"
)

// The administrator's durable collection marker is the authority. An absent
// root, an operator-authored success file, or a service's exit alone is not.
func verifyCollected(p Plan) error {
	if p.Fixture != "" {
		return errors.New("official collected resource required")
	}
	unlock, err := lockClaim(p)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err = os.Lstat(p.Root); !os.IsNotExist(err) {
		return errors.New("collected root is not retired")
	}
	root, err := collectedRoot(p)
	if err != nil || root != p.Root {
		return errors.New("privileged collection marker differs")
	}
	s, err := openService(p, 1|4)
	if err != nil {
		return err
	}
	defer closeSC(s)
	image, err := readImagePath(s)
	if err != nil {
		return err
	}
	bound, err := imageRoot(image)
	if err != nil || bound != p.Root {
		return errors.New("shared service no longer bound to collected root")
	}
	state, pid, _, err := queryService(s)
	if err != nil {
		return err
	}
	if err = collectionServiceStatus(state, pid); err != nil {
		return err
	}
	sid, err := serviceSID(p)
	if err != nil {
		return err
	}
	n, unreadable, err := ownedProcessCensus(sid)
	if err != nil || n != 0 {
		return errors.New("shared account processes remain or are uncertain")
	}
	if err = auditCollection(p); err != nil {
		return err
	}
	emitEvent("collection-verified", p, collectionObservation(n, unreadable))
	return nil
}

func collectionObservation(queryable, unreadable int) map[string]any {
	// The marker proves the administrator's completed collection; this current
	// census covers queryable processes only. Protected/unreadable processes
	// are reported, never silently counted as absent.
	return map[string]any{"complete": true, "root_retired": true, "scm_stopped": true,
		"queryable_account_process_count": queryable, "unreadable_process_count": unreadable,
		"process_census_scope": "queryable processes only; privileged collection marker and stopped bound SCM verified"}
}
