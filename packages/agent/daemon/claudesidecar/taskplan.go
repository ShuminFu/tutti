package claudesidecar

// taskPlanTracker mirrors taskPlan.ts: TaskCreated/TaskCompleted hook events
// become an ordered plan_updated projection.
type taskPlanTracker struct {
	order        []string
	tasks        map[string]*claudeTaskState
	activeTurnID func() string
	emit         Emitter
}

type claudeTaskState struct {
	id          string
	subject     string
	description string
	status      string
}

func newTaskPlanTracker(activeTurnID func() string, emit Emitter) *taskPlanTracker {
	return &taskPlanTracker{
		tasks:        map[string]*claudeTaskState{},
		activeTurnID: activeTurnID,
		emit:         emit,
	}
}

func (t *taskPlanTracker) reset() {
	t.tasks = map[string]*claudeTaskState{}
	t.order = nil
}

func (t *taskPlanTracker) create(id string, subject string, description string) bool {
	if id == "" || subject == "" {
		return false
	}
	if _, exists := t.tasks[id]; exists {
		return false
	}
	t.tasks[id] = &claudeTaskState{
		id:          id,
		subject:     subject,
		description: description,
		status:      "pending",
	}
	t.order = append(t.order, id)
	t.emitUpdated()
	return true
}

func (t *taskPlanTracker) complete(id string) bool {
	existing, exists := t.tasks[id]
	if !exists || existing.status == "completed" {
		return false
	}
	existing.status = "completed"
	t.emitUpdated()
	return true
}

func (t *taskPlanTracker) emitUpdated() {
	entries := make([]map[string]any, 0, len(t.order))
	for _, id := range t.order {
		task := t.tasks[id]
		if task == nil {
			continue
		}
		entry := map[string]any{
			"id":      task.id,
			"content": task.subject,
			"status":  task.status,
		}
		if task.description != "" {
			entry["description"] = task.description
		}
		entries = append(entries, entry)
	}
	t.emit("plan_updated", "", map[string]any{
		"turnId":  t.activeTurnID(),
		"entries": entries,
	})
}
