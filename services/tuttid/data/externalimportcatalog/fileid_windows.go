//go:build windows

package externalimportcatalog

import "os"

func fileIdentity(info os.FileInfo) string {
	return ""
}
