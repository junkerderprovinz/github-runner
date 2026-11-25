package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunnerTokenSendsTheAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/you/website/actions/runners/registration-token" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer github_pat_x" {
			t.Errorf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"token":"REG"}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, func() string { return "github_pat_x" }).RunnerToken(context.Background(), "you/website", "registration")
	if err != nil || got != "REG" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestRefusalsSayWhatIsWrong(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{401, "the access token was rejected (401)"},
		{403, "the token cannot manage runners here (403). It needs administration rights on this repository"},
		{404, "GitHub cannot find this repository, or the token has no access to it (404)"},
		{502, "GitHub answered 502"},
	}
	for _, tt := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tt.status)
		}))
		_, err := New(srv.URL, func() string { return "x" }).RunnerToken(context.Background(), "you/website", "registration")
		srv.Close()
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != tt.status || err.Error() != tt.want {
			t.Errorf("status %d: got %v", tt.status, err)
		}
	}
}

func TestNoTokenAsksNothing(t *testing.T) {
	_, err := New("http://127.0.0.1:1", func() string { return "" }).RunnerToken(context.Background(), "you/website", "remove")
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestActiveJobFindsTheRunnersJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/you/website/actions/runs":
			w.Write([]byte(`{"workflow_runs":[{"id":1,"name":"CI","head_branch":"dev"},{"id":2,"name":"Deploy","head_branch":"main"}]}`))
		case r.URL.Path == "/repos/you/website/actions/runs/1/jobs":
			w.Write([]byte(`{"jobs":[{"runner_name":"other","status":"in_progress"}]}`))
		case r.URL.Path == "/repos/you/website/actions/runs/2/jobs":
			w.Write([]byte(`{"jobs":[{"runner_name":"GitHub-Runner","status":"completed"},{"runner_name":"GitHub-Runner","status":"in_progress","html_url":"https://github.com/you/website/actions/runs/2/job/9"}]}`))
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	}))
	defer srv.Close()

	run, found, err := New(srv.URL, func() string { return "x" }).ActiveJob(context.Background(), "you/website", "GitHub-Runner")
	if err != nil || !found {
		t.Fatalf("found %v, err %v", found, err)
	}
	if run.Workflow != "Deploy" || run.Branch != "main" || !strings.HasSuffix(run.URL, "/job/9") {
		t.Errorf("run = %+v", run)
	}
}
