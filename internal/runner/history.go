package runner

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/junkerderprovinz/github-runner/internal/safefile"
)

const (
	Passed    = "passed"
	Failed    = "failed"
	Cancelled = "cancelled"
)

// Keep is how long finished jobs stay in the history.
const Keep = 48 * time.Hour

type Job struct {
	ID       int64     `json:"id"`
	Repo     string    `json:"repo"`
	Name     string    `json:"name"`
	Workflow string    `json:"workflow,omitempty"`
	Branch   string    `json:"branch,omitempty"`
	URL      string    `json:"url,omitempty"`
	Started  time.Time `json:"started"`
	Ended    time.Time `json:"ended,omitzero"`
	Result   string    `json:"result,omitempty"`
	// Expected is the usual length of this job in milliseconds, 0 when
	// there is nothing to go by yet.
	Expected int64 `json:"expected,omitempty"`
}

type history struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
	jobs []Job
	next int64
}

func openHistory(path string, now func() time.Time) (*history, error) {
	h := &history{path: path, now: now, next: 1}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Jobs []Job `json:"jobs"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	for _, j := range file.Jobs {
		if j.Ended.IsZero() {
			j.Ended, j.Result = j.Started, Cancelled
		}
		h.jobs = append(h.jobs, j)
		h.next = max(h.next, j.ID+1)
	}
	h.prune()
	return h, nil
}

func (h *history) all() []Job {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.jobs)
}

// start records a job that has just begun and estimates its length from the
// earlier passed runs of the same job in the same repository.
func (h *history) start(repo, name string, at time.Time) (Job, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var took []int64
	for _, j := range h.jobs {
		if j.Repo == repo && j.Name == name && j.Result == Passed {
			took = append(took, j.Ended.Sub(j.Started).Milliseconds())
		}
	}
	j := Job{ID: h.next, Repo: repo, Name: name, Started: at}
	if len(took) > 0 {
		slices.Sort(took)
		j.Expected = took[len(took)/2]
	}
	h.next++
	h.jobs = append(h.jobs, j)
	return j, h.save()
}

// update changes a job and returns it as it is afterwards.
func (h *history) update(id int64, fn func(*Job)) (Job, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	i := slices.IndexFunc(h.jobs, func(j Job) bool { return j.ID == id })
	if i < 0 {
		return Job{}, nil
	}
	fn(&h.jobs[i])
	return h.jobs[i], h.save()
}

func (h *history) prune() {
	cut := h.now().Add(-Keep)
	h.jobs = slices.DeleteFunc(h.jobs, func(j Job) bool {
		end := j.Ended
		if end.IsZero() {
			end = j.Started
		}
		return end.Before(cut)
	})
}

func (h *history) save() error {
	h.prune()
	data, err := json.Marshal(struct {
		Jobs []Job `json:"jobs"`
	}{h.jobs})
	if err != nil {
		return err
	}
	return safefile.Write(h.path, data)
}
