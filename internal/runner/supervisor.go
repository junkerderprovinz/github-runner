// Package runner keeps one GitHub Actions runner per repository going: it
// copies the runner the image ships, registers the copy, runs it, starts it
// again when it stops and removes its registration when asked to.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/junkerderprovinz/github-runner/internal/config"
	"github.com/junkerderprovinz/github-runner/internal/github"
)

type Status string

const (
	StatusStarting Status = "starting"
	StatusIdle     Status = "idle"
	StatusBusy     Status = "busy"
	StatusOffline  Status = "offline"
	StatusError    Status = "error"
)

// State is what the web interface shows for one repository.
type State struct {
	Repo    string    `json:"repo"`
	Line    int       `json:"line"`
	Labels  []string  `json:"labels"`
	Added   time.Time `json:"added"`
	Status  Status    `json:"status"`
	Since   time.Time `json:"since"`
	Error   string    `json:"error,omitempty"`
	RetryAt time.Time `json:"retryAt,omitzero"`
	BackAt  time.Time `json:"backAt,omitzero"`
	Job     *Job      `json:"job,omitempty"`
}

type Line struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
	Tone string    `json:"tone,omitempty"`
}

// Event goes out to every subscriber. Type is repo, log, job, removed or
// settings.
type Event struct {
	Type string
	Data any
}

const logSize = 500

type GitHub interface {
	RunnerToken(ctx context.Context, repo, kind string) (string, error)
	FindJob(ctx context.Context, repo, runner, job string, started time.Time) (github.Run, bool, error)
}

type Paths struct {
	Home    string
	Runners string
	Work    string
	History string
}

type kick int

const (
	none kick = iota
	restart
	reconfigure
)

type runner struct {
	name   string
	dir    string
	work   string
	cancel context.CancelFunc
	wake   chan struct{}
	done   chan struct{}

	want  kick
	state State
	log   []Line
}

type Supervisor struct {
	cfg   *config.Store
	gh    GitHub
	paths Paths
	hist  *history
	now   func() time.Time

	restartDelay time.Duration
	stopWait     time.Duration
	lookupDelay  time.Duration

	ctx    context.Context
	cancel context.CancelFunc

	outMu sync.Mutex
	out   io.Writer

	mu      sync.Mutex
	runners map[string]*runner
	version string

	subMu sync.Mutex
	subs  map[chan Event]struct{}
}

func New(cfg *config.Store, gh GitHub, paths Paths, out io.Writer) (*Supervisor, error) {
	if err := os.MkdirAll(paths.Runners, 0o755); err != nil {
		return nil, err
	}
	h, err := openHistory(paths.History, time.Now)
	if err != nil {
		return nil, fmt.Errorf("the job history cannot be read: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Supervisor{
		cfg: cfg, gh: gh, paths: paths, hist: h, now: time.Now,
		restartDelay: 10 * time.Second, stopWait: 20 * time.Second, lookupDelay: 3 * time.Second,
		ctx: ctx, cancel: cancel, out: out,
		runners: map[string]*runner{}, subs: map[chan Event]struct{}{},
	}, nil
}

// Start launches a runner for every repository in the config.
func (s *Supervisor) Start() {
	go s.readVersion()
	names := s.cfg.Get().RepoNames()
	if len(names) > 0 {
		s.Say("Starting %d runner(s): %s", len(names), strings.Join(names, " "))
	}
	for _, name := range names {
		s.Add(name)
	}
}

func (s *Supervisor) readVersion() {
	out, err := exec.Command(filepath.Join(s.paths.Home, "bin", "Runner.Listener"), "--version").Output()
	if err != nil {
		return
	}
	s.mu.Lock()
	s.version = strings.TrimSpace(string(out))
	s.mu.Unlock()
}

func (s *Supervisor) RunnerVersion() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.version
}

// Add starts the runner for a repository that is already in the config.
func (s *Supervisor) Add(name string) {
	ctx, cancel := context.WithCancel(s.ctx)
	slug := strings.ReplaceAll(name, "/", "__")
	r := &runner{
		name: name, dir: filepath.Join(s.paths.Runners, slug), work: filepath.Join(s.paths.Work, slug),
		cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{}),
		state: State{Status: StatusStarting, Since: s.now()},
	}
	s.mu.Lock()
	s.runners[strings.ToLower(name)] = r
	s.mu.Unlock()
	s.publish(r)
	go s.serve(ctx, r)
}

