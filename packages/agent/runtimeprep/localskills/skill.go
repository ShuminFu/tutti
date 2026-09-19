package localskills

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	// maxSkillReadBytes bounds how much of a SKILL.md is read. Only the
	// frontmatter is parsed, so a large body is never loaded whole.
	maxSkillReadBytes = 1 << 20
	// maxFrontmatterBytes bounds the YAML header itself.
	maxFrontmatterBytes = 64 << 10
	// maxSidecarBytes bounds agents/openai.yaml.
	maxSidecarBytes = 64 << 10
	// maxSkillNameRunes bounds a skill name in characters.
	maxSkillNameRunes = 64

	frontmatterDelimiter = "---"
	agentsSubdirName     = "agents"
	openAIPolicyFile     = "openai.yaml"
	// utf8BOM is the UTF-8 byte order mark a SKILL.md may open with.
	utf8BOM = "\ufeff"
)

// Skill is one discovered skill.
//
// Path is the SKILL.md path. SourceKind and PluginName are root facts filled in
// by Discover, so a skill read directly with Read leaves both empty. Automatic
// reports whether the model may invoke the skill on its own; UserInvocable
// reports whether a user may select it explicitly. Both default to true and
// can be turned off from the frontmatter (or, for Automatic, from the
// agents/openai.yaml policy beside the skill).
type Skill struct {
	Name          string
	Description   string
	Path          string
	SourceKind    string
	PluginName    string
	Automatic     bool
	UserInvocable bool
}

// Identity is the catalog key of a skill in its namespace: "pluginName:name"
// for a plugin-scoped skill, and the bare name for an unqualified one. Because
// the key carries the namespace, a plugin skill named "review" never collides
// with an unqualified skill named "review".
func Identity(skill Skill) string {
	name := strings.TrimSpace(skill.Name)
	pluginName := strings.TrimSpace(skill.PluginName)
	if pluginName == "" {
		return name
	}
	return pluginName + ":" + name
}

// Read parses the SKILL.md at path.
//
// Only a bounded prefix of the file is read, so an oversized body is never
// loaded whole. A UTF-8 BOM and CRLF line endings are accepted. The frontmatter
// is parsed as real YAML: block scalars fold as YAML says they do, and a
// duplicated key is an error rather than a silent last-one-wins.
//
// name and description must both be non-empty strings, and name must be a
// portable skill name: letters or digits (any Unicode letter, so Chinese names
// are accepted) first, then letters, digits, underscore, dot, or hyphen, at
// most 64 characters, with no slash, whitespace, or control character. A name
// that would read as another YAML type has to be quoted, so a skill named "1"
// is written name: "1".
//
// Automatic and UserInvocable default to true. The frontmatter
// "disable-model-invocation: true" and an agents/openai.yaml beside the file
// carrying policy.allow_implicit_invocation: false each set Automatic to false
// while leaving the skill explicitly usable; "user-invocable: false" sets
// UserInvocable to false.
//
// Every failure names the offending file and the reason.
func Read(path string) (Skill, error) {
	target := strings.TrimSpace(path)
	if target == "" {
		return Skill{}, errors.New("skill path is empty")
	}
	content, err := readBoundedFile(target, maxSkillReadBytes)
	if err != nil {
		return Skill{}, err
	}
	header, err := frontmatterHeader(content)
	if err != nil {
		return Skill{}, fmt.Errorf("%s: %w", target, err)
	}
	mapping, err := frontmatterMapping(header)
	if err != nil {
		return Skill{}, fmt.Errorf("%s: %w", target, err)
	}
	name, err := requiredString(mapping, "name")
	if err != nil {
		return Skill{}, fmt.Errorf("%s: %w", target, err)
	}
	if err := validateSkillName(name); err != nil {
		return Skill{}, fmt.Errorf("%s: %w", target, err)
	}
	description, err := requiredString(mapping, "description")
	if err != nil {
		return Skill{}, fmt.Errorf("%s: %w", target, err)
	}
	disableModelInvocation, err := optionalBool(mapping, "disable-model-invocation")
	if err != nil {
		return Skill{}, fmt.Errorf("%s: %w", target, err)
	}
	userInvocable, err := optionalBool(mapping, "user-invocable")
	if err != nil {
		return Skill{}, fmt.Errorf("%s: %w", target, err)
	}

	skill := Skill{
		Name:          name,
		Description:   description,
		Path:          filepath.Clean(target),
		Automatic:     disableModelInvocation == nil || !*disableModelInvocation,
		UserInvocable: userInvocable == nil || *userInvocable,
	}
	allowImplicit, present, err := readOpenAIPolicy(filepath.Dir(skill.Path))
	if err != nil {
		return Skill{}, err
	}
	if present && !allowImplicit {
		skill.Automatic = false
	}
	return skill, nil
}

