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
	"regexp"
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

func (f *fakeGitHub) FindJob(context.Context, string, string, string, time.Time) (github.Run, bool, error) {
	return github.Run{}, false, nil
}

type site struct {
	url  string
	cfg  *config.Store
	sup  *runner.Supervisor
	gh   *fakeGitHub
	path string
	log  *containerLog
}

// containerLog is what the container prints, read while the service writes.
type containerLog struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (l *containerLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *containerLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func newSite(t *testing.T, env map[string]string, opts ...func(*Server)) *site {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg, _, err := config.Load(path, func(k string) string { return env[k] }, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	gh := &fakeGitHub{refuse: map[string]error{}}
	out := &containerLog{}
	sup, err := runner.New(cfg, gh, runner.Paths{
		Home: filepath.Join(dir, "missing"), Runners: filepath.Join(dir, "runners"),
		Work: filepath.Join(dir, "work"), History: filepath.Join(dir, "history.json"),
	}, out)
	if err != nil {
		t.Fatal(err)
	}
	sup.Start()
	ui := fstest.MapFS{"index.html": {Data: []byte("<title>GitHub-Runner</title>")}}
	app := New(cfg, sup, gh, ui, "1.0.1")
	for _, opt := range opts {
		opt(app)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() {
		srv.Close()
		sup.Stop(false)
	})
	return &site{url: srv.URL, cfg: cfg, sup: sup, gh: gh, path: path, log: out}
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

func TestAFreshInstallSendsAnEmptyJobList(t *testing.T) {
	s := newSite(t, nil)
	if code, body := call(t, client(t), "GET", s.url+"/api/state", ""); code != 200 || !strings.Contains(body, `"jobs":[]`) {
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

func TestWrongPasswordsAreAnsweredMoreSlowly(t *testing.T) {
	var waits []time.Duration
	s := newSite(t, nil, func(srv *Server) { srv.sleep = func(d time.Duration) { waits = append(waits, d) } })
	call(t, client(t), "PUT", s.url+"/api/password", `{"password":"right"}`)
	c := client(t)
	for range 8 {
		if code, _ := call(t, c, "POST", s.url+"/api/login", `{"password":"wrong"}`); code != 401 {
			t.Fatalf("a wrong password got %d", code)
		}
	}
	if len(waits) == 0 || waits[len(waits)-1] != maxDelay {
		t.Fatalf("waits = %v", waits)
	}
	for i := 1; i < len(waits); i++ {
		if waits[i] < waits[i-1] {
			t.Fatalf("the delay shrank: %v", waits)
		}
	}
}

func TestTheRightPasswordAlwaysGetsThrough(t *testing.T) {
	s := newSite(t, nil, func(srv *Server) { srv.sleep = func(time.Duration) {} })
	call(t, client(t), "PUT", s.url+"/api/password", `{"password":"right"}`)
	c := client(t)
	for range 20 {
		call(t, c, "POST", s.url+"/api/login", `{"password":"wrong"}`)
	}
	if code, body := call(t, c, "POST", s.url+"/api/login", `{"password":"right"}`); code != 204 {
		t.Fatalf("%d %s", code, body)
	}
}

func TestFailuresCountPerClientBehindAProxy(t *testing.T) {
	var waits []time.Duration
	s := newSite(t, nil, func(srv *Server) { srv.sleep = func(d time.Duration) { waits = append(waits, d) } })
	call(t, client(t), "PUT", s.url+"/api/password", `{"password":"right"}`)
	c := client(t)
	for range 8 {
		call(t, c, "POST", s.url+"/api/login", `{"password":"wrong"}`, "X-Forwarded-For", "203.0.113.7")
	}
	waits = nil
	call(t, c, "POST", s.url+"/api/login", `{"password":"wrong"}`, "X-Forwarded-For", "198.51.100.9")
	if len(waits) != 0 {
		t.Fatalf("another client behind the same proxy was slowed down: %v", waits)
	}
}

func TestWithoutAPasswordEveryAddressGetsIn(t *testing.T) {
	s := newSite(t, map[string]string{"ACCESS_TOKEN": "x"})
	for _, host := range []string{"192.168.1.10:8080", "tower.local:8080", "runner.example.com:443"} {
		if code := callHost(t, s.url+"/api/state", host); code != 200 {
			t.Errorf("%s got %d", host, code)
		}
	}
}

var codeInLog = regexp.MustCompile(`code: ([2-9A-HJ-NP-Z]{4})-([2-9A-HJ-NP-Z]{4})-([2-9A-HJ-NP-Z]{4})`)

// codesIn returns every reset code the log shows, oldest first.
func codesIn(log string) []string {
	var codes []string
	for _, m := range codeInLog.FindAllStringSubmatch(log, -1) {
		codes = append(codes, m[1]+"-"+m[2]+"-"+m[3])
	}
	return codes
}

func lockedSite(t *testing.T, opts ...func(*Server)) (*site, *http.Client) {
	t.Helper()
	s := newSite(t, nil, opts...)
	owner := client(t)
	if code, _ := call(t, owner, "PUT", s.url+"/api/password", `{"password":"forgotten"}`); code != 204 {
		t.Fatal("the password was not set")
	}
	return s, owner
}

func TestAForgottenPasswordIsResetWithTheCodeFromTheLog(t *testing.T) {
	s, owner := lockedSite(t)
	c := client(t)
	if code, body := call(t, c, "POST", s.url+"/api/reset-code", ""); code != 204 {
		t.Fatalf("%d %s", code, body)
	}
	codes := codesIn(s.log.String())
	if len(codes) != 1 {
		t.Fatalf("the log shows %d codes:\n%s", len(codes), s.log.String())
	}
	if !strings.Contains(s.log.String(), "If nobody here asked for a reset, ignore this. The password is unchanged.") {
		t.Errorf("the log does not say what to do without a request:\n%s", s.log.String())
	}
	typed := strings.ToLower(strings.ReplaceAll(codes[0], "-", " "))
	if code, body := call(t, c, "POST", s.url+"/api/reset", `{"code":"`+typed+`","password":"remembered"}`); code != 204 {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ := call(t, c, "GET", s.url+"/api/state", ""); code != 200 {
		t.Errorf("the reset did not sign in: %d", code)
	}
	if code, _ := call(t, owner, "GET", s.url+"/api/state", ""); code != 401 {
		t.Errorf("another session outlived the reset: %d", code)
	}
	if code, _ := call(t, client(t), "POST", s.url+"/api/login", `{"password":"forgotten"}`); code != 401 {
		t.Errorf("the old password still works: %d", code)
	}
	if code, _ := call(t, client(t), "POST", s.url+"/api/login", `{"password":"remembered"}`); code != 204 {
		t.Errorf("the new password does not work: %d", code)
	}
	if code, _ := call(t, client(t), "POST", s.url+"/api/reset", `{"code":"`+codes[0]+`","password":"again"}`); code != 401 {
		t.Errorf("a used code worked twice: %d", code)
	}
}

func TestTheCodeAppearsOnlyInTheLog(t *testing.T) {
	s, _ := lockedSite(t)
	c := client(t)
	_, body := call(t, c, "POST", s.url+"/api/reset-code", "")
	code := codesIn(s.log.String())[0]
	_, refused := call(t, c, "POST", s.url+"/api/reset", `{"code":"2222-2222-2222","password":"x"}`)
	_, state := call(t, c, "GET", s.url+"/api/session", "")
	for _, answer := range []string{body, refused, state} {
		if strings.Contains(answer, code) || strings.Contains(answer, strings.ReplaceAll(code, "-", "")) {
			t.Errorf("an answer shows the code: %s", answer)
		}
	}
}

func TestAskingAgainKeepsTheCode(t *testing.T) {
	s, _ := lockedSite(t)
	for range 3 {
		call(t, client(t), "POST", s.url+"/api/reset-code", "")
	}
	if codes := codesIn(s.log.String()); len(codes) != 1 {
		t.Fatalf("asking three times wrote %d codes", len(codes))
	}
}

func TestAnExpiredCodeIsRefusedLikeAWrongOne(t *testing.T) {
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	s, _ := lockedSite(t, func(srv *Server) { srv.now = clock })
	c := client(t)
	call(t, c, "POST", s.url+"/api/reset-code", "")
	old := codesIn(s.log.String())[0]
	_, wrong := call(t, c, "POST", s.url+"/api/reset", `{"code":"2222-2222-2222","password":"x"}`)
	mu.Lock()
	now = now.Add(resetLife + time.Second)
	mu.Unlock()
	code, expired := call(t, c, "POST", s.url+"/api/reset", `{"code":"`+old+`","password":"x"}`)
	if code != 401 || expired != wrong {
		t.Fatalf("expired: %d %s, wrong: %s", code, expired, wrong)
	}
	if errorOf(t, expired) != "That code is wrong or has expired. If it expired, the log now shows a new one." {
		t.Errorf("refusal: %s", expired)
	}
	call(t, c, "POST", s.url+"/api/reset-code", "")
	if codes := codesIn(s.log.String()); len(codes) != 2 || codes[1] == old {
		t.Fatalf("no new code after the old one expired: %v", codes)
	}
}

func TestWrongCodesAreSlowedButTheRightOneGetsThrough(t *testing.T) {
	var waits []time.Duration
	s, _ := lockedSite(t, func(srv *Server) { srv.sleep = func(d time.Duration) { waits = append(waits, d) } })
	c := client(t)
	call(t, c, "POST", s.url+"/api/reset-code", "")
	for range 20 {
		call(t, c, "POST", s.url+"/api/reset", `{"code":"2222-2222-2222","password":"x"}`)
	}
	if len(waits) == 0 || waits[len(waits)-1] != maxDelay {
		t.Fatalf("waits = %v", waits)
	}
	waits = nil
	if code, body := call(t, c, "POST", s.url+"/api/login", `{"password":"wrong"}`); code != 401 || len(waits) != 0 {
		t.Errorf("wrong codes slowed down signing in: %d %s %v", code, body, waits)
	}
	code := codesIn(s.log.String())[0]
	if status, body := call(t, c, "POST", s.url+"/api/reset", `{"code":"`+code+`","password":"new"}`); status != 204 {
		t.Fatalf("the right code after wrong ones: %d %s", status, body)
	}
}

func TestAResetNeedsANewPassword(t *testing.T) {
	s, _ := lockedSite(t)
	c := client(t)
	call(t, c, "POST", s.url+"/api/reset-code", "")
	code := codesIn(s.log.String())[0]
	if status, body := call(t, c, "POST", s.url+"/api/reset", `{"code":"`+code+`","password":""}`); status != 400 {
		t.Fatalf("%d %s", status, body)
	}
	if status, _ := call(t, c, "POST", s.url+"/api/reset", `{"code":"`+code+`","password":"new"}`); status != 204 {
		t.Errorf("an empty password spent the code: %d", status)
	}
}

func TestWithoutAPasswordThereIsNothingToReset(t *testing.T) {
	s := newSite(t, nil)
	if code, _ := call(t, client(t), "POST", s.url+"/api/reset-code", ""); code != 409 {
		t.Errorf("reset-code got %d", code)
	}
	if codes := codesIn(s.log.String()); len(codes) != 0 {
		t.Errorf("an open page wrote a code: %v", codes)
	}
}

func callHost(t *testing.T, url, host string) int {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
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