// Remove stops a runner, removes its registration from GitHub and deletes its
// copy.
func (s *Supervisor) Remove(ctx context.Context, name string) {
	r := s.find(name)
	if r == nil {
		return
	}
	r.cancel()
	<-r.done
	s.deregister(ctx, r)
	os.RemoveAll(r.dir)
	os.RemoveAll(r.work)
	s.mu.Lock()
	delete(s.runners, strings.ToLower(name))
	s.mu.Unlock()
	s.Emit(Event{Type: "removed", Data: map[string]string{"repo": r.name}})
}

// Stop ends every runner and, with deregister set, removes their
// registrations.
func (s *Supervisor) Stop(deregister bool) {
	s.cancel()
	s.mu.Lock()
	all := make([]*runner, 0, len(s.runners))
	for _, r := range s.runners {
		all = append(all, r)
	}
	s.mu.Unlock()
	for _, r := range all {
		<-r.done
	}
	if !deregister {
		return
	}
	var wg sync.WaitGroup
	for _, r := range all {
		wg.Go(func() { s.deregister(context.Background(), r) })
	}
	wg.Wait()
}

func (s *Supervisor) find(name string) *runner {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runners[strings.ToLower(name)]
}

// Restart stops a runner and starts it again at once. A runner waiting to
// retry its registration tries right away.
func (s *Supervisor) Restart(name string) bool {
	return s.kick(name, restart)
}

// Reconfigure registers a runner again, for a new name or new labels.
func (s *Supervisor) Reconfigure(name string) bool {
	return s.kick(name, reconfigure)
}

func (s *Supervisor) ReconfigureAll() {
	for _, name := range s.cfg.Get().RepoNames() {
		s.kick(name, reconfigure)
	}
}

func (s *Supervisor) RestartAll() {
	for _, name := range s.cfg.Get().RepoNames() {
		s.kick(name, restart)
	}
}

// RetryFailed makes every runner that could not register try again now.
func (s *Supervisor) RetryFailed() {
	s.mu.Lock()
	var failed []string
	for _, r := range s.runners {
		if r.state.Status == StatusError {
			failed = append(failed, r.name)
		}
	}
	s.mu.Unlock()
	for _, name := range failed {
		s.kick(name, restart)
	}
}

func (s *Supervisor) kick(name string, k kick) bool {
	r := s.find(name)
	if r == nil {
		return false
	}
	s.mu.Lock()
	r.want = max(r.want, k)
	s.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
	return true
}

func (s *Supervisor) take(r *runner) kick {
	select {
	case <-r.wake:
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := r.want
	r.want = none
	return k
}

func (s *Supervisor) serve(ctx context.Context, r *runner) {
	defer close(r.done)
	for ctx.Err() == nil {
		if s.take(r) == reconfigure {
			s.deregister(ctx, r)
		}
		s.update(r, func(st *State) { *st = State{Status: StatusStarting, Since: s.now()} })
		if err := s.register(ctx, r); err != nil {
			if ctx.Err() != nil {
				return
			}
			s.refused(r, err)
			s.pause(ctx, r, time.Duration(s.cfg.Get().RetrySeconds)*time.Second)
			continue
		}
		switch s.run(ctx, r) {
		case restart:
			s.logf(r, "", "restarting the runner.")
		case reconfigure:
			s.logf(r, "", "registering again with the new settings.")
			s.deregister(ctx, r)
		default:
			if ctx.Err() != nil {
				return
			}
			s.logf(r, "", "runner stopped, starting it again in %d seconds.", int(s.restartDelay.Seconds()))
			back := s.now().Add(s.restartDelay)
			s.update(r, func(st *State) { *st = State{Status: StatusOffline, Since: s.now(), BackAt: back} })
			s.pause(ctx, r, s.restartDelay)
		}
	}
}

// pause waits for d, for the end of ctx or for a kick, whichever comes first.
func (s *Supervisor) pause(ctx context.Context, r *runner, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	case <-r.wake:
	}
}

