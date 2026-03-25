package main

import "testing"

func TestParseRepoSlugFromRemote(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		want   string
	}{
		{name: "https", remote: "https://github.com/messagesgoel-blip/codero.git", want: "messagesgoel-blip/codero"},
		{name: "ssh", remote: "git@github.com:messagesgoel-blip/codero.git", want: "messagesgoel-blip/codero"},
		{name: "plain", remote: "messagesgoel-blip/codero", want: "messagesgoel-blip/codero"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseRepoSlugFromRemote(tc.remote); got != tc.want {
				t.Fatalf("parseRepoSlugFromRemote(%q) = %q, want %q", tc.remote, got, tc.want)
			}
		})
	}
}
