package editor

import "testing"

func TestTemplateRoundTrip(t *testing.T) {
	doc := Template("Fix top bar", "It overflows.\n\n# Steps\n1. open", "Repo: a/b")
	title, body, ok := Parse(doc)
	if !ok || title != "Fix top bar" || body != "It overflows.\n\n# Steps\n1. open" {
		t.Fatalf("Parse = %q, %q, %v", title, body, ok)
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		doc, title, body string
		ok               bool
	}{
		{"", "", "", false},
		{"<!-- triage:\nhelp\n-->\n", "", "", false},
		{"Only a title\n", "Only a title", "", true},
		{"# Heading title\r\n\r\nbody\r\n", "Heading title", "body", true},
		{"Title\n\nbody <!-- keep me -->\n", "Title", "body <!-- keep me -->", true},
	}
	for _, tt := range tests {
		title, body, ok := Parse(tt.doc)
		if title != tt.title || body != tt.body || ok != tt.ok {
			t.Errorf("Parse(%q) = %q, %q, %v", tt.doc, title, body, ok)
		}
	}
}
