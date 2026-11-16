package main

import "testing"

func TestCompleteStatement(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"SELECT 1", false},
		{"SELECT 1;", true},
		{"SELECT ';'", false},
		{"SELECT ';';", true},
		{"SELECT 'it''s;';", true},
		{"SELECT \"a;b\"", false},
		{"SELECT \"a;b\";", true},
		{"-- a comment; still a comment\nSELECT 1", false},
		{"-- a comment; still a comment\nSELECT 1;", true},
		{"SELECT 1 /* ; */", false},
		{"SELECT 1 /* ; */;", true},
		{"", false},
	}
	for _, tc := range cases {
		if got := completeStatement(tc.input); got != tc.want {
			t.Errorf("completeStatement(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
