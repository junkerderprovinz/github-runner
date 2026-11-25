package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	ErrNoOwner = errors.New("no owner")
	ErrNotRepo = errors.New("not a repository")
)

var namePart = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValidName reports whether s can be an owner or a repository name.
func ValidName(s string) bool {
	return namePart.MatchString(s) && s != "." && s != ".."
}

// ParseRepo turns owner/name, a GitHub URL, or a bare name with a default
// owner into owner/name.
func ParseRepo(entry, owner string) (string, error) {
	s := strings.TrimSpace(entry)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "www.")
	s = strings.TrimPrefix(s, "github.com/")
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	parts := strings.Split(s, "/")
	for _, p := range parts {
		if !ValidName(p) {
			return "", ErrNotRepo
		}
	}
	switch len(parts) {
	case 1:
		if owner == "" {
			return "", ErrNoOwner
		}
		return owner + "/" + parts[0], nil
	case 2:
		return s, nil
	}
	return "", ErrNotRepo
}

// ParseRepos reads a list such as RUNNER_REPOS. Entries are separated by
// spaces, commas or semicolons; duplicates are dropped and every entry that
// cannot be read comes back as an error ready for the log.
func ParseRepos(raw, owner string) ([]string, []error) {
	var repos []string
	var bad []error
	seen := map[string]bool{}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	for _, f := range fields {
		repo, err := ParseRepo(f, owner)
		switch {
		case errors.Is(err, ErrNoOwner):
			bad = append(bad, fmt.Errorf("%q has no owner. Write it as owner/%s or set REPO_OWNER.", f, f))
		case err != nil:
			bad = append(bad, fmt.Errorf("%q is not a repository. Write it as owner/name.", f))
		case !seen[strings.ToLower(repo)]:
			seen[strings.ToLower(repo)] = true
			repos = append(repos, repo)
		}
	}
	return repos, bad
}
