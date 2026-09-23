//go:build windows

package agent

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

type cursorFileRenameInfo struct {
	ReplaceIfExists uint32
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [1]uint16
}

func renameRootedCursorEntry(root *os.Root, oldName, newName string) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	oldUnicode, err := windows.NewNTUnicodeString(oldName)
	if err != nil {
		return err
	}
	attributes := &windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(dir.Fd()), ObjectName: oldUnicode}
	attributes.Length = uint32(unsafe.Sizeof(*attributes))
	var status windows.IO_STATUS_BLOCK
	var allocation int64
	var source windows.Handle
	if err := windows.NtCreateFile(&source, windows.DELETE|windows.FILE_READ_ATTRIBUTES, attributes, &status,
		&allocation, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT, 0, 0); err != nil {
		return err
	}
	defer windows.CloseHandle(source)
	name, err := windows.UTF16FromString(newName)
	if err != nil {
		return err
	}
	nameBytes := (len(name) - 1) * 2
	var layout cursorFileRenameInfo
	buffer := make([]byte, int(unsafe.Offsetof(layout.FileName))+nameBytes)
	info := (*cursorFileRenameInfo)(unsafe.Pointer(&buffer[0]))
	info.RootDirectory = windows.Handle(dir.Fd())
	info.FileNameLength = uint32(nameBytes)
	copy(unsafe.Slice(&info.FileName[0], len(name)-1), name[:len(name)-1])
	return windows.SetFileInformationByHandle(source, windows.FileRenameInfo, &buffer[0], uint32(len(buffer)))
}
