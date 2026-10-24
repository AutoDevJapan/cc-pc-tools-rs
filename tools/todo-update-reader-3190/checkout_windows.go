package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"strconv"
)

// objectName is the SHA-1 name Git gives an object of kind with data.
func objectName(kind string, data []byte) string {
	h := sha1.New()
	h.Write([]byte(kind + " " + strconv.Itoa(len(data)) + "\x00"))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// gitBlobID is the name Git gives data stored as a blob. It ties bytes to an
// entry of the fixed tree; SHA-256 pins the same bytes separately.
func gitBlobID(data []byte) string { return objectName("blob", data) }

// checkedOut reports whether working bytes are exactly a fixed blob as Git
// writes it out on this PC: the blob itself or, under core.autocrlf=true, the
// blob with every LF written as CRLF. Git converts only text, so bytes with a
// NUL, a lone CR or a lone LF are never accepted as a conversion. Only the
// reader's pinned bytes are hashed; Git never opens the working file.
func checkedOut(data []byte, blob string) bool {
	if gitBlobID(data) == blob {
		return true
	}
	pairs := bytes.Count(data, []byte("\r\n"))
	if pairs == 0 || bytes.IndexByte(data, 0) >= 0 ||
		bytes.Count(data, []byte("\r")) != pairs || bytes.Count(data, []byte("\n")) != pairs {
		return false
	}
	return gitBlobID(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))) == blob
}
