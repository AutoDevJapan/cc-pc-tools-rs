package officialci3893

import (
	"archive/zip"
	"bytes"
	"io"
	"sort"
	"strings"
)

const maxExpanded = 1 << 30
const maxEntries = 16000

// Use a single, conservative Windows spelling. Do not clean an unsafe name
// into a safe one. ASCII avoids normalization/case ambiguities for this asset.
func safeName(name string) bool {
	if name == "" || len(name) > 220 || strings.ContainsAny(name, `\:<>"|?*`) {
		return false
	}
	for _, c := range []byte(name) {
		if c < 32 || c >= 127 {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || strings.TrimRight(part, " .") != part {
			return false
		}
		base, _, _ := strings.Cut(strings.ToUpper(part), ".")
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CONIN$" || base == "CONOUT$" {
			return false
		}
		if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9' {
			return false
		}
	}
	return true
}

func unpack(raw []byte) ([]Payload, error) {
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(z.File) == 0 || len(z.File) > maxEntries {
		return nil, ErrDenied
	}
	seen := map[string]bool{}
	kinds := map[string]bool{} // true is a directory
	spellings := map[string]string{}
	var total uint64
	var files []Payload
	for _, f := range z.File {
		dir := f.FileInfo().IsDir()
		name := f.Name
		if dir {
			name = strings.TrimSuffix(name, "/")
		}
		if !safeName(name) || (f.Mode().Type() != 0 && !dir) || f.Flags&1 != 0 {
			return nil, ErrDenied
		}
		key := strings.ToLower(name)
		if previous, ok := spellings[key]; ok && previous != name {
			return nil, ErrDenied
		}
		spellings[key] = name
		if seen[key] {
			return nil, ErrDenied
		}
		seen[key] = true
		if prior, ok := kinds[key]; ok && (!prior || !dir) {
			return nil, ErrDenied
		}
		kinds[key] = dir
		parts := strings.Split(key, "/")
		for i := 1; i < len(parts); i++ {
			parent := strings.Join(parts[:i], "/")
			spelling := strings.Join(strings.Split(name, "/")[:i], "/")
			if previous, ok := spellings[parent]; ok && previous != spelling {
				return nil, ErrDenied
			}
			spellings[parent] = spelling
			if directory, ok := kinds[parent]; ok && !directory {
				return nil, ErrDenied
			}
			kinds[parent] = true
		}
		if dir {
			if f.UncompressedSize64 != 0 {
				return nil, ErrDenied
			}
			continue
		}
		if f.UncompressedSize64 > maxExpanded || total > maxExpanded-f.UncompressedSize64 {
			return nil, ErrDenied
		}
		total += f.UncompressedSize64
		r, err := f.Open()
		if err != nil {
			return nil, ErrDenied
		}
		b, readErr := io.ReadAll(io.LimitReader(r, int64(f.UncompressedSize64)+1))
		closeErr := r.Close()
		if readErr != nil || closeErr != nil || uint64(len(b)) != f.UncompressedSize64 {
			return nil, ErrDenied
		}
		files = append(files, Payload{name, b})
	}
	if len(files) == 0 {
		return nil, ErrDenied
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}
