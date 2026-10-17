//go:build !timberjack_nozstd

package timberjack

import (
	"io"

	"github.com/klauspost/compress/zstd"
)

// zstdAvailable reports whether zstd support is compiled in.
// Build with -tags timberjack_nozstd to drop the github.com/klauspost/compress dependency.
const zstdAvailable = true

// newZstdWriter returns a zstd encoder writing to w.
func newZstdWriter(w io.Writer) (io.WriteCloser, error) {
	return zstd.NewWriter(w)
}