func readBoundedFile(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: skill metadata must be a regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return content, nil
}

// frontmatterHeader returns the YAML header between the opening and closing
// delimiter, after normalizing a leading BOM and line endings. A file that
// does not open with a delimiter is an error, as is a header that never closes
// or that grows past maxFrontmatterBytes.
func frontmatterHeader(content []byte) ([]byte, error) {
	content = bytes.TrimPrefix(content, []byte(utf8BOM))
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	content = bytes.ReplaceAll(content, []byte("\r"), []byte("\n"))
	lines := bytes.Split(content, []byte("\n"))
	start := -1
	for index, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if string(bytes.TrimSpace(line)) != frontmatterDelimiter {
			return nil, fmt.Errorf("missing %s frontmatter delimiter", frontmatterDelimiter)
		}
		start = index
		break
	}
	if start < 0 {
		return nil, fmt.Errorf("missing %s frontmatter delimiter", frontmatterDelimiter)
	}
	header := make([]byte, 0, 1024)
	for index := start + 1; index < len(lines); index++ {
		if string(bytes.TrimSpace(lines[index])) == frontmatterDelimiter {
			return header, nil
		}
		header = append(header, lines[index]...)
		header = append(header, '\n')
		if len(header) > maxFrontmatterBytes {
			return nil, fmt.Errorf("frontmatter exceeds %d bytes", maxFrontmatterBytes)
		}
	}
	return nil, fmt.Errorf(
		"unterminated %s frontmatter; the header must close within the first %d bytes",
		frontmatterDelimiter,
		maxSkillReadBytes,
	)
}

func frontmatterMapping(header []byte) (*yaml.Node, error) {
	if len(bytes.TrimSpace(header)) == 0 {
		return nil, errors.New("frontmatter is empty")
	}
	var document yaml.Node
	if err := yaml.Unmarshal(header, &document); err != nil {
		return nil, fmt.Errorf("invalid frontmatter YAML: %s", flattenYAMLError(err))
	}
	root := documentRoot(&document)
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("frontmatter must be a YAML mapping of name and description")
	}
	if err := checkDuplicateKeys(root); err != nil {
		return nil, err
	}
	return root, nil
}

func documentRoot(document *yaml.Node) *yaml.Node {
	if document == nil {
		return &yaml.Node{Kind: yaml.MappingNode}
	}
	if document.Kind == yaml.DocumentNode {
		if len(document.Content) == 0 {
			return &yaml.Node{Kind: yaml.MappingNode}
		}
		return document.Content[0]
	}
	return document
}

// checkDuplicateKeys rejects a mapping that defines the same key twice, at any
// depth. yaml.v3 decoding into a mapping would silently keep one of the two,
// which hides a broken skill instead of reporting it.
func checkDuplicateKeys(node *yaml.Node) error {
	return checkDuplicateKeysInto(node, map[*yaml.Node]struct{}{})
}

