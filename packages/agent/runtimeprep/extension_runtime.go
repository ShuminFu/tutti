package runtimeprep

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var extensionRuntimeEnvName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
var extensionRuntimeConfigKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

type ExtensionRuntimePreparer struct{}

func (ExtensionRuntimePreparer) Provider() string {
	return ""
}

func (ExtensionRuntimePreparer) Prepare(ctx context.Context, input ProviderPrepareInput) (ProviderPrepareResult, error) {
	if input.ExtensionRuntimePrep == nil {
		return InstructionFilePreparer{}.Prepare(ctx, input)
	}
	if err := ValidateExtensionRuntimePrep(*input.ExtensionRuntimePrep); err != nil {
		return ProviderPrepareResult{}, err
	}
	if strings.TrimSpace(input.ExtensionRuntimePrep.SessionInstructionsFile) == "" {
		if err := writeExtensionRuntimeInstructions(input); err != nil {
			return ProviderPrepareResult{}, err
		}
	}
	if input.ExtensionRuntimePrep.Home == nil {
		skillRoots, err := cwdExtensionSkillRoots(input.ExtensionSkillRoots)
		if err != nil {
			return ProviderPrepareResult{}, err
		}
		if err := materializeExtensionRuntimeSkills(input, skillRoots, true); err != nil {
			return ProviderPrepareResult{}, err
		}
		return ProviderPrepareResult{Cwd: input.Cwd}, nil
	}
	env, err := prepareExtensionRuntimeHome(input, *input.ExtensionRuntimePrep.Home)
	if err != nil {
		return ProviderPrepareResult{}, err
	}
	if err := writeExtensionSessionInstructions(input, *input.ExtensionRuntimePrep.Home); err != nil {
		return ProviderPrepareResult{}, err
	}
	envs := []string{env}
	if endpoint, declaration := input.ModelEndpoint, input.ExtensionRuntimePrep.ModelEndpoint; ExtensionModelEndpointApplies(endpoint, declaration) {
		envs = append(envs, strings.TrimSpace(declaration.APIKeyEnv)+"="+endpoint.APIKey)
	}
	return ProviderPrepareResult{
		Cwd: input.Cwd,
		Env: envs,
	}, nil
}

func writeExtensionSessionInstructions(input ProviderPrepareInput, home ExtensionRuntimeHome) error {
	fileName := strings.TrimSpace(input.ExtensionRuntimePrep.SessionInstructionsFile)
	if fileName == "" {
		return nil
	}
	policy, err := tuttiCLIPolicy(input.PrepareInput)
	if err != nil {
		return err
	}
	sessionHome := filepath.Join(input.RuntimeRoot, filepath.FromSlash(strings.TrimSpace(home.DirName)))
	filePath := filepath.Join(sessionHome, filepath.FromSlash(fileName))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return fmt.Errorf("create extension session instructions dir: %w", err)
	}
	if err := os.WriteFile(filePath, []byte(policy+"\n"), 0o600); err != nil {
		return fmt.Errorf("write extension session instructions: %w", err)
	}
	if input.Manifest != nil {
		input.Manifest.RecordManagedFile(filePath, "provider-session-instructions", true)
	}
	return nil
}

func writeExtensionRuntimeInstructions(input ProviderPrepareInput) error {
	fileName := strings.TrimSpace(input.ExtensionRuntimePrep.InstructionsFile)
	if fileName == "" {
		fileName = "AGENTS.md"
	}
	path := filepath.Join(input.Cwd, fileName)
	policy, err := tuttiCLIPolicy(input.PrepareInput)
	if err != nil {
		return err
	}
	writeResult, err := input.Store.WriteManagedBlock(path, policy)
	if err != nil {
		return err
	}
	if input.Manifest != nil {
		input.Manifest.RecordManagedFile(path, "provider-instructions", writeResult.Created)
	}
	return nil
}

