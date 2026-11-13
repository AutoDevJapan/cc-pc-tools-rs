// Synthetic JSON-lines producer for the opt-in Rust/bridge boundary test.
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 4 || os.Args[1] != "session" || os.Args[2] != "--config" {
		os.Exit(2)
	}
	input := bufio.NewScanner(os.Stdin)
	if !input.Scan() {
		os.Exit(2)
	}
	reply := os.Getenv("TODO_REMOTE_SYNTHETIC_REPLY")
	if reply == "" {
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, reply)
}
