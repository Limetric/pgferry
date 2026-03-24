package main

import (
	"strings"
	"unicode"
)

// toSnakeCase converts camelCase/PascalCase to snake_case.
// Consecutive uppercase runs are treated as acronyms:
// "nameASCII" → "name_ascii", "HTMLParser" → "html_parser".
func toSnakeCase(s string) string {
	runes := []rune(s)
	var result []byte
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := runes[i-1]
				if unicode.IsLower(prev) || unicode.IsDigit(prev) {
					// lowercase/digit → uppercase boundary: "name|A"
					result = append(result, '_')
				} else if unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
					// uppercase run ending before lowercase: "HTM|L|Parser" → split before L
					result = append(result, '_')
				}
			}
			result = append(result, byte(unicode.ToLower(r)))
		} else {
			result = append(result, byte(r))
		}
	}
	return string(result)
}

// pgIdent returns a PostgreSQL identifier quoted consistently. This avoids
// relying on a manually maintained keyword list and keeps generated SQL stable.
func pgIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