func prepareExtensionRuntimeHome(input ProviderPrepareInput, home ExtensionRuntimeHome) (string, error) {
	sessionHome := filepath.Join(input.RuntimeRoot, filepath.FromSlash(strings.TrimSpace(home.DirName)))
	if err := os.MkdirAll(sessionHome, 0o700); err != nil {
		return "", fmt.Errorf("create extension runtime home: %w", err)
	}

	sourceHome := resolveExtensionRuntimeSourceHome(home)
	userConfig, err := copyExtensionRuntimeHomeFiles(sourceHome, sessionHome, home)
	if err != nil {
		return "", err
	}
	if err := writeExtensionRuntimeManagedFiles(sessionHome, home.ManagedFiles); err != nil {
		return "", err
	}
	externalDirs, err := extensionRuntimeExternalDirs(input, sourceHome, home)
	if err != nil {
		return "", err
	}
	if err := writeExtensionRuntimeConfig(
		filepath.Join(sessionHome, filepath.FromSlash(home.ConfigFile)),
		userConfig,
		externalDirs,
		home,
		input.ModelEndpoint,
		input.ExtensionRuntimePrep.ModelEndpoint,
	); err != nil {
		return "", err
	}
	if input.Manifest != nil {
		input.Manifest.RecordManagedFile(sessionHome, "provider-extension-home", true)
	}
	return strings.TrimSpace(home.EnvVar) + "=" + sessionHome, nil
}

func resolveExtensionRuntimeSourceHome(home ExtensionRuntimeHome) string {
	if sourceEnv := strings.TrimSpace(home.SourceEnvVar); sourceEnv != "" {
		if v := strings.TrimSpace(os.Getenv(sourceEnv)); v != "" {
			return v
		}
	}
	if rel := strings.TrimSpace(home.SourceDefaultRel); rel != "" {
		if userHome, err := os.UserHomeDir(); err == nil && userHome != "" {
			return filepath.Join(userHome, filepath.FromSlash(rel))
		}
	}
	return ""
}

func copyExtensionRuntimeHomeFiles(sourceHome string, sessionHome string, home ExtensionRuntimeHome) ([]byte, error) {
	var config []byte
	if sourceHome == "" {
		return nil, nil
	}
	configFile := filepath.Clean(filepath.FromSlash(strings.TrimSpace(home.ConfigFile)))
	if configFile != "." && configFile != "" {
		data, err := os.ReadFile(filepath.Join(sourceHome, configFile))
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read extension runtime %s: %w", configFile, err)
		}
		if err == nil {
			config = data
		}
	}
	for _, file := range home.CopyFiles {
		name := filepath.Clean(filepath.FromSlash(strings.TrimSpace(file)))
		if name == configFile {
			continue
		}
		src := filepath.Join(sourceHome, name)
		if err := copyExtensionRuntimeHomeFile(src, filepath.Join(sessionHome, name)); err != nil {
			return nil, err
		}
	}
	return config, nil
}

func copyExtensionRuntimeHomeFile(src string, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read extension runtime %s: %w", filepath.Base(src), err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("create extension runtime home subdir: %w", err)
	}
	return os.WriteFile(dst, data, 0o600)
}

func writeExtensionRuntimeManagedFiles(sessionHome string, files []ExtensionRuntimeManagedFile) error {
	for _, file := range files {
		dst := filepath.Join(sessionHome, filepath.Clean(filepath.FromSlash(strings.TrimSpace(file.Path))))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return fmt.Errorf("create extension runtime managed file dir: %w", err)
		}
		if err := os.WriteFile(dst, []byte(file.Content), 0o600); err != nil {
			return fmt.Errorf("write extension runtime managed file: %w", err)
		}
	}
	return nil
}

