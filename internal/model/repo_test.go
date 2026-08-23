package model

import "testing"

func TestValidRepoRef(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "owner/repo", want: true},
		{value: "owner-name/repo_name.go", want: true},
		{value: " owner/repo ", want: true},
		{value: "owner", want: false},
		{value: "owner/repo/extra", want: false},
		{value: "owner/repo?tab=issues", want: false},
		{value: "owner name/repo", want: false},
		{value: "../repo", want: false},
	}

	for _, tt := range tests {
		if got := ValidRepoRef(tt.value); got != tt.want {
			t.Errorf("ValidRepoRef(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}
