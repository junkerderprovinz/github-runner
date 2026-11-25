package runner

import (
	"regexp"
	"strings"
)

type eventKind int

const (
	plain eventKind = iota
	listening
	jobStarted
	jobEnded
)

// event is what one line of runner output means for the state.
type event struct {
	kind   eventKind
	job    string
	result string
	tone   string
	text   string
}

var (
	stamp     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}Z: `)
	running   = regexp.MustCompile(`^Running job: (.+)$`)
	completed = regexp.MustCompile(`^Job (.+) completed with result: (\w+)`)
)

// readLine parses a line of run.sh output. The runner's own time stamp is
// dropped from the text, the web interface shows its own.
func readLine(line string) event {
	text := stamp.ReplaceAllString(line, "")
	e := event{text: text}
	switch {
	case text == "Listening for Jobs":
		e.kind = listening
	case strings.HasPrefix(text, "√"):
		e.tone = "ok"
	}
	if m := running.FindStringSubmatch(text); m != nil {
		e.kind, e.job = jobStarted, m[1]
	}
	if m := completed.FindStringSubmatch(text); m != nil {
		e.kind, e.job = jobEnded, m[1]
		switch m[2] {
		case "Succeeded", "SucceededWithIssues":
			e.result = Passed
		case "Canceled", "Cancelled":
			e.result = Cancelled
		default:
			e.result, e.tone = Failed, "fail"
		}
	}
	return e
}
