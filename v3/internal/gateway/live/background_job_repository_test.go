package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

// This process-only repository is a test double for the separately tested Redis
// repository. It enforces exclusive expiring claims and never shares pointers.
type backgroundJobsMemory struct {
	mu             sync.Mutex
	jobs           map[string]BackgroundJob
	events         map[string][]BackgroundEvent
	created        chan string
	appended       chan string
	createErr      error
	failBilledSave bool
}

func newBackgroundJobsMemory() *backgroundJobsMemory {
	return &backgroundJobsMemory{jobs: make(map[string]BackgroundJob), events: make(map[string][]BackgroundEvent), created: make(chan string, 32), appended: make(chan string, 100)}
}

func copyBackgroundJob(job BackgroundJob) BackgroundJob {
	data, _ := json.Marshal(job)
	var result BackgroundJob
	_ = json.Unmarshal(data, &result)
	return result
}

func (repo *backgroundJobsMemory) Create(_ context.Context, job BackgroundJob) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.createErr != nil {
		return repo.createErr
	}
	if _, ok := repo.jobs[job.ID]; ok {
		return errors.New("duplicate job")
	}
	repo.jobs[job.ID] = copyBackgroundJob(job)
	repo.created <- job.ID
	return nil
}

func (repo *backgroundJobsMemory) GetOwned(_ context.Context, id string, user, key int64) (BackgroundJob, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	job, ok := repo.jobs[id]
	if !ok || job.UserID != user || job.KeyID != key {
		return BackgroundJob{}, ErrNotFound
	}
	return copyBackgroundJob(job), nil
}

func (repo *backgroundJobsMemory) Pending(_ context.Context, limit int) ([]string, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	var ids []string
	for id, job := range repo.jobs {
		if !job.Billed {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

func (repo *backgroundJobsMemory) Claim(_ context.Context, id, lease string, ttl time.Duration) (BackgroundJob, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	job, ok := repo.jobs[id]
	if !ok {
		return BackgroundJob{}, ErrNotFound
	}
	if job.Billed || (job.LeaseID != lease && job.LeaseUntil.After(time.Now())) {
		return BackgroundJob{}, ErrBackgroundLeaseConflict
	}
	job.LeaseID, job.LeaseUntil = lease, time.Now().Add(ttl)
	repo.jobs[id] = job
	return copyBackgroundJob(job), nil
}

func (repo *backgroundJobsMemory) Save(_ context.Context, job BackgroundJob) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	current, ok := repo.jobs[job.ID]
	if !ok {
		return ErrNotFound
	}
	if current.Billed || current.LeaseID != job.LeaseID || !current.LeaseUntil.After(time.Now()) {
		return ErrBackgroundLeaseConflict
	}
	if job.Billed && repo.failBilledSave {
		repo.failBilledSave = false
		return errors.New("crash after billing before job save")
	}
	job.LeaseUntil = current.LeaseUntil
	job.CancelRequested = job.CancelRequested || current.CancelRequested
	repo.jobs[job.ID] = copyBackgroundJob(job)
	return nil
}

func (repo *backgroundJobsMemory) Append(_ context.Context, id, lease string, event BackgroundEvent) (int64, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	job, ok := repo.jobs[id]
	if !ok {
		return 0, ErrNotFound
	}
	if job.Billed || job.LeaseID != lease || !job.LeaseUntil.After(time.Now()) {
		return 0, ErrBackgroundLeaseConflict
	}
	event.Sequence = int64(len(repo.events[id]))
	event.Payload = bytes.Clone(event.Payload)
	repo.events[id] = append(repo.events[id], event)
	select {
	case repo.appended <- event.Type:
	default:
	}
	return event.Sequence, nil
}

func (repo *backgroundJobsMemory) Events(_ context.Context, id string, after int64, limit int) ([]BackgroundEvent, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if _, ok := repo.jobs[id]; !ok {
		return nil, ErrNotFound
	}
	var result []BackgroundEvent
	for _, event := range repo.events[id] {
		if event.Sequence > after && len(result) < limit {
			event.Payload = bytes.Clone(event.Payload)
			result = append(result, event)
		}
	}
	return result, nil
}

func (repo *backgroundJobsMemory) Cancel(_ context.Context, id string, user, key int64) (BackgroundJob, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	job, ok := repo.jobs[id]
	if !ok || job.UserID != user || job.KeyID != key {
		return BackgroundJob{}, ErrNotFound
	}
	job.CancelRequested = true
	repo.jobs[id] = job
	return copyBackgroundJob(job), nil
}

func (repo *backgroundJobsMemory) expire(id string) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	job := repo.jobs[id]
	job.LeaseUntil = time.Now().Add(-time.Second)
	repo.jobs[id] = job
}

func (repo *backgroundJobsMemory) change(id string, fn func(*BackgroundJob)) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	job := repo.jobs[id]
	fn(&job)
	repo.jobs[id] = job
}
