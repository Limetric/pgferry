package main

import "testing"

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"parentUserIdentifier", "parent_user_identifier"},
		{"geoRegionId", "geo_region_id"},
		{"chatMessages", "chat_messages"},
		{"updatedAt", "updated_at"},
		{"identifier", "identifier"},
		{"IP", "ip"},                   // acronym stays together
		{"ABCDef", "abc_def"},          // acronym + word
		{"nameASCII", "name_ascii"},    // word + trailing acronym
		{"HTMLParser", "html_parser"},  // leading acronym + word
		{"getHTTPSUrl", "get_https_url"}, // multiple acronyms
		{"getHTTPSURL", "get_httpsurl"},  // adjacent acronyms without lowercase boundary
	}
	for _, tt := range tests {
		got := toSnakeCase(tt.in)
		if got != tt.want {
			t.Errorf("toSnakeCase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPgIdent(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"user", `"user"`},
		{"order", `"order"`},
		{"table", `"table"`},
		{"users", "users"},
		{"match_id", "match_id"},
		{"chat_id-ended_at", `"chat_id-ended_at"`},
		{"has space", `"has space"`},
		{"Upper", `"Upper"`},
		{"0start", `"0start"`},
	}
	for _, tt := range tests {
		got := pgIdent(tt.in)
		if got != tt.want {
			t.Errorf("pgIdent(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExtractMySQLDBName(t *testing.T) {
	tests := []struct {
		dsn  string
		want string
		err  bool
	}{
		{"root:root@tcp(127.0.0.1:3306)/example_db", "example_db", false},
		{"root:root@tcp(127.0.0.1:3307)/another_db", "another_db", false},
		{"user:pass@/mydb", "mydb", false},
		{"nodatabase", "", true},
		{"user:pass@tcp(host:3306)/", "", true},
	}
	for _, tt := range tests {
		got, err := extractMySQLDBName(tt.dsn)
		if tt.err && err == nil {
			t.Errorf("extractMySQLDBName(%q) expected error", tt.dsn)
		}
		if !tt.err && err != nil {
			t.Errorf("extractMySQLDBName(%q) unexpected error: %v", tt.dsn, err)
		}
		if got != tt.want {
			t.Errorf("extractMySQLDBName(%q) = %q, want %q", tt.dsn, got, tt.want)
		}
	}
}
