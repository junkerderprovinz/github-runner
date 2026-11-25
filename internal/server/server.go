// Package server serves the web interface and its JSON API.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/github-runner/internal/config"
	"github.com/junkerderprovinz/github-runner/internal/github"
	"github.com/junkerderprovinz/github-runner/internal/runner"
)

type TokenSource interface {
	RunnerToken(ctx context.Context, repo, kind string) (string, error)
}

type Server struct {
	cfg      *config.Store
	sup      *runner.Supervisor
	gh       TokenSource
	ui       fs.FS
	version  string
	sessions *sessions
	now      func() time.Time
	sleep    func(time.Duration)
}

func New(cfg *config.Store, sup *runner.Supervisor, gh TokenSource, ui fs.FS, version string) *Server {
	return &Server{cfg: cfg, sup: sup, gh: gh, ui: ui, version: version, sessions: newSessions(), now: time.Now, sleep: time.Sleep}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/session", s.session)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)

	private := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.signedIn(h)) }
	private("GET /api/state", s.state)
	private("GET /api/events", s.events)
	private("POST /api/repos", s.addRepo)
	private("DELETE /api/repos/{owner}/{name}", s.removeRepo)
	private("POST /api/repos/{owner}/{name}/restart", s.restartRepo)
	private("PUT /api/repos/{owner}/{name}/labels", s.setRepoLabels)
	private("GET /api/repos/{owner}/{name}/log", s.repoLog)
	private("PATCH /api/settings", s.changeSettings)
	private("PUT /api/token", s.setToken)
	private("POST /api/token/check", s.checkToken)
	private("PUT /api/password", s.setPassword)
	private("DELETE /api/password", s.clearPassword)
	mux.Handle("GET /", s.static())

	return secure(http.NewCrossOriginProtection().Handler(mux))
}

func secure(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; font-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) locked() bool {
	return s.cfg.Get().PasswordHash != ""
}

func (s *Server) signedIn(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case s.locked() && !s.sessions.valid(r):
			fail(w, http.StatusUnauthorized, "Sign in first.")
		case !s.locked() && !homeHost(r.Host):
			fail(w, http.StatusForbidden, homeOnly)
		default:
			h.ServeHTTP(w, r)
		}
	})
}

const homeOnly = "Without a password, GitHub-Runner only answers on addresses of your own network. Open it by its IP address and set a password first."

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	reply(w, status, map[string]string{"error": msg})
}

func read(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "The request could not be read.")
		return false
	}
	return true
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	listening, total := s.sup.Listening()
	if listening < total {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, "%d of %d runners listening\n", listening, total)
		return
	}
	fmt.Fprintln(w, "ok")
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	locked := s.locked()
	reply(w, http.StatusOK, map[string]bool{
		"locked":   locked,
		"signedIn": !locked || s.sessions.valid(r),
		"homeOnly": !locked && !homeHost(r.Host),
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !read(w, r, &body) {
		return
	}
	hash := s.cfg.Get().PasswordHash
	if hash == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	client := clientOf(r)
	done := s.sessions.checking(client)
	defer done()
	if !checkPassword(hash, body.Password) {
		if wait := s.sessions.failed(client); wait > 0 {
			s.sleep(wait)
		}
		fail(w, http.StatusUnauthorized, "That is not the password set for this page.")
		return
	}
	s.sessions.forgive(client)
	s.sessions.start(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.sessions.end(w, r)
	w.WriteHeader(http.StatusNoContent)
}

type tokenInfo struct {
	Set   bool   `json:"set"`
	Last4 string `json:"last4,omitempty"`
}

// describeToken shows the end of a token only when that leaves most of it
// hidden.
func describeToken(token string) tokenInfo {
	if len(token) < 8 {
		return tokenInfo{Set: token != ""}
	}
	return tokenInfo{Set: true, Last4: token[len(token)-4:]}
}

// settingsView is everything the settings pages show. The token itself never
// leaves the server.
type settingsView struct {
	config.Settings
	Token    tokenInfo `json:"token"`
	Password bool      `json:"password"`
}

func viewSettings(c config.Config) settingsView {
	return settingsView{Settings: c.Settings, Token: describeToken(c.Token), Password: c.PasswordHash != ""}
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, map[string]any{
		"version":       s.version,
		"runnerVersion": s.sup.RunnerVersion(),
		"now":           s.now(),
		"settings":      viewSettings(s.cfg.Get()),
		"repos":         s.sup.States(),
		"jobs":          s.sup.Jobs(),
	})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	events, stop := s.sup.Subscribe()
	defer stop()
	fmt.Fprint(w, "retry: 3000\n\n")
	rc.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			// A password set since the stream opened closes it.
			if s.locked() && !s.sessions.valid(r) {
				return
			}
			fmt.Fprint(w, ": ping\n\n")
		case e, ok := <-events:
			if !ok {
				return
			}
			data, err := json.Marshal(e.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data)
		}
		if rc.Flush() != nil {
			return
		}
	}
}

