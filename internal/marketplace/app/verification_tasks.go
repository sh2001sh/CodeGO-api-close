package app

import (
	"context"
	"sync"
)

const (
	verificationTaskConnectivity = "connectivity"
	verificationTaskGPT56Mapping = "gpt56_mapping"
)

type verificationTaskKey struct {
	channelID string
	kind      string
}

type verificationTaskRegistry struct {
	mu     sync.Mutex
	nextID uint64
	tasks  map[verificationTaskKey]verificationTask
}

type verificationTask struct {
	id     uint64
	cancel context.CancelFunc
}

var marketplaceVerificationTasks = verificationTaskRegistry{
	tasks: make(map[verificationTaskKey]verificationTask),
}

func (registry *verificationTaskRegistry) begin(
	parent context.Context,
	channelID string,
	kind string,
) (context.Context, func(), bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	key := verificationTaskKey{channelID: channelID, kind: kind}
	if _, exists := registry.tasks[key]; exists {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(parent)
	registry.nextID++
	task := verificationTask{id: registry.nextID, cancel: cancel}
	registry.tasks[key] = task
	return ctx, func() { registry.finish(key, task.id) }, true
}

func (registry *verificationTaskRegistry) finish(key verificationTaskKey, taskID uint64) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if current, exists := registry.tasks[key]; exists && current.id == taskID {
		delete(registry.tasks, key)
	}
}

func (registry *verificationTaskRegistry) cancelChannel(channelID string) bool {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	canceled := false
	for key, task := range registry.tasks {
		if key.channelID != channelID {
			continue
		}
		task.cancel()
		delete(registry.tasks, key)
		canceled = true
	}
	return canceled
}

func (registry *verificationTaskRegistry) active(channelID, kind string) bool {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	_, exists := registry.tasks[verificationTaskKey{channelID: channelID, kind: kind}]
	return exists
}
