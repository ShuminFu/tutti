package runtimeprep

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// RNDMASTER_EXTENSION_TOML_PREP is the managed DinTalDock source marker for
// extension runtimePrep configFormat=toml.
const RNDMASTER_EXTENSION_TOML_PREP = "RNDMASTER_EXTENSION_TOML_PREP"

func extensionTOMLPrepEnabled() bool {
	return RNDMASTER_EXTENSION_TOML_PREP != ""
}

func mergeTOMLExtensionRuntimeConfig(
	_ string,
	seed map[string]any,
	endpoint *ModelEndpointConfig,
	declaration *ExtensionModelEndpoint,
) (string, error) {
	if !extensionTOMLPrepEnabled() {
		return "", nil
	}
	doc := tomlTable{}
	if err := tomlMergeMap(doc, seed); err != nil {
		return "", err
	}
	if ExtensionModelEndpointApplies(endpoint, declaration) {
		if err := tomlApplyModelEndpoint(doc, endpoint, declaration); err != nil {
			return "", err
		}
	}
	if len(doc) == 0 {
		return "", nil
	}
	return encodeTOML(doc), nil
}

func tomlApplyModelEndpoint(doc tomlTable, endpoint *ModelEndpointConfig, declaration *ExtensionModelEndpoint) error {
	baseURL := strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")
	values := []struct {
		path  []string
		value any
	}{}
	if len(declaration.ConfigKeys.Provider) > 0 {
		values = append(values, struct {
			path  []string
			value any
		}{declaration.ConfigKeys.Provider, strings.TrimSpace(declaration.ProviderValue)})
	}
	if len(declaration.ConfigKeys.Model) > 0 {
		values = append(values, struct {
			path  []string
			value any
		}{declaration.ConfigKeys.Model, strings.TrimSpace(endpoint.Model)})
	}
	modelPrefix := declaration.ConfigKeys.Models
	perModel := len(modelPrefix) > 0
	if !perModel || !tomlPathHasPrefix(declaration.ConfigKeys.BaseURL, modelPrefix) {
		if len(declaration.ConfigKeys.BaseURL) > 0 {
			values = append(values, struct {
				path  []string
				value any
			}{declaration.ConfigKeys.BaseURL, baseURL})
		}
	}
	if !perModel || !tomlPathHasPrefix(declaration.ConfigKeys.APIKeyEnv, modelPrefix) {
		if len(declaration.ConfigKeys.APIKeyEnv) > 0 {
			values = append(values, struct {
				path  []string
				value any
			}{declaration.ConfigKeys.APIKeyEnv, strings.TrimSpace(declaration.APIKeyEnv)})
		}
	}
	if !perModel || !tomlPathHasPrefix(declaration.ConfigKeys.WireAPI, modelPrefix) {
		if len(declaration.ConfigKeys.WireAPI) > 0 {
			wireAPIValue := strings.TrimSpace(declaration.WireAPIConfigValue)
			if wireAPIValue == "" {
				wireAPIValue = strings.TrimSpace(declaration.WireAPI)
			}
			values = append(values, struct {
				path  []string
				value any
			}{declaration.ConfigKeys.WireAPI, wireAPIValue})
		}
	}
	for _, value := range values {
		if len(value.path) == 0 {
			continue
		}
		if err := tomlSetPath(doc, value.path, value.value); err != nil {
			return err
		}
	}
	if !perModel {
		return nil
	}
	seen := map[string]struct{}{}
	allowed := make([]string, 0, len(endpoint.Models))
	models := append([]ModelEndpointModel(nil), endpoint.Models...)
	if id := strings.TrimSpace(endpoint.Model); id != "" {
		models = append([]ModelEndpointModel{{ID: id}}, models...)
	}
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		allowed = append(allowed, id)
		name := strings.TrimSpace(model.Name)
		if name == "" {
			name = id
		}
		table := append(append([]string{}, modelPrefix...), id)
		if err := tomlSetPath(doc, append(table, "model"), id); err != nil {
			return err
		}
		if err := tomlSetPath(doc, append(table, "name"), name); err != nil {
			return err
		}
		if rest, ok := tomlPathRemainder(declaration.ConfigKeys.BaseURL, modelPrefix); ok {
			if err := tomlSetPath(doc, append(table, rest...), baseURL); err != nil {
				return err
			}
		}
		if rest, ok := tomlPathRemainder(declaration.ConfigKeys.APIKeyEnv, modelPrefix); ok {
			if err := tomlSetPath(doc, append(table, rest...), strings.TrimSpace(declaration.APIKeyEnv)); err != nil {
				return err
			}
		}
		if rest, ok := tomlPathRemainder(declaration.ConfigKeys.WireAPI, modelPrefix); ok {
			wireAPIValue := strings.TrimSpace(declaration.WireAPIConfigValue)
			if wireAPIValue == "" {
				wireAPIValue = strings.TrimSpace(declaration.WireAPI)
			}
			if err := tomlSetPath(doc, append(table, rest...), wireAPIValue); err != nil {
				return err
			}
		}
	}
	// Only ever written with ids in hand: an empty allowlist would tell the
	// runtime to hide every model, which is strictly worse than leaving the
	// key absent (it then keeps whatever it inferred from the tables above).
	if len(declaration.ConfigKeys.AllowedModels) > 0 && len(allowed) > 0 {
		if err := tomlSetPath(doc, declaration.ConfigKeys.AllowedModels, allowed); err != nil {
			return err
		}
	}
	return nil
}

