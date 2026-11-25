package config

import (
	"errors"
	"slices"
	"testing"
)

func TestParseRepo(t *testing.T) {
	tests := []struct {
		in, owner, want string
		err             error
	}{
		{"you/website", "", "you/website", nil},
		{"  you/website  ", "", "you/website", nil},
		{"website", "you", "you/website", nil},
		{"website", "", "", ErrNoOwner},
		{"https://github.com/you/website", "", "you/website", nil},
		{"https://github.com/you/website/", "", "you/website", nil},
		{"github.com/you/website.git", "", "you/website", nil},
		{"http://www.github.com/you/my.site", "", "you/my.site", nil},
		{"you/web/site", "", "", ErrNotRepo},
		{"/website", "", "", ErrNotRepo},
		{"you/web site", "", "", ErrNotRepo},
		{"you/..", "", "", ErrNotRepo},
		{"", "you", "", ErrNotRepo},
	}
	for _, tt := range tests {
		got, err := ParseRepo(tt.in, tt.owner)
		if got != tt.want || !errors.Is(err, tt.err) {
			t.Errorf("ParseRepo(%q, %q) = %q, %v; want %q, %v", tt.in, tt.owner, got, err, tt.want, tt.err)
		}
	}
}

func TestParseReposSplitsOnSpacesCommasAndSemicolons(t *testing.T) {
	got, bad := ParseRepos("you/one, you/two;you/three\n  four,,", "you")
	want := []string{"you/one", "you/two", "you/three", "you/four"}
	if !slices.Equal(got, want) || len(bad) != 0 {
		t.Fatalf("got %v, %v; want %v", got, bad, want)
	}
}

func TestParseReposDropsDuplicatesIgnoringCase(t *testing.T) {
	got, _ := ParseRepos("you/one You/One https://github.com/you/one", "")
	if !slices.Equal(got, []string{"you/one"}) {
		t.Fatalf("got %v", got)
	}
}

func TestParseReposReportsEntriesWithoutOwner(t *testing.T) {
	got, bad := ParseRepos("you/one lonely", "")
	if !slices.Equal(got, []string{"you/one"}) {
		t.Fatalf("got %v", got)
	}
	if len(bad) != 1 || bad[0].Error() != `"lonely" has no owner. Write it as owner/lonely or set REPO_OWNER.` {
		t.Fatalf("bad = %v", bad)
	}
}
