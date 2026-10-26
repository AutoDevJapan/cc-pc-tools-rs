//go:build !windows || !amd64

package main

func nativeInputPaths() (inputPaths, error)                          { return inputPaths{}, errInputSource }
func startInputChild(string, []string, []string) (inputChild, error) { return nil, errInputSource }
