//go:build !windows

package runtimeprep

import "os"

func exposeImportedProviderFile(source, target string, _ os.FileMode) error {
	return os.Symlink(source, target)
}
