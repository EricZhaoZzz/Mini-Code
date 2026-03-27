//go:build !windows

package tools

import (
	"os"

	"github.com/google/renameio/v2"
)

func writeFileAtomically(path string, data []byte, perm os.FileMode) error {
	return renameio.WriteFile(path, data, perm)
}
