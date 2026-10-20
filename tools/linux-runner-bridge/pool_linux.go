package main

import "errors"

func burstHostCapacity() error {
	return errors.New("burst admission runs only through the Windows host")
}
