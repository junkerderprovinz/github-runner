// Package config keeps the settings in /config/config.json. The first start
// seeds the file from the container's environment, after that the web
// interface edits it.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/github-runner/internal/safefile"
)

type Settings struct {
	Owner            string   `json:"owner"`
	Name             string   `json:"name"`
	Labels           []string `json:"labels"`
	NoDefaultLabels  bool     `json:"noDefaultLabels"`
	RunAsRoot        bool     `json:"runAsRoot"`
	DeregisterOnStop bool     `json:"deregisterOnStop"`
	RetrySeconds     int      `json:"retrySeconds"`
}

// Repo is one repository with a runner. Labels nil means the runner uses the
// labels from Settings; Line picks its colour in the web interface.
type Repo struct {
	Name   string    `json:"name"`
	Line   int       `json:"line"`
	Labels []string  `json:"labels"`
	Added  time.Time `json:"added"`
}

type Config struct {
	Token string `json:"token"`
	Settings
	Repos        []Repo `json:"repos"`
	PasswordHash string `json:"passwordHash,omitempty"`
	// Seen holds a fingerprint of every seed variable as it was at the last
	// start, so a value changed in the container settings can be told apart
	// from one that merely stayed the same.
	Seen map[string]string `json:"seen"`
}

func (c Config) RepoNames() []string {
	names := make([]string, len(c.Repos))
	for i, r := range c.Repos {
		names[i] = r.Name
	}
	return names
}

func (c Config) Repo(name string) (Repo, bool) {
	i := c.index(name)
	if i < 0 {
		return Repo{}, false
	}
	return c.Repos[i], true
}

func (c Config) index(name string) int {
	return slices.IndexFunc(c.Repos, func(r Repo) bool { return strings.EqualFold(r.Name, name) })
}

func (c *Config) RemoveRepo(name string) {
	if i := c.index(name); i >= 0 {
		c.Repos = slices.Delete(c.Repos, i, i+1)
	}
}

// FreeLine is the first of the seven line colours no repository uses yet.
func (c Config) FreeLine() int {
	for line := 0; ; line++ {
		if !slices.ContainsFunc(c.Repos, func(r Repo) bool { return r.Line == line }) {
			return line
		}
	}
}

// LabelsFor returns the labels the runner of repo registers with.
func (c Config) LabelsFor(r Repo) []string {
	if r.Labels != nil {
		return r.Labels
	}
	return c.Labels
}

func (c Config) clone() Config {
	c.Labels = slices.Clone(c.Labels)
	c.Repos = slices.Clone(c.Repos)
	for i := range c.Repos {
		c.Repos[i].Labels = slices.Clone(c.Repos[i].Labels)
	}
	seen := make(map[string]string, len(c.Seen))
	for k, v := range c.Seen {
		seen[k] = v
	}
	c.Seen = seen
	return c
}

type Store struct {
	path string
	mu   sync.Mutex
	cfg  Config
}

func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.clone()
}

// Update applies fn to a copy and saves it. Nothing changes when fn or the
// write fails.
func (s *Store) Update(fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg.clone()
	if err := fn(&next); err != nil {
		return err
	}
	if err := save(s.path, next); err != nil {
		return err
	}
	s.cfg = next
	return nil
}

func save(path string, c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return safefile.Write(path, append(data, '\n'))
}

var seedVars = []string{
	"ACCESS_TOKEN", "RUNNER_REPOS", "REPO_OWNER", "RUNNER_NAME", "LABELS",
	"NO_DEFAULT_LABELS", "RUN_AS_ROOT", "DEREGISTER_ON_STOP", "RETRY_SECONDS",
}

func fingerprint(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:8])
}

