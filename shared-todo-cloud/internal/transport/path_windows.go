//go:build windows

package transport

import "strings"

func pathsEqual(a, b string) bool { return strings.EqualFold(a, b) }
