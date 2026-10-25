package main

import (
	"strings"
	"testing"
)

const sample = `#EXTM3U
#EXTINF:-1 tvg-id="espn.us" tvg-logo="http://x/l.png" group-title="Sports, US",ESPN HD
http://host/espn
#EXTINF:-1 tvg-id="cnn.us" group-title="News",CNN
#EXTVLCOPT:http-user-agent=foo
http://host/cnn
#EXTINF:-1 group-title="Adult",Night Show
http://host/night
`

func run(t *testing.T, inc, exc []string, mode string) []string {
	t.Helper()
	_, entries, err := parsePlaylist(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	ir, err := compileRules(inc, "name", mode, false)
	if err != nil {
		t.Fatal(err)
	}
	er, err := compileRules(exc, "name", mode, false)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range filterEntries(entries, ir, er) {
		names = append(names, e.Name)
	}
	return names
}

func TestParse(t *testing.T) {
	_, entries, _ := parsePlaylist(strings.NewReader(sample))
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].Group != "Sports, US" || entries[0].Name != "ESPN HD" {
		t.Errorf("quoted comma not handled: %+v", entries[0])
	}
	if len(entries[1].Lines) != 2 {
		t.Errorf("extra tags not preserved: %v", entries[1].Lines)
	}
}

func TestFilters(t *testing.T) {
	tests := []struct {
		name     string
		inc, exc []string
		mode     string
		want     string
	}{
		{"contains", []string{"espn"}, nil, "contains", "ESPN HD"},
		{"regex group", []string{"group:^(news|sports)"}, nil, "regex", "ESPN HD,CNN"},
		{"glob", []string{"c*"}, nil, "glob", "CNN"},
		{"exclude", nil, []string{"group:adult"}, "contains", "ESPN HD,CNN"},
		{"url", []string{"url:/cnn$"}, nil, "regex", "CNN"},
	}
	for _, tc := range tests {
		got := strings.Join(run(t, tc.inc, tc.exc, tc.mode), ",")
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestInvalidRegex(t *testing.T) {
	if _, err := compileRule("(", "name", "regex", false); err == nil {
		t.Error("expected error for invalid regex")
	}
}
