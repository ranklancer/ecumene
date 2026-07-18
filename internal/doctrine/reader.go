package doctrine

import "bytes"

// bytesReader is a tiny indirection so the parse path is easy to test.
func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
