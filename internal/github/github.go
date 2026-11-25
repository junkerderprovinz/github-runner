// Package github talks to the GitHub REST API for runner tokens and for the
// job a runner is working on.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrNoToken = errors.New("there is no access token yet. Paste one in the web interface")

// APIError is a refusal from GitHub. Its text is meant for people.
type APIError struct {
	Status int
}

func (e *APIError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "the access token was rejected (401)"
	case http.StatusForbidden:
		return "the token cannot manage runners here (403). It needs administration rights on this repository"
	case http.StatusNotFound:
		return "GitHub cannot find this repository, or the token has no access to it (404)"
	}
	return fmt.Sprintf("GitHub answered %d", e.Status)
}

type Client struct {
	API   string
	Token func() string
	HTTP  *http.Client
}

func New(api string, token func() string) *Client {
	return &Client{API: strings.TrimSuffix(api, "/"), Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// RunnerToken asks for a short-lived registration or removal token; kind is
// "registration" or "remove".
func (c *Client) RunnerToken(ctx context.Context, repo, kind string) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	if err := c.do(ctx, http.MethodPost, "/repos/"+repo+"/actions/runners/"+kind+"-token", &out); err != nil {
		return "", err
	}
	return out.Token, nil
}

// Run is what GitHub knows about the job a runner works on.
type Run struct {
	Workflow string
	Branch   string
	URL      string
}

// ActiveJob finds the in-progress job that runner picked up in repo. Found is
// false while GitHub does not list it yet.
func (c *Client) ActiveJob(ctx context.Context, repo, runner string) (run Run, found bool, err error) {
	var runs struct {
		Runs []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Branch string `json:"head_branch"`
		} `json:"workflow_runs"`
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+repo+"/actions/runs?status=in_progress&per_page=20", &runs); err != nil {
		return Run{}, false, err
	}
	for _, r := range runs.Runs {
		var jobs struct {
			Jobs []struct {
				Runner string `json:"runner_name"`
				Status string `json:"status"`
				URL    string `json:"html_url"`
			} `json:"jobs"`
		}
		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/actions/runs/%d/jobs?per_page=100", repo, r.ID), &jobs); err != nil {
			return Run{}, false, err
		}
		for _, j := range jobs.Jobs {
			if j.Runner == runner && j.Status == "in_progress" {
				return Run{Workflow: r.Name, Branch: r.Branch, URL: j.URL}, true, nil
			}
		}
	}
	return Run{}, false, nil
}

func (c *Client) do(ctx context.Context, method, path string, out any) error {
	token := c.Token()
	if token == "" {
		return ErrNoToken
	}
	req, err := http.NewRequestWithContext(ctx, method, c.API+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return fmt.Errorf("GitHub cannot be reached: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return &APIError{Status: resp.StatusCode}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
