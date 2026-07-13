package main

import (
	"fmt"
	"strings"
)

func parseMySQLEnumSetValues(columnType string) ([]string, error) {
	open := strings.IndexByte(columnType, '(')
	close := strings.LastIndexByte(columnType, ')')
	if open < 0 || close <= open {
		return nil, fmt.Errorf("invalid enum/set column_type %q", columnType)
	}

	inside := columnType[open+1 : close]
	var values []string
	i := 0
	for i < len(inside) {
		for i < len(inside) && (inside[i] == ' ' || inside[i] == ',') {
			i++
		}
		if i >= len(inside) {
			break
		}
		if inside[i] != '\'' {
			return nil, fmt.Errorf("invalid enum/set value list in %q", columnType)
		}
		i++

		var b strings.Builder
		for i < len(inside) {
			c := inside[i]
			if c == '\\' {
				if i+1 >= len(inside) {
					return nil, fmt.Errorf("invalid escape in %q", columnType)
				}
				b.WriteByte(inside[i+1])
				i += 2
				continue
			}
			if c == '\'' {
				if i+1 < len(inside) && inside[i+1] == '\'' {
					b.WriteByte('\'')
					i += 2
					continue
				}
				i++
				break
			}
			b.WriteByte(c)
			i++
		}

		values = append(values, b.String())
	}

	return values, nil
}

// parseMySQLSetDefault splits a MySQL SET column default ("a,b") into its members.
// Members are kept verbatim: MySQL preserves leading spaces in SET/ENUM member
// values, and both the generated CHECK constraint (parseMySQLEnumSetValues) and
// the row transformer (mysqlSetTextArrayTransformer) split without trimming.
// Trimming here produced a DEFAULT that its own CHECK constraint rejected.
func parseMySQLSetDefault(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