func (s *Supervisor) refused(r *runner, err error) {
	secs := s.cfg.Get().RetrySeconds
	s.logf(r, "fail", "%v.", err)
	s.logf(r, "", "trying again in %d seconds.", secs)
	now := s.now()
	s.update(r, func(st *State) {
		since := now
		if st.Status == StatusError {
			since = st.Since
		}
		*st = State{Status: StatusError, Since: since, Error: sentence(err), RetryAt: now.Add(time.Duration(secs) * time.Second)}
	})
}

var errRegister = errors.New("the runner could not be registered. The log says why")

func registered(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".runner"))
	return err == nil
}

func (s *Supervisor) register(ctx context.Context, r *runner) error {
	c := s.cfg.Get()
	if err := s.prepare(ctx, r, c.RunAsRoot); err != nil {
		return err
	}
	if registered(r.dir) {
		return nil
	}
	token, err := s.gh.RunnerToken(ctx, r.name, "registration")
	if err != nil {
		return err
	}
	repo, _ := c.Repo(r.name)
	args := []string{"./config.sh", "--unattended", "--replace",
		"--url", "https://github.com/" + r.name, "--token", token,
		"--name", c.Name, "--work", r.work}
	if labels := c.LabelsFor(repo); len(labels) > 0 {
		args = append(args, "--labels", strings.Join(labels, ","))
	}
	if c.NoDefaultLabels {
		args = append(args, "--no-default-labels")
	}
	if err := s.script(ctx, r, c.RunAsRoot, args...); err != nil {
		return errRegister
	}
	s.logf(r, "", "registered as %s.", c.Name)
	return nil
}

// prepare gives the repository its own copy of the runner and a work folder,
// owned by the user the runner runs as.
func (s *Supervisor) prepare(ctx context.Context, r *runner, asRoot bool) error {
	if _, err := os.Stat(r.dir); errors.Is(err, fs.ErrNotExist) {
		if out, err := exec.CommandContext(ctx, "cp", "-a", s.paths.Home, r.dir).CombinedOutput(); err != nil {
			return fmt.Errorf("the runner cannot be copied: %s", bytes.TrimSpace(out))
		}
	}
	if err := os.MkdirAll(r.work, 0o755); err != nil {
		return err
	}
	if !asRoot {
		if out, err := exec.CommandContext(ctx, "chown", "-R", "runner", r.dir, r.work).CombinedOutput(); err != nil {
			return fmt.Errorf("the runner cannot be handed to the runner user: %s", bytes.TrimSpace(out))
		}
	}
	return nil
}

// deregister removes a runner's registration. It runs to the end even when
// ctx is cancelled, since a stop or a removal is exactly when it must happen.
func (s *Supervisor) deregister(ctx context.Context, r *runner) {
	if !registered(r.dir) {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 40*time.Second)
	defer cancel()
	token, err := s.gh.RunnerToken(ctx, r.name, "remove")
	if err != nil {
		s.logf(r, "fail", "the registration cannot be removed: %v.", err)
	} else if s.script(ctx, r, s.cfg.Get().RunAsRoot, "./config.sh", "remove", "--token", token) == nil {
		s.logf(r, "", "registration removed.")
	}
	for _, f := range []string{".runner", ".credentials", ".credentials_rsaparams"} {
		os.Remove(filepath.Join(r.dir, f))
	}
}

func (s *Supervisor) command(ctx context.Context, dir string, asRoot bool, args ...string) *exec.Cmd {
	argv := append([]string{"bash"}, args...)
	if !asRoot {
		argv = append([]string{"gosu", "runner"}, argv...)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "RUNNER_ALLOW_RUNASROOT=1")
	ownGroup(cmd)
	cmd.Cancel = func() error { return kill(cmd) }
	return cmd
}

// script runs config.sh. Its output stays quiet, its complaints go to the
// log.
func (s *Supervisor) script(ctx context.Context, r *runner, asRoot bool, args ...string) error {
	cmd := s.command(ctx, r.dir, asRoot, args...)
	errs := &lineWriter{emit: func(line string) {
		if strings.TrimSpace(line) != "" {
			s.print("[" + r.name + "] " + line)
			s.appendLog(r, line, "fail")
		}
	}}
	cmd.Stderr = errs
	err := cmd.Run()
	errs.flush()
	return err
}

