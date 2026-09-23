//go:build darwin

package agent

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameRootedCursorEntry(root *os.Root, oldName, newName string) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return unix.RenameatxNp(int(dir.Fd()), oldName, int(dir.Fd()), newName, unix.RENAME_EXCL)
}
