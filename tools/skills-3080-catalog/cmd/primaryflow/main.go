package main

import (
	"cc-pc-tools-rs/skills-3080-catalog/internal/primaryflow"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
)

func run() error {
	var v any
	var e error
	switch {
	case len(os.Args) == 2 && os.Args[1] == "prepare":
		v, e = primaryflow.Prepare()
	case len(os.Args) == 2 && os.Args[1] == "verify":
		v, e = primaryflow.VerifyProtection()
	case len(os.Args) == 6 && os.Args[1] == "slice":
		a, x := strconv.Atoi(os.Args[4])
		n, y := strconv.Atoi(os.Args[5])
		if x != nil || y != nil {
			return errors.New("integer cursor required")
		}
		v, e = primaryflow.Slice(os.Args[2], os.Args[3], a, n)
	case len(os.Args) == 7 && os.Args[1] == "render":
		a, x := strconv.Atoi(os.Args[4])
		first, y := strconv.Atoi(os.Args[5])
		through, z := strconv.Atoi(os.Args[6])
		if x != nil || y != nil || z != nil {
			return errors.New("integer ordinal and physical range required")
		}
		v, e = primaryflow.Render(os.Args[2], os.Args[3], a, first, through)
	case len(os.Args) == 7 && os.Args[1] == "members":
		a, x := strconv.Atoi(os.Args[4])
		first, y := strconv.Atoi(os.Args[5])
		count, z := strconv.Atoi(os.Args[6])
		if x != nil || y != nil || z != nil {
			return errors.New("integer member metadata cursor required")
		}
		v, e = primaryflow.Members(os.Args[2], os.Args[3], a, first, count)
	default:
		return errors.New("prepare | verify | slice <frozen-plan> <SHA> <first> <count1..16> | render <frozen-plan> <SHA> <ordinal> <firstLine> <throughLine> | members <frozen-plan> <SHA> <ordinal> <first> <count1..32>")
	}
	if e != nil {
		return e
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