// refusal words a GitHub refusal for someone adding repo by hand.
func refusal(err error, repo string) string {
	var apiErr *github.APIError
	switch {
	case errors.Is(err, github.ErrNoToken):
		return "There is no access token yet. Paste one under Settings, General first."
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized:
		return "GitHub rejected the access token (401). Replace it under Settings, General."
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden:
		return fmt.Sprintf("The token cannot manage runners in %s (403). It needs Administration: Read and write on this repository.", repo)
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound:
		return fmt.Sprintf("GitHub cannot find %s, or the token has no access to it (404).", repo)
	}
	msg := err.Error()
	return strings.ToUpper(msg[:1]) + msg[1:] + "."
}

func (s *Server) addRepo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Repo string `json:"repo"`
	}
	if !read(w, r, &body) {
		return
	}
	c := s.cfg.Get()
	name, err := config.ParseRepo(body.Repo, c.Owner)
	switch {
	case errors.Is(err, config.ErrNoOwner):
		fail(w, http.StatusBadRequest, "Write it as owner/name, or set a default owner in Settings.")
		return
	case err != nil:
		fail(w, http.StatusBadRequest, "Write it as owner/name, such as you/website.")
		return
	}
	if _, ok := c.Repo(name); ok {
		fail(w, http.StatusConflict, name+" already has a runner.")
		return
	}
	if _, err := s.gh.RunnerToken(r.Context(), name, "registration"); err != nil {
		fail(w, http.StatusUnprocessableEntity, refusal(err, name))
		return
	}
	err = s.cfg.Update(func(c *config.Config) error {
		if _, ok := c.Repo(name); ok {
			return errTaken
		}
		c.Repos = append(c.Repos, config.Repo{Name: name, Line: c.FreeLine(), Added: s.now()})
		return nil
	})
	if errors.Is(err, errTaken) {
		fail(w, http.StatusConflict, name+" already has a runner.")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "The settings could not be saved: "+err.Error())
		return
	}
	s.sup.Say("%s: added in the web interface.", name)
	s.sup.Add(name)
	reply(w, http.StatusCreated, map[string]string{"repo": name})
}

var errTaken = errors.New("taken")

func repoOf(r *http.Request) string {
	return r.PathValue("owner") + "/" + r.PathValue("name")
}

