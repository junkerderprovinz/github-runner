package runner

import "testing"

func TestReadLine(t *testing.T) {
	tests := []struct {
		line string
		want event
	}{
		{"2026-11-25 10:00:00Z: Listening for Jobs", event{kind: listening, text: "Listening for Jobs"}},
		{"2026-11-25 10:00:01Z: Running job: build", event{kind: jobStarted, job: "build", text: "Running job: build"}},
		{"2026-11-25 10:00:01Z: Running job: test (ubuntu, 22)", event{kind: jobStarted, job: "test (ubuntu, 22)", text: "Running job: test (ubuntu, 22)"}},
		{"2026-11-25 10:02:00Z: Job build completed with result: Succeeded", event{kind: jobEnded, job: "build", result: Passed, text: "Job build completed with result: Succeeded"}},
		{"Job deploy completed with result: Failed", event{kind: jobEnded, job: "deploy", result: Failed, tone: "fail", text: "Job deploy completed with result: Failed"}},
		{"Job lint completed with result: Canceled", event{kind: jobEnded, job: "lint", result: Cancelled, text: "Job lint completed with result: Canceled"}},
		{"Job lint completed with result: Abandoned", event{kind: jobEnded, job: "lint", result: Failed, tone: "fail", text: "Job lint completed with result: Abandoned"}},
		{"√ Connected to GitHub", event{tone: "ok", text: "√ Connected to GitHub"}},
		{"Current runner version: '2.338.2'", event{text: "Current runner version: '2.338.2'"}},
	}
	for _, tt := range tests {
		if got := readLine(tt.line); got != tt.want {
			t.Errorf("readLine(%q)\n got %+v\nwant %+v", tt.line, got, tt.want)
		}
	}
}
