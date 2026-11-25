package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/junkerderprovinz/github-runner/internal/config"
	"github.com/junkerderprovinz/github-runner/internal/github"
	"github.com/junkerderprovinz/github-runner/internal/runner"
)

type fakeGitHub struct {
	mu     sync.Mutex
	refuse map[string]error
}

func (f *fakeGitHub) RunnerToken(_ context.Context, repo, kind string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refuse[repo]; err != nil {
		return "", err
	}
	return "TOKEN", nil
}

func (f *fakeGitHub) ActiveJob(context.Context, string, string) (github.Run, bool, error) {
	return github.Run{}, false, nil
}

type site struct {
	url  string
	cfg  *config.Store
	sup  *runner.Supervisor
	gh   *fakeGitHub
	path string
}

func newSite(t *testing.T, env map[string]string) *site {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg, _, err := config.Load(path, func(k string) string { return env[k] }, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	gh := &fakeGitHub{refuse: map[string]error{}}
	sup, err := runner.New(cfg, gh, runner.Paths{
		Home: filepath.Join(dir, "missing"), Runners: filepath.Join(dir, "runners"),
		Work: filepath.Join(dir, "work"), History: filepath.Join(dir, "history.json"),
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	sup.Start()
	ui := fstest.MapFS{"index.html": {Data: []byte("<title>GitHub-Runner</title>")}}
	srv := httptest.NewServer(New(cfg, sup, gh, ui, "1.1.0").Handler())
	t.Cleanup(func() {
		srv.Close()
		sup.Stop(false)
	})
	return &site{url: srv.URL, cfg: cfg, sup: sup, gh: gh, path: path}
}

func client(t *testing.T) *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

func call(t *testing.T, c *http.Client, method, url, body string, header ...string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

func errorOf(t *testing.T, body string) string {
	var v struct{ Error string }
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("not JSON: %s", body)
	}
	return v.Error
}

func TestThePageIsOpenWithoutAPassword(t *testing.T) {
	s := newSite(t, nil)
	c := client(t)
	if code, body := call(t, c, "GET", s.url+"/api/state", ""); code != 200 || !strings.Contains(body, `"repos":[]`) {
		t.Fatalf("%d %s", code, body)
	}
	if code, body := call(t, c, "GET", s.url+"/api/session", ""); code != 200 || body != "{\"locked\":false,\"signedIn\":true}\n" {
		t.Fatalf("%d %s", code, body)
	}
	if code, body := call(t, c, "GET", s.url+"/", ""); code != 200 || !strings.Contains(body, "GitHub-Runner") {
		t.Fatalf("%d %s", code, body)
	}
}

func TestTheTokenNeverLeavesTheServer(t *testing.T) {
	s := newSite(t, map[string]string{"ACCESS_TOKEN": "github_pat_SECRETSECRET1234"})
	c := client(t)
	_, body := call(t, c, "GET", s.url+"/api/state", "")
	if strings.Contains(body, "SECRETSECRET") || !strings.Contains(body, `"token":{"set":true,"last4":"1234"}`) {
		t.Fatalf("state: %s", body)
	}
	code, body := call(t, c, "PUT", s.url+"/api/token", `{"token":"fake-OTHERSECRET9876"}`)
	if code != 200 || strings.Contains(body, "OTHERSECRET") || !strings.Contains(body, `"last4":"9876"`) {
		t.Fatalf("%d %s", code, body)
	}
	if s.cfg.Get().Token != "fake-OTHERSECRET9876" {
		t.Error("the new token was not saved")
	}
}

func TestAPasswordLocksThePage(t *testing.T) {
	s := newSite(t, nil)
	owner := client(t)
	if code, body := call(t, owner, "PUT", s.url+"/api/password", `{"password":"tiny"}`); code != 204 {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ := call(t, owner, "GET", s.url+"/api/state", ""); code != 200 {
		t.Errorf("the person who set the password was signed out: %d", code)
	}

	stranger := client(t)
	if code, body := call(t, stranger, "GET", s.url+"/api/state", ""); code != 401 || errorOf(t, body) != "Sign in first." {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ := call(t, stranger, "POST", s.url+"/api/repos", `{"repo":"you/x"}`); code != 401 {
		t.Errorf("a locked page took a change: %d", code)
	}
	if _, body := call(t, stranger, "GET", s.url+"/api/session", ""); body != "{\"locked\":true,\"signedIn\":false}\n" {
		t.Errorf("session: %s", body)
	}
	if code, body := call(t, stranger, "POST", s.url+"/api/login", `{"password":"wrong"}`); code != 401 || errorOf(t, body) != "That is not the password set for this page." {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ := call(t, stranger, "POST", s.url+"/api/login", `{"password":"tiny"}`); code != 204 {
		t.Fatalf("login: %d", code)
	}
	if code, _ := call(t, stranger, "GET", s.url+"/api/state", ""); code != 200 {
		t.Errorf("signed in, still refused: %d", code)
	}

	data, _ := os.ReadFile(s.path)
	if strings.Contains(string(data), "tiny") || !strings.Contains(string(data), "$argon2id$") {
		t.Errorf("the password is not stored as a hash:\n%s", data)
	}

	if code, _ := call(t, stranger, "POST", s.url+"/api/logout", ""); code != 204 {
		t.Fatalf("logout: %d", code)
	}
	if code, _ := call(t, stranger, "GET", s.url+"/api/state", ""); code != 401 {
		t.Errorf("still signed in after signing out: %d", code)
	}
}

func TestTheSessionCookieIsLockedDown(t *testing.T) {
	s := newSite(t, nil)
	req, _ := http.NewRequest("PUT", s.url+"/api/password", strings.NewReader(`{"password":"pw"}`))
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	cookie := resp.Header.Get("Set-Cookie")
	for _, want := range []string{"HttpOnly", "SameSite=Strict", "Secure", "Path=/"} {
		if !strings.Contains(cookie, want) {
			t.Errorf("cookie %q lacks %s", cookie, want)
		}
	}
}

func TestRemovingThePasswordOpensThePage(t *testing.T) {
	s := newSite(t, nil)
	owner := client(t)
	call(t, owner, "PUT", s.url+"/api/password", `{"password":"pw"}`)
	if code, _ := call(t, owner, "DELETE", s.url+"/api/password", ""); code != 204 {
		t.Fatalf("remove: %d", code)
	}
	if code, _ := call(t, client(t), "GET", s.url+"/api/state", ""); code != 200 {
		t.Errorf("still locked: %d", code)
	}
}

func TestRepeatedWrongPasswordsAreRefusedForAWhile(t *testing.T) {
	s := newSite(t, nil)
	call(t, client(t), "PUT", s.url+"/api/password", `{"password":"right"}`)
	c := client(t)
	for range maxFails {
		call(t, c, "POST", s.url+"/api/login", `{"password":"wrong"}`)
	}
	if code, body := call(t, c, "POST", s.url+"/api/login", `{"password":"right"}`); code != 429 || !strings.Contains(errorOf(t, body), "Wait a few minutes") {
		t.Fatalf("%d %s", code, body)
	}
}

func TestCrossSiteWritesAreRefused(t *testing.T) {
	s := newSite(t, map[string]string{"ACCESS_TOKEN": "x"})
	c := client(t)
	code, _ := call(t, c, "POST", s.url+"/api/repos", `{"repo":"you/website"}`, "Sec-Fetch-Site", "cross-site", "Origin", "https://evil.example")
	if code != 403 {
		t.Errorf("a cross-site write got %d", code)
	}
	code, _ = call(t, c, "PUT", s.url+"/api/password", `{"password":"pw"}`, "Origin", "https://evil.example")
	if code != 403 {
		t.Errorf("a cross-origin write without Sec-Fetch-Site got %d", code)
	}
	if len(s.cfg.Get().Repos) != 0 || s.cfg.Get().PasswordHash != "" {
		t.Error("a refused request changed the settings")
	}
	if code, _ := call(t, c, "POST", s.url+"/api/repos", `{"repo":"you/website"}`, "Sec-Fetch-Site", "same-origin"); code != 201 {
		t.Errorf("a same-origin write got %d", code)
	}
}

func TestAddingARepositorySaysWhyGitHubRefused(t *testing.T) {
	s := newSite(t, map[string]string{"ACCESS_TOKEN": "x"})
	s.gh.refuse = map[string]error{
		"you/a": &github.APIError{Status: 401},
		"you/b": &github.APIError{Status: 403},
		"you/c": &github.APIError{Status: 404},
	}
	want := map[string]string{
		"you/a": "GitHub rejected the access token (401). Replace it under Settings, General.",
		"you/b": "The token cannot manage runners in you/b (403). It needs Administration: Read and write on this repository.",
		"you/c": "GitHub cannot find you/c, or the token has no access to it (404).",
	}
	for repo, msg := range want {
		code, body := call(t, client(t), "POST", s.url+"/api/repos", `{"repo":"`+repo+`"}`)
		if code != 422 || errorOf(t, body) != msg {
			t.Errorf("%s: %d %s", repo, code, body)
		}
	}
	if len(s.cfg.Get().Repos) != 0 {
		t.Error("a refused repository was added")
	}
}

func TestAddingARepositoryWithoutATokenSaysSo(t *testing.T) {
	s := newSite(t, nil)
	s.gh.refuse["you/a"] = github.ErrNoToken
	code, body := call(t, client(t), "POST", s.url+"/api/repos", `{"repo":"you/a"}`)
	if code != 422 || !strings.HasPrefix(errorOf(t, body), "There is no access token yet.") {
		t.Errorf("%d %s", code, body)
	}
}

func TestAddingARepositoryReadsTheName(t *testing.T) {
	s := newSite(t, map[string]string{"ACCESS_TOKEN": "x"})
	c := client(t)
	if code, body := call(t, c, "POST", s.url+"/api/repos", `{"repo":"website"}`); code != 400 || !strings.Contains(errorOf(t, body), "default owner") {
		t.Errorf("%d %s", code, body)
	}
	call(t, c, "PATCH", s.url+"/api/settings", `{"owner":"you"}`)
	if code, body := call(t, c, "POST", s.url+"/api/repos", `{"repo":"website"}`); code != 201 || !strings.Contains(body, `"you/website"`) {
		t.Errorf("%d %s", code, body)
	}
	if code, body := call(t, c, "POST", s.url+"/api/repos", `{"repo":"https://github.com/you/website"}`); code != 409 || errorOf(t, body) != "you/website already has a runner." {
		t.Errorf("%d %s", code, body)
	}
	if code, _ := call(t, c, "POST", s.url+"/api/repos", `{"repo":"you/web site"}`); code != 400 {
		t.Errorf("a bad name got %d", code)
	}
}

func TestRemovingARepositoryDropsItFromTheConfig(t *testing.T) {
	s := newSite(t, map[string]string{"ACCESS_TOKEN": "x", "RUNNER_REPOS": "you/website you/backend"})
	c := client(t)
	if code, _ := call(t, c, "DELETE", s.url+"/api/repos/you/website", ""); code != 204 {
		t.Fatalf("remove: %d", code)
	}
	if got := s.cfg.Get().RepoNames(); len(got) != 1 || got[0] != "you/backend" {
		t.Errorf("repos = %v", got)
	}
	if code, _ := call(t, c, "DELETE", s.url+"/api/repos/you/website", ""); code != 404 {
		t.Errorf("removing twice got %d", code)
	}
}

func TestSettingsAreCheckedAndSaved(t *testing.T) {
	s := newSite(t, nil)
	c := client(t)
	if code, body := call(t, c, "PATCH", s.url+"/api/settings", `{"retrySeconds":5}`); code != 400 || errorOf(t, body) != "At least 10 seconds." {
		t.Errorf("%d %s", code, body)
	}
	if code, body := call(t, c, "PATCH", s.url+"/api/settings", `{"name":"  "}`); code != 400 || errorOf(t, body) != "The runners need a name." {
		t.Errorf("%d %s", code, body)
	}
	code, _ := call(t, c, "PATCH", s.url+"/api/settings", `{"name":"box","labels":["a"," b ","a",""],"runAsRoot":false,"retrySeconds":60}`)
	got := s.cfg.Get()
	if code != 200 || got.Name != "box" || strings.Join(got.Labels, ",") != "a,b" || got.RunAsRoot || got.RetrySeconds != 60 {
		t.Errorf("%d %+v", code, got.Settings)
	}
}

func TestHealthFollowsTheRunners(t *testing.T) {
	s := newSite(t, nil)
	if code, body := call(t, client(t), "GET", s.url+"/healthz", ""); code != 200 || body != "ok\n" {
		t.Errorf("no repositories: %d %s", code, body)
	}
	s = newSite(t, map[string]string{"ACCESS_TOKEN": "x", "RUNNER_REPOS": "you/website"})
	if code, body := call(t, client(t), "GET", s.url+"/healthz", ""); code != 503 || body != "0 of 1 runners listening\n" {
		t.Errorf("one repository without a runner: %d %s", code, body)
	}
}

func TestPasswordHashes(t *testing.T) {
	h := hashPassword("correct horse")
	if !checkPassword(h, "correct horse") || checkPassword(h, "correct horsE") || checkPassword("", "") {
		t.Fatal("the hash does not tell passwords apart")
	}
	if h == hashPassword("correct horse") {
		t.Error("two hashes of one password are equal, the salt is missing")
	}
}