func extensionRuntimeExternalDirs(input ProviderPrepareInput, sourceHome string, home ExtensionRuntimeHome) ([]string, error) {
	externalDirs := []string{}
	if home.IncludeSkillRoots {
		skillRoots, err := extensionRuntimeSkillRoots(input, input.ExtensionSkillRoots)
		if err != nil {
			return nil, err
		}
		if err := materializeExtensionRuntimeSkills(input, skillRoots, false); err != nil {
			return nil, err
		}
		for _, root := range skillRoots {
			externalDirs = appendUniquePath(externalDirs, root)
		}
	}
	if home.IncludeUserHomeDir && sourceHome != "" {
		userSkillDir := strings.TrimSpace(home.UserHomeSkillDir)
		if userSkillDir == "" {
			userSkillDir = "skills"
		}
		globalSkills := filepath.Join(sourceHome, filepath.FromSlash(userSkillDir))
		if info, err := os.Stat(globalSkills); err == nil && info.IsDir() {
			externalDirs = appendUniquePath(externalDirs, globalSkills)
		} else if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect extension runtime user skill dir: %w", err)
		}
	}
	return externalDirs, nil
}

func extensionRuntimeSkillRoots(input ProviderPrepareInput, declaredRoots []string) ([]string, error) {
	roots := make([]string, 0, len(declaredRoots))
	for _, root := range declaredRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if err := validateExtensionRuntimeRelPath(root, "extension runtime skill root"); err != nil {
			return nil, err
		}
		root = filepath.Clean(filepath.FromSlash(root))
		roots = appendUniquePath(roots, filepath.Join(input.RuntimeRoot, "extension-skills", root))
	}
	return roots, nil
}

func materializeExtensionRuntimeSkills(input ProviderPrepareInput, roots []string, fallback bool) error {
	skillRoots := append([]string(nil), roots...)
	if len(skillRoots) == 0 && fallback {
		if root := providerSkillRoot(input.Cwd, input.Provider); root != "" {
			skillRoots = []string{root}
		}
	}
	for _, skillRoot := range skillRoots {
		if !filepath.IsAbs(skillRoot) {
			skillRoot = filepath.Join(input.Cwd, skillRoot)
		}
		skillPaths, err := installProviderNativeSkillsStable(skillRoot, input.PrepareInput)
		if err != nil {
			return err
		}
		if input.Manifest != nil {
			for _, skillPath := range skillPaths {
				input.Manifest.RecordManagedFile(skillPath, "provider-skill", true)
			}
		}
	}
	return nil
}

func writeExtensionRuntimeConfig(
	path string,
	userConfig []byte,
	externalDirs []string,
	home ExtensionRuntimeHome,
	endpoint *ModelEndpointConfig,
	declaration *ExtensionModelEndpoint,
) error {
	if strings.TrimSpace(home.ConfigFile) == "" {
		return nil
	}
	config := string(userConfig)
	if strings.TrimSpace(home.ConfigFormat) == "json" {
		if len(externalDirs) > 0 {
			return errors.New("extension runtime JSON config does not support external skill dirs")
		}
		var err error
		config, err = mergeJSONExtensionRuntimeConfig(config, endpoint, declaration)
		if err != nil {
			return err
		}
		if strings.TrimSpace(config) == "" {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("create extension runtime config dir: %w", err)
		}
		return os.WriteFile(path, []byte(config), 0o600)
	}
	if len(home.ExternalDirsKey) > 0 {
		var err error
		config, err = mergeYAMLStringList(config, home.ExternalDirsKey, externalDirs)
		if err != nil {
			return err
		}
	}
	if ExtensionModelEndpointApplies(endpoint, declaration) {
		var err error
		values := []yamlStringValue{
			{Path: declaration.ConfigKeys.Provider, Value: strings.TrimSpace(declaration.ProviderValue)},
			{Path: declaration.ConfigKeys.Model, Value: strings.TrimSpace(endpoint.Model)},
			{Path: declaration.ConfigKeys.BaseURL, Value: strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")},
		}
		if len(declaration.ConfigKeys.APIKeyEnv) > 0 {
			values = append(values, yamlStringValue{
				Path:  declaration.ConfigKeys.APIKeyEnv,
				Value: strings.TrimSpace(declaration.APIKeyEnv),
			})
		}
		if len(declaration.ConfigKeys.WireAPI) > 0 {
			wireAPIValue := strings.TrimSpace(declaration.WireAPIConfigValue)
			if wireAPIValue == "" {
				wireAPIValue = strings.TrimSpace(declaration.WireAPI)
			}
			values = append(values, yamlStringValue{
				Path:  declaration.ConfigKeys.WireAPI,
				Value: wireAPIValue,
			})
		}
		config, err = mergeYAMLStringValues(config, values)
		if err != nil {
			return err
		}
		if len(declaration.ConfigKeys.Models) > 0 {
			config, err = mergeYAMLModelEndpointCatalog(config, declaration.ConfigKeys.Models, endpoint.Models)
			if err != nil {
				return err
			}
		}
	}
	if strings.TrimSpace(config) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create extension runtime config dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		return fmt.Errorf("write extension runtime config: %w", err)
	}
	return nil
}