// run starts run.sh and returns when it ends or a kick or the end of ctx
// stopped it. It returns the kick, if one did.
func (s *Supervisor) run(ctx context.Context, r *runner) kick {
	cmd := s.command(context.Background(), r.dir, s.cfg.Get().RunAsRoot, "./run.sh")
	lines := &lineWriter{emit: func(line string) { s.runnerLine(ctx, r, line) }}
	cmd.Stdout, cmd.Stderr = lines, lines
	// Something a job left running in the background can hold the output
	// pipe open after the runner itself has gone.
	cmd.WaitDelay = time.Minute
	if err := cmd.Start(); err != nil {
		s.logf(r, "fail", "the runner cannot start: %v.", err)
		return none
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	k := none
wait:
	for {
		select {
		case <-done:
			break wait
		case <-ctx.Done():
			s.halt(cmd, done)
			break wait
		case <-r.wake:
			if k = s.take(r); k != none {
				s.halt(cmd, done)
				break wait
			}
		}
	}
	lines.flush()
	s.closeJob(r, Cancelled)
	return k
}

func (s *Supervisor) halt(cmd *exec.Cmd, done <-chan error) {
	terminate(cmd)
	select {
	case <-done:
	case <-time.After(s.stopWait):
		kill(cmd)
		<-done
	}
}

func (s *Supervisor) runnerLine(ctx context.Context, r *runner, line string) {
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return
	}
	s.print("[" + r.name + "] " + line)
	e := readLine(line)
	s.appendLog(r, e.text, e.tone)
	switch e.kind {
	case listening:
		s.update(r, func(st *State) {
			if st.Status != StatusBusy {
				*st = State{Status: StatusIdle, Since: s.now()}
			}
		})
	case jobStarted:
		s.startJob(ctx, r, e.job)
	case jobEnded:
		if j, ok := s.closeJob(r, e.result); ok && j.Workflow == "" {
			go s.findRun(ctx, r, j)
		}
	}
}

func (s *Supervisor) startJob(ctx context.Context, r *runner, name string) {
	s.closeJob(r, Cancelled)
	j, err := s.hist.start(r.name, name, s.now())
	if err != nil {
		s.Say("The job history cannot be saved: %v.", err)
	}
	s.update(r, func(st *State) { *st = State{Status: StatusBusy, Since: j.Started, Job: &j} })
	s.Emit(Event{Type: "job", Data: j})
	go s.findRun(ctx, r, j)
}

func (s *Supervisor) closeJob(r *runner, result string) (Job, bool) {
	s.mu.Lock()
	open := r.state.Job
	s.mu.Unlock()
	if open == nil {
		return Job{}, false
	}
	now := s.now()
	j, err := s.hist.update(open.ID, func(j *Job) { j.Ended, j.Result = now, result })
	if err != nil {
		s.Say("The job history cannot be saved: %v.", err)
	}
	s.update(r, func(st *State) { *st = State{Status: StatusIdle, Since: now} })
	s.Emit(Event{Type: "job", Data: j})
	return j, true
}

// findRun asks GitHub which workflow and branch the job belongs to, since
// the runner only prints the job's name. A token without Actions read
// access ends the search quietly.
func (s *Supervisor) findRun(ctx context.Context, r *runner, job Job) {
	id, name := job.ID, s.cfg.Get().Name
	for range 3 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.lookupDelay):
		}
		run, found, err := s.gh.FindJob(ctx, r.name, name, job.Name, job.Started)
		if err != nil {
			return
		}
		if !found {
			continue
		}
		j, err := s.hist.update(id, func(j *Job) { j.Workflow, j.Branch, j.URL = run.Workflow, run.Branch, run.URL })
		if err != nil {
			s.Say("The job history cannot be saved: %v.", err)
		}
		s.update(r, func(st *State) {
			if st.Job != nil && st.Job.ID == id {
				st.Job = &j
			}
		})
		s.Emit(Event{Type: "job", Data: j})
		return
	}
}