// Load reads the config file, or seeds a new one from env on the first start.
// The notes are lines for the log about what the environment did.
func Load(path string, env func(string) string, now time.Time) (*Store, []string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		c, notes := seed(env, now)
		if err := save(path, c); err != nil {
			return nil, nil, err
		}
		switch {
		case len(c.Repos) > 0:
		case env("RUNNER_REPOS") == "":
			notes = append(notes, "RUNNER_REPOS is empty. Add repositories in the web interface, or list them in RUNNER_REPOS.")
		default:
			notes = append(notes, missingRepos)
		}
		return &Store{path: path, cfg: c}, append(notes, missingToken(c)...), nil
	}
	if err != nil {
		return nil, nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, nil, fmt.Errorf("%s cannot be read: %w", path, err)
	}
	if c.Seen == nil {
		c.Seen = map[string]string{}
	}
	var notes []string
	changed := false
	for _, name := range seedVars {
		v := env(name)
		if c.Seen[name] == fingerprint(v) {
			continue
		}
		changed = true
		c.Seen[name] = fingerprint(v)
		switch {
		case name == "ACCESS_TOKEN" && v != "" && v != c.Token:
			c.Token = v
			notes = append(notes, "ACCESS_TOKEN changed in the container settings and replaces the saved token.")
		case name == "ACCESS_TOKEN":
		// A repository taken out of the list keeps its runner, because removing
		// one is done in the web interface.
		case name == "RUNNER_REPOS":
			repos, bad := ParseRepos(v, c.Owner)
			for _, err := range bad {
				notes = append(notes, err.Error())
			}
			for _, repo := range repos {
				if _, ok := c.Repo(repo); !ok {
					c.Repos = append(c.Repos, Repo{Name: repo, Line: c.FreeLine(), Added: now})
					notes = append(notes, repo+" is new in RUNNER_REPOS and was added.")
				}
			}
		case v != "":
			notes = append(notes, name+" changed in the container settings. It only seeds the first start, so change it in the web interface instead.")
		}
	}
	if changed {
		if err := save(path, c); err != nil {
			return nil, nil, err
		}
	}
	if len(c.Repos) == 0 {
		notes = append(notes, missingRepos)
	}
	return &Store{path: path, cfg: c}, append(notes, missingToken(c)...), nil
}

const missingRepos = "No repositories yet. Add them in the web interface."

func missingToken(c Config) []string {
	if c.Token != "" {
		return nil
	}
	return []string{"There is no access token yet. Paste one in the web interface, or set ACCESS_TOKEN."}
}

func seed(env func(string) string, now time.Time) (Config, []string) {
	or := func(name, def string) string {
		if v := env(name); v != "" {
			return v
		}
		return def
	}
	c := Config{
		Token: env("ACCESS_TOKEN"),
		Settings: Settings{
			Owner:            env("REPO_OWNER"),
			Name:             or("RUNNER_NAME", "GitHub-Runner"),
			Labels:           SplitLabels(or("LABELS", "self-hosted")),
			NoDefaultLabels:  env("NO_DEFAULT_LABELS") == "true",
			RunAsRoot:        or("RUN_AS_ROOT", "true") == "true",
			DeregisterOnStop: or("DEREGISTER_ON_STOP", "true") == "true",
			RetrySeconds:     300,
		},
		Seen: map[string]string{},
	}
	if n, err := strconv.Atoi(env("RETRY_SECONDS")); err == nil && n > 0 {
		c.RetrySeconds = n
	}
	for _, name := range seedVars {
		c.Seen[name] = fingerprint(env(name))
	}

	var notes []string
	repos, bad := ParseRepos(env("RUNNER_REPOS"), c.Owner)
	for _, err := range bad {
		notes = append(notes, err.Error())
	}
	for i, name := range repos {
		c.Repos = append(c.Repos, Repo{Name: name, Line: i, Added: now})
	}
	return c, notes
}

// SplitLabels reads a comma-separated label list.
func SplitLabels(raw string) []string {
	labels := []string{}
	for _, l := range strings.Split(raw, ",") {
		if l = strings.TrimSpace(l); l != "" && !slices.Contains(labels, l) {
			labels = append(labels, l)
		}
	}
	return labels
}