func tomlPathHasPrefix(path, prefix []string) bool {
	_, ok := tomlPathRemainder(path, prefix)
	return ok
}

func tomlPathRemainder(path, prefix []string) ([]string, bool) {
	if len(prefix) == 0 || len(path) <= len(prefix) {
		return nil, false
	}
	for i, key := range prefix {
		if path[i] != key {
			return nil, false
		}
	}
	return path[len(prefix):], true
}

type tomlTable map[string]any

func tomlMergeMap(dst tomlTable, src map[string]any) error {
	if len(src) == 0 {
		return nil
	}
	keys := make([]string, 0, len(src))
	for key := range src {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value, err := tomlNormalizeValue(src[key])
		if err != nil {
			return err
		}
		if nested, ok := value.(tomlTable); ok {
			existing, exists := dst[key]
			if !exists {
				dst[key] = nested
				continue
			}
			current, ok := existing.(tomlTable)
			if !ok {
				return fmt.Errorf("extension runtime TOML %s must be a table", key)
			}
			if err := tomlMergeMap(current, map[string]any(nested)); err != nil {
				return err
			}
			continue
		}
		dst[key] = value
	}
	return nil
}

func tomlSetPath(root tomlTable, keyPath []string, value any) error {
	if len(keyPath) == 0 {
		return errors.New("extension runtime TOML key path is empty")
	}
	normalized, err := tomlNormalizeValue(value)
	if err != nil {
		return err
	}
	table := root
	for _, key := range keyPath[:len(keyPath)-1] {
		next, exists := table[key]
		if !exists {
			child := tomlTable{}
			table[key] = child
			table = child
			continue
		}
		child, ok := next.(tomlTable)
		if !ok {
			return fmt.Errorf("extension runtime TOML %s must be a table", strings.Join(keyPath[:len(keyPath)-1], "."))
		}
		table = child
	}
	table[keyPath[len(keyPath)-1]] = normalized
	return nil
}

func tomlNormalizeValue(value any) (any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, errors.New("extension runtime TOML value is unsupported")
	case string:
		return typed, nil
	case bool:
		return typed, nil
	case int:
		return int64(typed), nil
	case int8:
		return int64(typed), nil
	case int16:
		return int64(typed), nil
	case int32:
		return int64(typed), nil
	case int64:
		return typed, nil
	case uint:
		return int64(typed), nil
	case uint8:
		return int64(typed), nil
	case uint16:
		return int64(typed), nil
	case uint32:
		return int64(typed), nil
	case uint64:
		if typed > uint64(^uint64(0)>>1) {
			return nil, errors.New("extension runtime TOML integer is out of range")
		}
		return int64(typed), nil
	case float64:
		if typed != float64(int64(typed)) {
			return nil, errors.New("extension runtime TOML float is unsupported")
		}
		return int64(typed), nil
	case json.Number:
		if i, err := typed.Int64(); err == nil {
			return i, nil
		}
		return nil, errors.New("extension runtime TOML number is unsupported")
	case []string:
		return append([]string(nil), typed...), nil
	case tomlTable:
		return typed, nil
	case map[string]any:
		table := tomlTable{}
		if err := tomlMergeMap(table, typed); err != nil {
			return nil, err
		}
		return table, nil
	default:
		return nil, fmt.Errorf("extension runtime TOML value type %T is unsupported", value)
	}
}

func encodeTOML(doc tomlTable) string {
	var b strings.Builder
	encodeTOMLTable(&b, nil, doc)
	return b.String()
}

func encodeTOMLTable(b *strings.Builder, path []string, doc tomlTable) {
	keys := make([]string, 0, len(doc))
	for key := range doc {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var scalars, tables []string
	for _, key := range keys {
		if _, ok := doc[key].(tomlTable); ok {
			tables = append(tables, key)
			continue
		}
		scalars = append(scalars, key)
	}
	if len(path) > 0 && (len(scalars) > 0 || len(tables) == 0) {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteByte('[')
		b.WriteString(joinTOMLKeyPath(path))
		b.WriteString("]\n")
	}
	for _, key := range scalars {
		b.WriteString(tomlKey(key))
		b.WriteString(" = ")
		b.WriteString(tomlLiteral(doc[key]))
		b.WriteByte('\n')
	}
	for _, key := range tables {
		encodeTOMLTable(b, append(append([]string{}, path...), key), doc[key].(tomlTable))
	}
}

func joinTOMLKeyPath(path []string) string {
	parts := make([]string, len(path))
	for i, key := range path {
		parts[i] = tomlKey(key)
	}
	return strings.Join(parts, ".")
}

func tomlKey(key string) string {
	if isBareTOMLKey(key) {
		return key
	}
	return tomlQuote(key)
}

func isBareTOMLKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func tomlLiteral(value any) string {
	switch typed := value.(type) {
	case string:
		return tomlQuote(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(typed, 10)
	case []string:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, tomlQuote(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return tomlQuote(fmt.Sprint(value))
	}
}

func tomlQuote(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')
	for i := 0; i < len(value); {
		r, size := utf8.DecodeRuneInString(value[i:])
		i += size
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func validateTOMLConfigValues(values map[string]any) error {
	if len(values) == 0 {
		return nil
	}
	_, err := tomlNormalizeValue(values)
	return err
}
