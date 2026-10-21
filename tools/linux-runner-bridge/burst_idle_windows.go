package main

import "os/exec"

func burstWorkerIdle() int { return 1 }

func stopBurstIdleListener() int { return 1 }

func runRunnerConfiguration(c *exec.Cmd, _ bool) error { return c.Run() }
