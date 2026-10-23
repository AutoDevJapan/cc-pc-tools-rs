package main

// gitBlobID is the SHA-1 object name Git gives data stored as a blob.
func gitBlobID(data []byte) string { return "" }

// checkedOut reports whether working bytes are exactly a fixed blob as Git
// writes it out on this PC.
func checkedOut(data []byte, blob string) bool { return false }