func ValidateExtensionRuntimePrep(prep ExtensionRuntimePrep) error {
	if file := strings.TrimSpace(prep.InstructionsFile); file != "" {
		if err := validateExtensionRuntimeRelPath(file, "extension runtime instructions file"); err != nil {
			return err
		}
	}
	if file := strings.TrimSpace(prep.SessionInstructionsFile); file != "" {
		if err := validateExtensionRuntimeRelPath(file, "extension runtime session instructions file"); err != nil {
			return err
		}
	}
	if prep.Home == nil {
		if strings.TrimSpace(prep.SessionInstructionsFile) != "" {
			return errors.New("extension session instructions require a runtime home")
		}
		if prep.ModelEndpoint != nil {
			return errors.New("extension model endpoint requires a runtime home")
		}
		return nil
	}
	home := *prep.Home
	if !extensionRuntimeEnvName.MatchString(strings.TrimSpace(home.EnvVar)) {
		return errors.New("extension runtime home env is unsupported")
	}
	if err := validateExtensionRuntimeRelPath(home.DirName, "extension runtime home dir"); err != nil {
		return err
	}
	if sourceEnv := strings.TrimSpace(home.SourceEnvVar); sourceEnv != "" && !extensionRuntimeEnvName.MatchString(sourceEnv) {
		return errors.New("extension runtime source env is unsupported")
	}
	if sourceRel := strings.TrimSpace(home.SourceDefaultRel); sourceRel != "" {
		if err := validateExtensionRuntimeRelPath(sourceRel, "extension runtime source default path"); err != nil {
			return err
		}
	}
	configFormat := strings.TrimSpace(home.ConfigFormat)
	if configFormat != "" && configFormat != "yaml" && configFormat != "json" {
		return errors.New("extension runtime config format is unsupported")
	}
	for _, file := range home.CopyFiles {
		if err := validateExtensionRuntimeRelPath(file, "extension runtime copy file"); err != nil {
			return err
		}
	}
	for _, file := range home.ManagedFiles {
		if err := validateExtensionRuntimeRelPath(file.Path, "extension runtime managed file"); err != nil {
			return err
		}
	}
	if configFile := strings.TrimSpace(home.ConfigFile); configFile != "" {
		if err := validateExtensionRuntimeRelPath(configFile, "extension runtime config file"); err != nil {
			return err
		}
	}
	if len(home.ExternalDirsKey) > 0 && !slices.Equal(home.ExternalDirsKey, []string{"skills", "external_dirs"}) {
		return errors.New("extension runtime external dirs key is unsupported")
	}
	if userSkillDir := strings.TrimSpace(home.UserHomeSkillDir); userSkillDir != "" {
		if err := validateExtensionRuntimeRelPath(userSkillDir, "extension runtime user skill dir"); err != nil {
			return err
		}
	}
	if prep.ModelEndpoint != nil {
		endpoint := *prep.ModelEndpoint
		if strings.TrimSpace(home.ConfigFile) == "" || (configFormat != "yaml" && configFormat != "json") {
			return errors.New("extension model endpoint requires a YAML or JSON config file")
		}
		if strings.TrimSpace(endpoint.Protocol) != "openai" || strings.TrimSpace(endpoint.WireAPI) != "chat" {
			return errors.New("extension model endpoint protocol is unsupported")
		}
		if !extensionRuntimeEnvName.MatchString(strings.TrimSpace(endpoint.APIKeyEnv)) {
			return errors.New("extension model endpoint API key env is unsupported")
		}
		if strings.TrimSpace(endpoint.ProviderValue) == "" {
			return errors.New("extension model endpoint provider value is required")
		}
		paths := [][]string{endpoint.ConfigKeys.Provider, endpoint.ConfigKeys.Model, endpoint.ConfigKeys.BaseURL}
		for _, optionalPath := range [][]string{endpoint.ConfigKeys.APIKeyEnv, endpoint.ConfigKeys.WireAPI, endpoint.ConfigKeys.Models} {
			if len(optionalPath) > 0 {
				paths = append(paths, optionalPath)
			}
		}
		for _, keyPath := range paths {
			if err := validateExtensionModelEndpointKeyPath(keyPath); err != nil {
				return err
			}
		}
		for i := range paths {
			for j := i + 1; j < len(paths); j++ {
				if slices.Equal(paths[i], paths[j]) {
					return errors.New("extension model endpoint config keys must be distinct")
				}
			}
		}
	}
	return nil
}