func (s *Server) removeRepo(w http.ResponseWriter, r *http.Request) {
	name := repoOf(r)
	repo, ok := s.cfg.Get().Repo(name)
	if !ok {
		fail(w, http.StatusNotFound, "There is no runner for "+name+".")
		return
	}
	if err := s.cfg.Update(func(c *config.Config) error { c.RemoveRepo(repo.Name); return nil }); err != nil {
		fail(w, http.StatusInternalServerError, "The settings could not be saved: "+err.Error())
		return
	}
	s.sup.Say("%s: removed in the web interface.", repo.Name)
	s.sup.Remove(r.Context(), repo.Name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) restartRepo(w http.ResponseWriter, r *http.Request) {
	if !s.sup.Restart(repoOf(r)) {
		fail(w, http.StatusNotFound, "There is no runner for "+repoOf(r)+".")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func cleanLabels(in []string) []string {
	out := []string{}
	for _, l := range in {
		if l = strings.TrimSpace(l); l != "" && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

func (s *Server) setRepoLabels(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Labels []string `json:"labels"`
	}
	if !read(w, r, &body) {
		return
	}
	name := repoOf(r)
	err := s.cfg.Update(func(c *config.Config) error {
		for i := range c.Repos {
			if strings.EqualFold(c.Repos[i].Name, name) {
				c.Repos[i].Labels = nil
				if body.Labels != nil {
					c.Repos[i].Labels = cleanLabels(body.Labels)
				}
				return nil
			}
		}
		return errMissing
	})
	if errors.Is(err, errMissing) {
		fail(w, http.StatusNotFound, "There is no runner for "+name+".")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "The settings could not be saved: "+err.Error())
		return
	}
	s.sup.Reconfigure(name)
	w.WriteHeader(http.StatusNoContent)
}

var errMissing = errors.New("missing")

func (s *Server) repoLog(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.cfg.Get().Repo(repoOf(r)); !ok {
		fail(w, http.StatusNotFound, "There is no runner for "+repoOf(r)+".")
		return
	}
	lines := s.sup.Log(repoOf(r))
	if lines == nil {
		lines = []runner.Line{}
	}
	reply(w, http.StatusOK, lines)
}

func (s *Server) changeSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Owner            *string   `json:"owner"`
		Name             *string   `json:"name"`
		Labels           *[]string `json:"labels"`
		NoDefaultLabels  *bool     `json:"noDefaultLabels"`
		RunAsRoot        *bool     `json:"runAsRoot"`
		DeregisterOnStop *bool     `json:"deregisterOnStop"`
		RetrySeconds     *int      `json:"retrySeconds"`
	}
	if !read(w, r, &body) {
		return
	}
	if body.Owner != nil {
		if *body.Owner = strings.TrimSpace(*body.Owner); *body.Owner != "" && !config.ValidName(*body.Owner) {
			fail(w, http.StatusBadRequest, "That is not a GitHub user or organization name.")
			return
		}
	}
	if body.Name != nil {
		if *body.Name = strings.TrimSpace(*body.Name); *body.Name == "" {
			fail(w, http.StatusBadRequest, "The runners need a name.")
			return
		}
	}
	if body.RetrySeconds != nil && *body.RetrySeconds < 10 {
		fail(w, http.StatusBadRequest, "At least 10 seconds.")
		return
	}

	var reregister, restart bool
	var next config.Config
	err := s.cfg.Update(func(c *config.Config) error {
		if body.Owner != nil {
			c.Owner = *body.Owner
		}
		if body.Name != nil && *body.Name != c.Name {
			c.Name, reregister = *body.Name, true
		}
		if body.Labels != nil {
			if labels := cleanLabels(*body.Labels); !slices.Equal(labels, c.Labels) {
				c.Labels, reregister = labels, true
			}
		}
		if body.NoDefaultLabels != nil && *body.NoDefaultLabels != c.NoDefaultLabels {
			c.NoDefaultLabels, reregister = *body.NoDefaultLabels, true
		}
		if body.RunAsRoot != nil && *body.RunAsRoot != c.RunAsRoot {
			c.RunAsRoot, restart = *body.RunAsRoot, true
		}
		if body.DeregisterOnStop != nil {
			c.DeregisterOnStop = *body.DeregisterOnStop
		}
		if body.RetrySeconds != nil {
			c.RetrySeconds = *body.RetrySeconds
		}
		next = *c
		return nil
	})
	if err != nil {
		fail(w, http.StatusInternalServerError, "The settings could not be saved: "+err.Error())
		return
	}
	switch {
	case reregister:
		s.sup.ReconfigureAll()
	case restart:
		s.sup.RestartAll()
	}
	view := viewSettings(next)
	s.sup.Emit(runner.Event{Type: "settings", Data: view})
	reply(w, http.StatusOK, view)
}

func (s *Server) setToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if !read(w, r, &body) {
		return
	}
	token := strings.TrimSpace(body.Token)
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		fail(w, http.StatusBadRequest, "Paste the whole token, it has no spaces.")
		return
	}
	var next config.Config
	if err := s.cfg.Update(func(c *config.Config) error { c.Token = token; next = *c; return nil }); err != nil {
		fail(w, http.StatusInternalServerError, "The token could not be saved: "+err.Error())
		return
	}
	s.sup.Say("The access token was replaced in the web interface.")
	s.sup.RetryFailed()
	view := viewSettings(next)
	s.sup.Emit(runner.Event{Type: "settings", Data: view})
	reply(w, http.StatusOK, view)
}

type checkResult struct {
	Repo  string `json:"repo"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func (s *Server) checkToken(w http.ResponseWriter, r *http.Request) {
	names := s.cfg.Get().RepoNames()
	results := make([]checkResult, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() {
			results[i] = checkResult{Repo: name, OK: true}
			if _, err := s.gh.RunnerToken(r.Context(), name, "registration"); err != nil {
				results[i] = checkResult{Repo: name, Error: refusal(err, name)}
			}
		})
	}
	wg.Wait()
	reply(w, http.StatusOK, results)
}

// setPassword locks the page. Everyone else is signed out, the person who
// set it stays signed in.
func (s *Server) setPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !read(w, r, &body) {
		return
	}
	if body.Password == "" {
		fail(w, http.StatusBadRequest, "Type a password first.")
		return
	}
	hash := hashPassword(body.Password)
	if err := s.cfg.Update(func(c *config.Config) error { c.PasswordHash = hash; return nil }); err != nil {
		fail(w, http.StatusInternalServerError, "The password could not be saved: "+err.Error())
		return
	}
	s.sessions.endAll()
	s.sessions.start(w, r)
	s.sup.Emit(runner.Event{Type: "settings", Data: viewSettings(s.cfg.Get())})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) clearPassword(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Update(func(c *config.Config) error { c.PasswordHash = ""; return nil }); err != nil {
		fail(w, http.StatusInternalServerError, "The password could not be removed: "+err.Error())
		return
	}
	s.sessions.endAll()
	s.sup.Emit(runner.Event{Type: "settings", Data: viewSettings(s.cfg.Get())})
	w.WriteHeader(http.StatusNoContent)
}

// static serves the built web interface. Vite names its assets after their
// content, so those may be cached for good.
func (s *Server) static() http.Handler {
	files := http.FileServerFS(s.ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			fail(w, http.StatusNotFound, "There is nothing here.")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