func checkDuplicateKeysInto(node *yaml.Node, visited map[*yaml.Node]struct{}) error {
	if node == nil {
		return nil
	}
	if _, ok := visited[node]; ok {
		return nil
	}
	visited[node] = struct{}{}
	switch node.Kind {
	case yaml.MappingNode:
		seen := make(map[string]int, len(node.Content)/2)
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index]
			label := strings.TrimSpace(key.Value)
			if firstLine, ok := seen[label]; ok {
				return fmt.Errorf(
					"duplicate frontmatter key %q at line %d, first defined at line %d",
					label,
					key.Line,
					firstLine,
				)
			}
			seen[label] = key.Line
			if err := checkDuplicateKeysInto(node.Content[index+1], visited); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			if err := checkDuplicateKeysInto(child, visited); err != nil {
				return err
			}
		}
	case yaml.AliasNode:
		return checkDuplicateKeysInto(node.Alias, visited)
	}
	return nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if strings.TrimSpace(mapping.Content[index].Value) == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

func requiredString(mapping *yaml.Node, key string) (string, error) {
	node := mappingValue(mapping, key)
	if node == nil {
		return "", fmt.Errorf("frontmatter %q is required", key)
	}
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return "", fmt.Errorf("frontmatter %q must be a string", key)
	}
	value := strings.TrimSpace(node.Value)
	if value == "" {
		return "", fmt.Errorf("frontmatter %q must not be empty", key)
	}
	return value, nil
}

// optionalBool returns nil when the key is absent, so callers can tell "unset"
// apart from an explicit false.
func optionalBool(mapping *yaml.Node, key string) (*bool, error) {
	node := mappingValue(mapping, key)
	if node == nil {
		return nil, nil
	}
	if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
		return nil, fmt.Errorf("frontmatter %q must be a boolean", key)
	}
	var value bool
	if err := node.Decode(&value); err != nil {
		return nil, fmt.Errorf("frontmatter %q must be a boolean", key)
	}
	return &value, nil
}

// validateSkillName enforces a portable name: letters or digits first, then
// letters, digits, underscore, dot, or hyphen. Unicode letters count, so a
// Chinese name is accepted, while a slash, whitespace, or control character is
// not.
func validateSkillName(name string) error {
	if utf8.RuneCountInString(name) > maxSkillNameRunes {
		return fmt.Errorf("skill name %q is longer than %d characters", name, maxSkillNameRunes)
	}
	for index, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("skill name %q contains a control character", name)
		}
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
		case index > 0 && (r == '_' || r == '.' || r == '-'):
		default:
			return fmt.Errorf(
				"skill name %q must start with a letter or digit and continue with letters, digits, underscore, dot, or hyphen",
				name,
			)
		}
	}
	return nil
}

// readOpenAIPolicy reads the optional agents/openai.yaml beside a skill and
// reports policy.allow_implicit_invocation. A missing file, or a file without
// that key, reports present=false rather than an error.
func readOpenAIPolicy(skillDir string) (value bool, present bool, err error) {
	path := filepath.Join(skillDir, agentsSubdirName, openAIPolicyFile)
	content, err := readBoundedFile(path, maxSidecarBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, false, nil
		}
		return false, false, err
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return false, false, nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return false, false, fmt.Errorf("%s: invalid YAML: %s", path, flattenYAMLError(err))
	}
	root := documentRoot(&document)
	if root.Kind != yaml.MappingNode {
		return false, false, fmt.Errorf("%s: must be a YAML mapping", path)
	}
	policy := mappingValue(root, "policy")
	if policy == nil {
		return false, false, nil
	}
	if policy.Kind != yaml.MappingNode {
		return false, false, fmt.Errorf("%s: policy must be a YAML mapping", path)
	}
	node := mappingValue(policy, "allow_implicit_invocation")
	if node == nil {
		return false, false, nil
	}
	if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
		return false, false, fmt.Errorf("%s: policy.allow_implicit_invocation must be a boolean", path)
	}
	var allowImplicit bool
	if err := node.Decode(&allowImplicit); err != nil {
		return false, false, fmt.Errorf("%s: policy.allow_implicit_invocation must be a boolean", path)
	}
	return allowImplicit, true, nil
}

// flattenYAMLError folds a possibly multi-line yaml.v3 error into one line so
// it reads as a single diagnostic.
func flattenYAMLError(err error) string {
	message := strings.TrimPrefix(err.Error(), "yaml: ")
	return strings.Join(strings.Fields(strings.ReplaceAll(message, "\n", " ")), " ")
}
