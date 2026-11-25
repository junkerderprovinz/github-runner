package config

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

var start = time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)

func TestFirstStartSeedsFromTheEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	s, notes, err := Load(path, envOf(map[string]string{
		"ACCESS_TOKEN":       "github_pat_one",
		"RUNNER_REPOS":       "you/website, backend lonely/x/y",
		"REPO_OWNER":         "you",
		"RUNNER_NAME":        "box",
		"LABELS":             "docker, gpu",
		"NO_DEFAULT_LABELS":  "true",
		"RUN_AS_ROOT":        "false",
		"DEREGISTER_ON_STOP": "yes",
		"RETRY_SECONDS":      "60",
	}), start)
	if err != nil {
		t.Fatal(err)
	}
	c := s.Get()
	if c.Token != "github_pat_one" || c.Owner != "you" || c.Name != "box" {
		t.Errorf("token, owner, name = %q, %q, %q", c.Token, c.Owner, c.Name)
	}
	if !slices.Equal(c.Labels, []string{"docker", "gpu"}) || !c.NoDefaultLabels || c.RunAsRoot || c.DeregisterOnStop || c.RetrySeconds != 60 {
		t.Errorf("settings = %+v", c.Settings)
	}
	if got := c.RepoNames(); !slices.Equal(got, []string{"you/website", "you/backend"}) {
		t.Errorf("repos = %v", got)
	}
	if c.Repos[0].Line != 0 || c.Repos[1].Line != 1 || !c.Repos[0].Added.Equal(start) {
		t.Errorf("repos = %+v", c.Repos)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], `"lonely/x/y" is not a repository`) {
		t.Errorf("notes = %q", notes)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("the seeded config was not written:", err)
	}
}

func TestSeedDefaultsMatchTheOldScript(t *testing.T) {
	s, _, err := Load(filepath.Join(t.TempDir(), "config.json"), envOf(nil), start)
	if err != nil {
		t.Fatal(err)
	}
	c := s.Get()
	want := Settings{Name: "GitHub-Runner", Labels: []string{"self-hosted"}, RunAsRoot: true, DeregisterOnStop: true, RetrySeconds: 300}
	if c.Owner != want.Owner || c.Name != want.Name || !slices.Equal(c.Labels, want.Labels) ||
		c.NoDefaultLabels || !c.RunAsRoot || !c.DeregisterOnStop || c.RetrySeconds != 300 {
		t.Errorf("settings = %+v", c.Settings)
	}
}

func TestSeedNamesWhatIsMissing(t *testing.T) {
	_, notes, err := Load(filepath.Join(t.TempDir(), "config.json"), envOf(nil), start)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(notes, "\n")
	for _, want := range []string{"There is no access token yet", "RUNNER_REPOS is empty"} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes %q lack %q", notes, want)
		}
	}
}

func TestLaterStartsKeepWhatTheWebInterfaceChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	env := map[string]string{"ACCESS_TOKEN": "github_pat_one", "RUNNER_REPOS": "you/website"}
	s, _, err := Load(path, envOf(env), start)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *Config) error {
		c.Name = "renamed"
		c.Repos = append(c.Repos, Repo{Name: "you/backend", Line: 1})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	s, notes, err := Load(path, envOf(env), start)
	if err != nil {
		t.Fatal(err)
	}
	c := s.Get()
	if c.Name != "renamed" || len(c.Repos) != 2 || len(notes) != 0 {
		t.Errorf("name %q, repos %v, notes %q", c.Name, c.RepoNames(), notes)
	}
}

func TestAChangedTokenInTheEnvironmentWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if _, _, err := Load(path, envOf(map[string]string{"ACCESS_TOKEN": "github_pat_one"}), start); err != nil {
		t.Fatal(err)
	}
	s, notes, err := Load(path, envOf(map[string]string{"ACCESS_TOKEN": "github_pat_two"}), start)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get().Token; got != "github_pat_two" {
		t.Errorf("token = %q", got)
	}
	if !slices.ContainsFunc(notes, func(n string) bool { return strings.HasPrefix(n, "ACCESS_TOKEN changed") }) {
		t.Errorf("notes = %q", notes)
	}
}

func TestATokenReplacedInTheWebInterfaceSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	env := envOf(map[string]string{"ACCESS_TOKEN": "github_pat_one"})
	s, _, err := Load(path, env, start)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *Config) error { c.Token = "github_pat_web"; return nil }); err != nil {
		t.Fatal(err)
	}
	s, _, err = Load(path, env, start)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get().Token; got != "github_pat_web" {
		t.Errorf("token = %q, the unchanged environment value overwrote the one from the web interface", got)
	}
}

func TestAnEmptyTokenInTheEnvironmentKeepsTheSavedOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if _, _, err := Load(path, envOf(map[string]string{"ACCESS_TOKEN": "github_pat_one"}), start); err != nil {
		t.Fatal(err)
	}
	s, _, err := Load(path, envOf(nil), start)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get().Token; got != "github_pat_one" {
		t.Errorf("token = %q", got)
	}
}

func TestOtherChangedVariablesAreNamedNotApplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if _, _, err := Load(path, envOf(map[string]string{"ACCESS_TOKEN": "x", "RUNNER_REPOS": "you/one"}), start); err != nil {
		t.Fatal(err)
	}
	s, notes, err := Load(path, envOf(map[string]string{"ACCESS_TOKEN": "x", "RUNNER_REPOS": "you/two"}), start)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get().RepoNames(); !slices.Equal(got, []string{"you/one"}) {
		t.Errorf("repos = %v", got)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "RUNNER_REPOS changed") {
		t.Errorf("notes = %q", notes)
	}
	if _, notes, _ := Load(path, envOf(map[string]string{"ACCESS_TOKEN": "x", "RUNNER_REPOS": "you/two"}), start); len(notes) != 0 {
		t.Errorf("the same change was named again: %q", notes)
	}
}

func TestTheConfigFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are a Unix matter")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if _, _, err := Load(path, envOf(map[string]string{"ACCESS_TOKEN": "x"}), start); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
}

func TestABrokenConfigFileStopsTheStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path, envOf(nil), start); err == nil {
		t.Fatal("a broken file was accepted")
	}
	if data, _ := os.ReadFile(path); string(data) != "{" {
		t.Error("the broken file was overwritten")
	}
}

func TestLaterStartsNameWhatIsStillMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if _, _, err := Load(path, envOf(nil), start); err != nil {
		t.Fatal(err)
	}
	_, notes, err := Load(path, envOf(nil), start)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 || notes[0] != "No repositories yet. Add them in the web interface." || !strings.HasPrefix(notes[1], "There is no access token yet") {
		t.Errorf("notes = %q", notes)
	}
}

func TestGetReturnsACopy(t *testing.T) {
	s, _, err := Load(filepath.Join(t.TempDir(), "config.json"), envOf(map[string]string{"RUNNER_REPOS": "you/one", "LABELS": "a"}), start)
	if err != nil {
		t.Fatal(err)
	}
	c := s.Get()
	c.Labels[0] = "changed"
	c.Repos[0].Name = "changed"
	if again := s.Get(); again.Labels[0] != "a" || again.Repos[0].Name != "you/one" {
		t.Error("changing a copy changed the store")
	}
}
