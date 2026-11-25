package runner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/github-runner/internal/config"
	"github.com/junkerderprovinz/github-runner/internal/github"
)

const configScript = `#!/bin/bash
if [ "$1" = remove ]; then
  echo "remove $3" >> calls
  exit 0
fi
echo "config $*" >> calls
touch .runner
`

const idleScript = `#!/bin/bash
echo "√ Connected to GitHub"
echo
echo "2026-11-25 10:00:00Z: Listening for Jobs"
while :; do sleep 0.1; done
`

const jobScript = `#!/bin/bash
echo "2026-11-25 10:00:00Z: Listening for Jobs"
echo "2026-11-25 10:00:01Z: Running job: build"
sleep 0.3
echo "2026-11-25 10:00:02Z: Job build completed with result: Failed"
while :; do sleep 0.1; done
`

type fakeGitHub struct {
	mu        sync.Mutex
	refuse    error
	tokens    []string
	run       github.Run
	lookups   int
	missFirst int
}

func (f *fakeGitHub) RunnerToken(ctx context.Context, repo, kind string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if f.refuse != nil {
		return "", f.refuse
	}
	f.tokens = append(f.tokens, kind+" "+repo)
	return strings.ToUpper(kind), nil
}

func (f *fakeGitHub) FindJob(context.Context, string, string, string, time.Time) (github.Run, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups++
	if f.lookups <= f.missFirst {
		return github.Run{}, false, nil
	}
	return f.run, f.run.Workflow != "", nil
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

type rig struct {
	sup  *Supervisor
	gh   *fakeGitHub
	out  *syncBuffer
	cfg  *config.Store
	root string
}

func newRig(t *testing.T, runScript string, repos ...string) *rig {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("needs bash")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	os.MkdirAll(home, 0o755)
	os.WriteFile(filepath.Join(home, "config.sh"), []byte(configScript), 0o755)
	os.WriteFile(filepath.Join(home, "run.sh"), []byte(runScript), 0o755)

	cfg, _, err := config.Load(filepath.Join(root, "config.json"), func(k string) string {
		return map[string]string{"ACCESS_TOKEN": "x", "RUNNER_REPOS": strings.Join(repos, " "), "LABELS": "own"}[k]
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	gh := &fakeGitHub{}
	out := &syncBuffer{}
	sup, err := New(cfg, gh, Paths{
		Home: home, Runners: filepath.Join(root, "runners"), Work: filepath.Join(root, "work"),
		History: filepath.Join(root, "history.json"),
	}, out)
	if err != nil {
		t.Fatal(err)
	}
	sup.restartDelay, sup.stopWait, sup.lookupDelay = 50*time.Millisecond, 2*time.Second, 10*time.Millisecond
	t.Cleanup(func() { sup.Stop(false) })
	return &rig{sup: sup, gh: gh, out: out, cfg: cfg, root: root}
}

func (r *rig) waitFor(t *testing.T, what string, ok func(State) bool) State {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if states := r.sup.States(); len(states) > 0 && ok(states[0]) {
			return states[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never %s; states %+v\nlog:\n%s", what, r.sup.States(), r.out)
	return State{}
}

func (r *rig) calls(t *testing.T, repo string) string {
	data, _ := os.ReadFile(filepath.Join(r.root, "runners", strings.ReplaceAll(repo, "/", "__"), "calls"))
	return string(data)
}

func TestARunnerRegistersAndWaitsForJobs(t *testing.T) {
	r := newRig(t, idleScript, "you/website")
	r.sup.Start()
	r.waitFor(t, "idle", func(s State) bool { return s.Status == StatusIdle })

	calls := r.calls(t, "you/website")
	for _, want := range []string{"--unattended --replace", "--url https://github.com/you/website", "--token REGISTRATION", "--name GitHub-Runner", "--labels own"} {
		if !strings.Contains(calls, want) {
			t.Errorf("config.sh was called with %q, lacking %q", calls, want)
		}
	}
	out := r.out.String()
	for _, want := range []string{
		"[github-runner] Starting 1 runner(s): you/website\n",
		"[github-runner] you/website: registered as GitHub-Runner.\n",
		"[you/website] √ Connected to GitHub\n",
		"[you/website] 2026-11-25 10:00:00Z: Listening for Jobs\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the log lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "[you/website] \n") {
		t.Error("a blank runner line reached the log")
	}
	log := r.sup.Log("you/website")
	if last := log[len(log)-1]; last.Text != "Listening for Jobs" {
		t.Errorf("last log line = %+v", last)
	}
}

func TestRemovingARunnerDeregistersIt(t *testing.T) {
	r := newRig(t, idleScript, "you/website")
	r.sup.Start()
	r.waitFor(t, "idle", func(s State) bool { return s.Status == StatusIdle })
	events, stop := r.sup.Subscribe()
	defer stop()

	r.sup.Remove(context.Background(), "you/website")
	if !strings.Contains(strings.Join(r.gh.tokens, ","), "remove you/website") {
		t.Errorf("no removal token was asked for: %v", r.gh.tokens)
	}
	if !strings.Contains(r.out.String(), "[github-runner] you/website: registration removed.") {
		t.Errorf("log:\n%s", r.out)
	}
	if _, err := os.Stat(filepath.Join(r.root, "runners", "you__website")); !os.IsNotExist(err) {
		t.Error("the runner copy is still there")
	}
	if len(r.sup.States()) != 0 {
		t.Error("the runner is still listed")
	}
	for e := range events {
		if e.Type == "removed" {
			return
		}
	}
}

func TestRemovingFinishesTheDeregistrationEvenWhenTheRequestIsGone(t *testing.T) {
	r := newRig(t, idleScript, "you/website")
	r.sup.Start()
	r.waitFor(t, "idle", func(s State) bool { return s.Status == StatusIdle })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.sup.Remove(ctx, "you/website")
	if !strings.Contains(r.out.String(), "you/website: registration removed.") {
		t.Errorf("log:\n%s", r.out)
	}
}

func TestARefusedRepositoryWaitsAndTriesAgain(t *testing.T) {
	r := newRig(t, idleScript, "you/notes")
	r.gh.refuse = &github.APIError{Status: 403}
	r.sup.Start()
	st := r.waitFor(t, "refused", func(s State) bool { return s.Status == StatusError })
	if st.Error != "The token cannot manage runners here (403). It needs administration rights on this repository." {
		t.Errorf("error = %q", st.Error)
	}
	if st.RetryAt.Sub(st.Since) != 300*time.Second {
		t.Errorf("retry at %v, since %v", st.RetryAt, st.Since)
	}
	if !strings.Contains(r.out.String(), "[github-runner] you/notes: trying again in 300 seconds.") {
		t.Errorf("log:\n%s", r.out)
	}

	r.gh.mu.Lock()
	r.gh.refuse = nil
	r.gh.mu.Unlock()
	r.sup.RetryFailed()
	r.waitFor(t, "idle after the retry", func(s State) bool { return s.Status == StatusIdle })
}

func TestJobsAreTrackedFromTheRunnerOutput(t *testing.T) {
	r := newRig(t, jobScript, "you/website")
	r.gh.run = github.Run{Workflow: "CI", Branch: "main"}
	r.sup.Start()
	r.waitFor(t, "busy", func(s State) bool { return s.Status == StatusBusy && s.Job != nil && s.Job.Name == "build" })
	r.waitFor(t, "idle after the job", func(s State) bool { return s.Status == StatusIdle })

	jobs := r.sup.Jobs()
	if len(jobs) != 1 || jobs[0].Result != Failed || jobs[0].Ended.IsZero() {
		t.Fatalf("jobs = %+v", jobs)
	}
	if jobs[0].Workflow != "CI" || jobs[0].Branch != "main" {
		t.Errorf("the workflow was not looked up: %+v", jobs[0])
	}
}

func TestAWorkflowNotFoundDuringTheJobIsLookedUpAfterIt(t *testing.T) {
	r := newRig(t, jobScript, "you/website")
	r.gh.run = github.Run{Workflow: "CI", Branch: "main"}
	r.gh.missFirst = 3
	r.sup.Start()
	r.waitFor(t, "idle after the job", func(s State) bool { return s.Status == StatusIdle && len(r.sup.Jobs()) == 1 })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && r.sup.Jobs()[0].Workflow == "" {
		time.Sleep(20 * time.Millisecond)
	}
	if got := r.sup.Jobs()[0]; got.Workflow != "CI" {
		t.Errorf("the workflow stayed unknown: %+v", got)
	}
}

func TestARunnerThatStopsIsStartedAgain(t *testing.T) {
	r := newRig(t, "#!/bin/bash\necho 'Listening for Jobs'\nsleep 0.2\n", "you/website")
	r.sup.Start()
	r.waitFor(t, "offline", func(s State) bool { return s.Status == StatusOffline })
	r.waitFor(t, "idle again", func(s State) bool { return s.Status == StatusIdle })
	if !strings.Contains(r.out.String(), "runner stopped, starting it again in 0 seconds.") {
		t.Errorf("log:\n%s", r.out)
	}
}

func TestReconfiguringRegistersAgain(t *testing.T) {
	r := newRig(t, idleScript, "you/website")
	r.sup.Start()
	r.waitFor(t, "idle", func(s State) bool { return s.Status == StatusIdle })
	r.cfg.Update(func(c *config.Config) error { c.Name = "renamed"; return nil })
	r.sup.Reconfigure("you/website")
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(r.calls(t, "you/website"), "--name renamed") {
		if time.Now().After(deadline) {
			t.Fatalf("calls:\n%s\nlog:\n%s", r.calls(t, "you/website"), r.out)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(r.calls(t, "you/website"), "remove REMOVE") {
		t.Errorf("the old registration was not removed: %s", r.calls(t, "you/website"))
	}
}
