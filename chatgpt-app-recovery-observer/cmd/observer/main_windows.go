package main

import (
	observer "github.com/AutoDevJapan/cc-pc-tools-rs/chatgpt-app-recovery-observer"
	"os"
)

func main() { os.Exit(observer.RunCLI(os.Args[1:], os.Stdout)) }
