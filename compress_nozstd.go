//go:build timberjack_nozstd

package timberjack

import (
	"errors"
	"io"
)

// zstdAvailable reports whether zstd support is compiled in.
// It is false because this binary was built with -tags timberjack_nozstd.
const zstdAvailable = false

// newZstdWriter always fails; zstd support was compiled out.
func newZstdWriter(io.Writer) (io.WriteCloser, error) {
	return nil, errors.New("zstd support not compiled in (built with timberjack_nozstd)")
}
