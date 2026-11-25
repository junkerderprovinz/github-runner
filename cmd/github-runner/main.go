// GitHub-Runner runs one self-hosted GitHub Actions runner per repository and
// serves a web interface to watch and manage them.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/junkerderprovinz/github-runner/internal/config"
	"github.com/junkerderprovinz/github-runner/internal/github"
	"github.com/junkerderprovinz/github-runner/internal/runner"
	"github.com/junkerderprovinz/github-runner/internal/server"
	"github.com/junkerderprovinz/github-runner/web"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "health" {
		os.Exit(health(os.Stdout))
	}
	configDir := flag.String("config", "/config", "folder for config.json and the job history")
	home := flag.String("runner-home", "/actions-runner", "the runner each repository gets a copy of")
	runners := flag.String("runners", "/runners", "folder for the copies")
	work := flag.String("work", "/tmp/runner-work", "folder for the jobs' work folders")
	api := flag.String("api", "https://api.github.com", "GitHub API")
	banner := flag.String("banner", "/usr/local/bin/print-banner.sh", "script that prints the init banner")
	flag.Parse()

	art := exec.Command(*banner, "GitHub-Runner", "one runner per repository")
	art.Stdout, art.Stderr = os.Stdout, os.Stderr
	art.Run()
	if err := run(*configDir, *api, runner.Paths{
		Home: *home, Runners: *runners, Work: *work,
		History: filepath.Join(*configDir, "history.json"),
	}); err != nil {
		fmt.Printf("[github-runner] ERROR: %v\n", err)
		os.Exit(1)
	}
}

func port() string {
	if p := os.Getenv("WEBUI_PORT"); p != "" {
		return p
	}
	return "8080"
}

func run(configDir, api string, paths runner.Paths) error {
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	cfg, notes, err := config.Load(filepath.Join(configDir, "config.json"), os.Getenv, time.Now())
	if err != nil {
		return err
	}
	// The jobs run as children of this process and must not see the token.
	os.Unsetenv("ACCESS_TOKEN")

	gh := github.New(api, func() string { return cfg.Get().Token })
	sup, err := runner.New(cfg, gh, paths, os.Stdout)
	if err != nil {
		return err
	}
	for _, n := range notes {
		sup.Say("%s", n)
	}
	followDockerSocket()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	srv := &http.Server{
		Addr:              ":" + port(),
		Handler:           server.New(cfg, sup, gh, web.Files(), version).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	failed := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			failed <- err
		}
	}()
	sup.Say("Web interface on port %s.", port())
	sup.Start()

	select {
	case <-ctx.Done():
	case err = <-failed:
		err = fmt.Errorf("the web interface cannot start: %w", err)
	}
	sup.Say("Stopping.")
	stop()
	sup.Stop(cfg.Get().DeregisterOnStop)
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutdown)
	return err
}

// followDockerSocket gives the image's docker group the group id of the
// mapped socket, so a runner running as the runner user may use it.
func followDockerSocket() {
	gid, err := exec.Command("stat", "-c", "%g", "/var/run/docker.sock").Output()
	if err != nil {
		return
	}
	exec.Command("groupmod", "-g", strings.TrimSpace(string(gid)), "docker").Run()
}

// health is the container's health check: healthy while every repository
// has a runner listening for jobs.
func health(out io.Writer) int {
	c := http.Client{Timeout: 4 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port() + "/healthz")
	if err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	defer resp.Body.Close()
	io.Copy(out, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
