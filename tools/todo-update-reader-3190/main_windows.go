// Command todo-update-reader-3190 qualifies exactly one software update of
// D:/CC/Tools/todo/todo.exe for ToDo 3190. It is a non-distributed,
// read-only reader: it replaces nothing, reads only software and build
// evidence, and never grants data access. See docs/todo-update-reader-3190.md.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const purposeFlag = "verify-todo-runtime-update"

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage of todo-update-reader-3190:")
	fmt.Fprintln(w, "  -verify-todo-runtime-update string")
	fmt.Fprintln(w, "    \tqualify the one fixed ToDo3190 update of D:/CC/Tools/todo/todo.exe;")
	fmt.Fprintln(w, "    \treads software and build evidence only, replaces nothing, grants no data access")
}

// runCLI accepts exactly one purpose flag with one absolute path. Any other
// shape is refused before verification starts, so no caller input can add a
// manifest, digest, source, root or grant.
func runCLI(args []string, stdout, stderr io.Writer, verify func(string) (qualification, string)) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	if len(args) == 1 {
		switch args[0] {
		case "-h", "-help", "--help":
			usage(stderr)
			return 0
		}
	}
	result, reason := qualification{}, "argument"
	if path, ok := parsePurpose(args); ok {
		result, reason = verify(path)
	}
	if !result.Qualified {
		// Only a fixed reason code; never a digest, path content or receipt text.
		fmt.Fprintf(stderr, "todo-update-reader-3190: not qualified: %s\n", reason)
		_ = json.NewEncoder(stdout).Encode(map[string]bool{"qualified": false})
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return 1
	}
	return 0
}

func parsePurpose(args []string) (string, bool) {
	path := ""
	switch {
	case len(args) == 2 && (args[0] == "--"+purposeFlag || args[0] == "-"+purposeFlag):
		path = args[1]
	case len(args) == 1:
		for _, prefix := range []string{"--" + purposeFlag + "=", "-" + purposeFlag + "="} {
			if strings.HasPrefix(args[0], prefix) {
				path = strings.TrimPrefix(args[0], prefix)
			}
		}
	}
	return path, path != "" && filepath.IsAbs(path)
}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr, func(path string) (qualification, string) {
		return verifyUpdate(path, productionProfile, productionEnvironment())
	}))
}
