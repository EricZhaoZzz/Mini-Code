package tools

import "os"

func writeFileAtomically(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}