// ExtensionModelEndpointApplies reports whether a validated host endpoint
// satisfies an extension's declared session mapping.
func ExtensionModelEndpointApplies(endpoint *ModelEndpointConfig, declaration *ExtensionModelEndpoint) bool {
	if endpoint == nil || declaration == nil || !endpoint.valid() || strings.TrimSpace(endpoint.Model) == "" {
		return false
	}
	wireAPI := strings.TrimSpace(endpoint.WireAPI)
	if wireAPI == "" {
		wireAPI = "chat"
	}
	return strings.TrimSpace(endpoint.Protocol) == strings.TrimSpace(declaration.Protocol) &&
		wireAPI == strings.TrimSpace(declaration.WireAPI)
}

func validateExtensionModelEndpointKeyPath(keyPath []string) error {
	if len(keyPath) == 0 || len(keyPath) > 4 {
		return errors.New("extension model endpoint config key path is unsupported")
	}
	for _, key := range keyPath {
		if !extensionRuntimeConfigKey.MatchString(strings.TrimSpace(key)) {
			return errors.New("extension model endpoint config key path is unsupported")
		}
	}
	return nil
}

func validateExtensionRuntimeRelPath(value string, label string) error {
	trimmed := strings.TrimSpace(value)
	portable := strings.ReplaceAll(trimmed, `\`, "/")
	cleaned := filepath.Clean(filepath.FromSlash(trimmed))
	// Runtime descriptors use slash-separated paths, so reject absolute
	// spellings from both the descriptor syntax and the host OS.
	if cleaned == "." || cleaned == "" || isPortableAbsolutePath(trimmed, portable) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s must be a safe relative path", label)
	}
	return nil
}

func isPortableAbsolutePath(trimmed, portable string) bool {
	return filepath.IsAbs(filepath.FromSlash(trimmed)) || path.IsAbs(portable) || portableDrivePath(portable)
}

func portableDrivePath(value string) bool {
	return len(value) >= 2 && value[1] == ':' &&
		((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z'))
}

func appendUniquePath(paths []string, path string) []string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return paths
	}
	if slices.Contains(paths, path) {
		return paths
	}
	return append(paths, path)
}