func (s *Supervisor) update(r *runner, fn func(*State)) {
	s.mu.Lock()
	fn(&r.state)
	s.mu.Unlock()
	s.publish(r)
}

func (s *Supervisor) publish(r *runner) {
	c := s.cfg.Get()
	s.mu.Lock()
	st := s.view(r, c)
	s.mu.Unlock()
	s.Emit(Event{Type: "repo", Data: st})
}

// view joins a runner's state with its entry in the config. The caller holds
// s.mu.
func (s *Supervisor) view(r *runner, c config.Config) State {
	st := r.state
	if st.Job != nil {
		j := *st.Job
		st.Job = &j
	}
	st.Repo = r.name
	if repo, ok := c.Repo(r.name); ok {
		st.Line, st.Labels, st.Added = repo.Line, repo.Labels, repo.Added
	}
	return st
}

// States lists every repository in the order of the config.
func (s *Supervisor) States() []State {
	c := s.cfg.Get()
	s.mu.Lock()
	defer s.mu.Unlock()
	states := []State{}
	for _, repo := range c.Repos {
		if r := s.runners[strings.ToLower(repo.Name)]; r != nil {
			states = append(states, s.view(r, c))
		}
	}
	return states
}

// Listening counts the repositories whose runner waits for or runs a job.
func (s *Supervisor) Listening() (listening, total int) {
	for _, st := range s.States() {
		total++
		if st.Status == StatusIdle || st.Status == StatusBusy {
			listening++
		}
	}
	return listening, total
}

func (s *Supervisor) Log(name string) []Line {
	r := s.find(name)
	if r == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Line{}, r.log...)
}

// Jobs is the history of the last two days, oldest first.
func (s *Supervisor) Jobs() []Job {
	return s.hist.all()
}

func (s *Supervisor) appendLog(r *runner, text, tone string) {
	l := Line{At: s.now(), Text: text, Tone: tone}
	s.mu.Lock()
	r.log = append(r.log, l)
	if len(r.log) > logSize {
		r.log = append(r.log[:0], r.log[len(r.log)-logSize:]...)
	}
	s.mu.Unlock()
	s.Emit(Event{Type: "log", Data: struct {
		Repo string `json:"repo"`
		Line
	}{r.name, l}})
}

// Say writes a line of the service's own to the container log.
func (s *Supervisor) Say(format string, args ...any) {
	s.print("[github-runner] " + fmt.Sprintf(format, args...))
}

// logf writes a line about one repository to the container log and to the
// repository's log in the web interface.
func (s *Supervisor) logf(r *runner, tone, format string, args ...any) {
	text := "[github-runner] " + r.name + ": " + fmt.Sprintf(format, args...)
	s.print(text)
	s.appendLog(r, text, tone)
}

// Print writes lines to the container log as they are, kept together.
func (s *Supervisor) Print(lines ...string) {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	for _, line := range lines {
		fmt.Fprintln(s.out, line)
	}
}

func (s *Supervisor) print(line string) {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	fmt.Fprintln(s.out, line)
}

// Subscribe returns a channel with every event from now on. A subscriber that
// falls too far behind is dropped and finds its channel closed.
func (s *Supervisor) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 256)
	s.subMu.Lock()
	s.subs[ch] = struct{}{}
	s.subMu.Unlock()
	return ch, func() {
		s.subMu.Lock()
		defer s.subMu.Unlock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
	}
}

func (s *Supervisor) Emit(e Event) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- e:
		default:
			delete(s.subs, ch)
			close(ch)
		}
	}
}

// sentence turns an error into a sentence for the web interface.
func sentence(err error) string {
	msg := err.Error()
	first, size := utf8.DecodeRuneInString(msg)
	msg = string(unicode.ToUpper(first)) + msg[size:]
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return msg
}

// lineWriter cuts a stream into lines.
type lineWriter struct {
	buf  []byte
	emit func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		w.emit(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
	}
}

func (w *lineWriter) flush() {
	if len(w.buf) > 0 {
		w.emit(string(w.buf))
		w.buf = nil
	}
}
