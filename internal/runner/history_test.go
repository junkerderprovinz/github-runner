package runner

import (
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

func TestHistoryKeepsJobsAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	h, err := openHistory(path, fixed(t0))
	if err != nil {
		t.Fatal(err)
	}
	j, err := h.start("you/website", "build", t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.update(j.ID, func(j *Job) { j.Ended, j.Result = t0.Add(time.Minute), Passed }); err != nil {
		t.Fatal(err)
	}

	again, err := openHistory(path, fixed(t0.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	jobs := again.all()
	if len(jobs) != 1 || jobs[0].Name != "build" || jobs[0].Result != Passed {
		t.Fatalf("jobs = %+v", jobs)
	}
	if next, _ := again.start("you/website", "test", t0.Add(time.Hour)); next.ID == j.ID {
		t.Error("a new job reused an old id")
	}
}

func TestHistoryDropsJobsOlderThanTwoDays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	h, err := openHistory(path, fixed(t0))
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Time{t0.Add(-50 * time.Hour), t0.Add(-47 * time.Hour), t0} {
		j, _ := h.start("you/website", "build", at)
		h.update(j.ID, func(j *Job) { j.Ended, j.Result = at.Add(time.Minute), Passed })
	}
	if got := len(h.all()); got != 2 {
		t.Errorf("%d jobs after adding, want 2", got)
	}

	later, err := openHistory(path, fixed(t0.Add(47*time.Hour+30*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if jobs := later.all(); len(jobs) != 1 || !jobs[0].Started.Equal(t0) {
		t.Errorf("jobs = %+v", jobs)
	}
}

func TestAJobLeftOpenByACrashIsClosedOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	h, _ := openHistory(path, fixed(t0))
	h.start("you/website", "build", t0)

	again, err := openHistory(path, fixed(t0.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	j := again.all()[0]
	if j.Result != Cancelled || !j.Ended.Equal(t0) {
		t.Errorf("job = %+v", j)
	}
}

func TestExpectedDurationIsTheMedianOfEarlierPassedRuns(t *testing.T) {
	h, _ := openHistory(filepath.Join(t.TempDir(), "history.json"), fixed(t0))
	runs := []struct {
		name   string
		took   time.Duration
		result string
	}{
		{"build", 60 * time.Second, Passed},
		{"build", 90 * time.Second, Passed},
		{"build", 600 * time.Second, Failed},
		{"build", 120 * time.Second, Passed},
		{"test", 10 * time.Second, Passed},
	}
	at := t0
	for _, r := range runs {
		j, _ := h.start("you/website", r.name, at)
		h.update(j.ID, func(j *Job) { j.Ended, j.Result = at.Add(r.took), r.result })
		at = at.Add(time.Hour)
	}
	j, _ := h.start("you/website", "build", at)
	if j.Expected != 90_000 {
		t.Errorf("expected = %d ms, want 90000", j.Expected)
	}
	other, _ := h.start("you/backend", "build", at)
	if other.Expected != 0 {
		t.Errorf("another repository borrowed the estimate: %d", other.Expected)
	}
}

func fixed(at time.Time) func() time.Time {
	return func() time.Time { return at }
}
