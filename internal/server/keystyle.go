package server

import (
	"encoding/json"
	"os"
	"strings"
	"unicode"
)

// KeyStyle controls the case style of every JSON key in API responses.
type KeyStyle string

const (
	// KeyStyleCamel renders keys as lowerCamelCase (e.g. numNodes). Default.
	KeyStyleCamel KeyStyle = "camel"
	// KeyStyleSnake renders keys as snake_case (e.g. num_nodes).
	KeyStyleSnake KeyStyle = "snake"
	// KeyStylePascal renders keys as PascalCase (e.g. NumNodes).
	KeyStylePascal KeyStyle = "pascal"
)

// DefaultKeyStyle is used when no GPI_API_RESPONSE_KEY_STYLE is set.
const DefaultKeyStyle = KeyStyleCamel

// ValidKeyStyles lists the supported key styles.
var ValidKeyStyles = []KeyStyle{KeyStyleCamel, KeyStyleSnake, KeyStylePascal}

func keyStyleFromEnv() KeyStyle {
	switch KeyStyle(os.Getenv("GPI_API_RESPONSE_KEY_STYLE")) {
	case KeyStyleSnake, KeyStylePascal:
		return KeyStyle(os.Getenv("GPI_API_RESPONSE_KEY_STYLE"))
	default:
		return DefaultKeyStyle
	}
}

// applyKeyStyle re-encodes data as JSON and rewrites every object key to the
// requested style. Handlers stay style-agnostic; only the wire format changes.
func applyKeyStyle(data any, style KeyStyle) any {
	if style == "" {
		return data
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return data
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return data
	}
	return convertKeys(v, style)
}

func convertKeys(v any, style KeyStyle) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[toCase(k, style)] = convertKeys(val, style)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = convertKeys(e, style)
		}
		return out
	case string:
		// Rewrite OpenAPI $ref JSON pointers (e.g.
		// "#/components/schemas/LaunchRequest") so the referenced schema name
		// matches the converted component key (e.g. launch_request under
		// snake_case). Without this, refs break when a non-camel style is used.
		if strings.HasPrefix(t, "#/components/schemas/") && len(t) > len("#/components/schemas/") {
			name := t[len("#/components/schemas/"):]
			return "#/components/schemas/" + toCase(name, style)
		}
		return t
	default:
		return v
	}
}

// splitWords splits a JSON key into words, handling snake_case, camelCase and
// PascalCase inputs (e.g. num_nodes / numNodes / NumNodes / requestId / GPU).
func splitWords(s string) []string {
	var words []string
	var cur []rune
	runes := []rune(s)
	for i, r := range runes {
		if r == '_' || r == '-' {
			if len(cur) > 0 {
				words = append(words, string(cur))
				cur = nil
			}
			continue
		}
		if unicode.IsUpper(r) && i > 0 {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || (unicode.IsUpper(prev) && nextLower) {
				if len(cur) > 0 {
					words = append(words, string(cur))
					cur = nil
				}
			}
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		words = append(words, string(cur))
	}
	if len(words) == 0 {
		words = append(words, s)
	}
	return words
}

func toCase(key string, style KeyStyle) string {
	words := splitWords(key)
	for i := range words {
		words[i] = strings.ToLower(words[i])
	}
	switch style {
	case KeyStyleSnake:
		return strings.Join(words, "_")
	case KeyStylePascal:
		for i := range words {
			words[i] = upperFirst(words[i])
		}
		return strings.Join(words, "")
	default: // camel
		if len(words) == 0 {
			return key
		}
		words[0] = strings.ToLower(words[0])
		for i := 1; i < len(words); i++ {
			words[i] = upperFirst(words[i])
		}
		return strings.Join(words, "")
	}
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return string(unicode.ToUpper(r[0])) + string(r[1:])
}
