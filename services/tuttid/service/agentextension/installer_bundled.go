package agentextension

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	bundledRuntimeChecksumFile       = "SHA256SUMS"
	maxBundledRuntimeFiles           = 60_000
	maxBundledRuntimeBytes           = int64(1 << 30)
	deepSeekHarnessRuntimeChannelEnv = "TUTTI_AGENT_EXTENSION_DEEPSEEK_HARNESS_RUNTIME_CHANNEL"
)

func copyBundledRuntime(installation Installation, plan InstallPlan, destination *managedRuntimeDirectory) error {
	if plan.Runner != "bundled" || plan.Platform != runtimePlatform() {
		return errors.New("bundled runtime plan is unavailable for this platform")
	}
	if !installation.HasLocalPackageProvenance() || selectedBundledRuntimePackageName(installation) != plan.PackageName {
		return errors.New("bundled runtime plan does not match the trusted local package channel")
	}
	sourceDir := "runtime"
	if plan.PackageName == "bundled-runtime-previous" {
		sourceDir = "runtime-previous"
	}
	sourceRoot := filepath.Join(installation.PackageDir, sourceDir, plan.Platform)
	info, err := os.Lstat(sourceRoot)
	if err != nil {
		if plan.PackageName == "bundled-runtime-previous" && errors.Is(err, os.ErrNotExist) {
			return errors.New("rollback bundle unavailable for this platform")
		}
		return fmt.Errorf("open bundled runtime for %s: %w", plan.Platform, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("bundled runtime root must be an ordinary directory")
	}
	realPackage, err := filepath.EvalSymlinks(installation.PackageDir)
	if err != nil {
		return err
	}
	realSource, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil || !pathWithin(realSource, realPackage) {
		return errors.New("bundled runtime root escapes extension package")
	}
	expected, err := readBundledRuntimeChecksums(filepath.Join(sourceRoot, bundledRuntimeChecksumFile))
	if err != nil {
		return err
	}
	files, err := inspectBundledRuntimeFiles(sourceRoot)
	if err != nil {
		return err
	}
	if len(files) != len(expected) {
		return errors.New("bundled runtime checksum manifest does not cover every file")
	}
	for _, relative := range files {
		want, ok := expected[filepath.ToSlash(relative)]
		if !ok {
			return fmt.Errorf("bundled runtime checksum is missing %s", filepath.ToSlash(relative))
		}
		sourcePath := filepath.Join(sourceRoot, relative)
		file, err := os.Open(sourcePath)
		if err != nil {
			return err
		}
		openedInfo, statErr := file.Stat()
		pathInfo, lstatErr := os.Lstat(sourcePath)
		if statErr != nil || lstatErr != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !openedInfo.Mode().IsRegular() || !os.SameFile(openedInfo, pathInfo) {
			file.Close()
			return fmt.Errorf("bundled runtime file is unsafe: %s", filepath.ToSlash(relative))
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		if got := hex.EncodeToString(hash.Sum(nil)); got != want {
			return fmt.Errorf("bundled runtime checksum mismatch: %s", filepath.ToSlash(relative))
		}
	}
	for _, relative := range files {
		sourcePath := filepath.Join(sourceRoot, relative)
		info, err := os.Stat(sourcePath)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if info.Mode()&0o111 != 0 {
			mode = 0o700
		}
		target, err := destination.createFile(relative, mode)
		if err != nil {
			return err
		}
		source, err := os.Open(sourcePath)
		if err != nil {
			target.Close()
			return err
		}
		_, copyErr := io.Copy(target, source)
		closeErr := errors.Join(source.Close(), target.Close())
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
	}
	checksumTarget, err := destination.createFile(bundledRuntimeChecksumFile, 0o600)
	if err != nil {
		return err
	}
	checksumSource, err := os.Open(filepath.Join(sourceRoot, bundledRuntimeChecksumFile))
	if err != nil {
		checksumTarget.Close()
		return err
	}
	_, copyErr := io.Copy(checksumTarget, checksumSource)
	return errors.Join(copyErr, checksumSource.Close(), checksumTarget.Close())
}

func readBundledRuntimeChecksums(filePath string) (map[string]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("read bundled runtime checksums: %w", err)
	}
	defer file.Close()
	result := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		digest, relative, ok := strings.Cut(line, "  ")
		if !ok || len(digest) != 64 {
			return nil, errors.New("bundled runtime checksum manifest is invalid")
		}
		if _, err := hex.DecodeString(digest); err != nil || strings.ToLower(digest) != digest {
			return nil, errors.New("bundled runtime checksum digest is invalid")
		}
		if !safeBundledRuntimeRelativePath(relative) {
			return nil, errors.New("bundled runtime checksum path is unsafe")
		}
		if relative == bundledRuntimeChecksumFile {
			return nil, errors.New("bundled runtime checksum manifest cannot list itself")
		}
		if _, exists := result[relative]; exists {
			return nil, errors.New("bundled runtime checksum path is duplicated")
		}
		result[relative] = digest
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 || len(result) > maxBundledRuntimeFiles {
		return nil, errors.New("bundled runtime checksum manifest is empty or too large")
	}
	return result, nil
}

func inspectBundledRuntimeFiles(root string) ([]string, error) {
	files := []string{}
	var total int64
	err := filepath.WalkDir(root, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == root {
			return nil
		}
		relative, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("bundled runtime contains symlink: %s", filepath.ToSlash(relative))
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.ToSlash(relative) == bundledRuntimeChecksumFile {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("bundled runtime contains non-regular file: %s", filepath.ToSlash(relative))
		}
		total += info.Size()
		if total > maxBundledRuntimeBytes {
			return errors.New("bundled runtime exceeds size limit")
		}
		if !safeBundledRuntimeRelativePath(filepath.ToSlash(relative)) {
			return errors.New("bundled runtime contains unsafe path")
		}
		files = append(files, relative)
		if len(files) > maxBundledRuntimeFiles {
			return errors.New("bundled runtime file count exceeds limit")
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func safeBundledRuntimeRelativePath(relative string) bool {
	if relative == "" || strings.Contains(relative, "\\") || strings.HasPrefix(relative, "/") {
		return false
	}
	cleaned := path.Clean(relative)
	return cleaned == relative && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}
