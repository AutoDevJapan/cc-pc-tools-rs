package main

import "errors"

func superviseNormal() int { return 1 }

func cargoScratchEnv(mode string) ([]string, error) {
	if mode == "normal" {
		return nil, errors.New("Cargo execution requires Linux")
	}
	return nil, nil
}
