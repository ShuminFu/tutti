package claudesidecar

// GoalCommandDispatch mirrors goalExecQueue.ts's dispatch metadata.
type GoalCommandDispatch struct {
	OperationID string
	Revision    float64
	RepairEpoch float64
	Action      string // "set" or "clear"
}

// goalExecInput is one queued goal-carrying exec.
type goalExecInput struct {
	turnID     string
	prompt     string
	content    any
	turnOrigin string
	goal       *GoalCommandDispatch
}

// goalExecQueue mirrors goalExecQueue.ts: goal commands dispatch in order,
// and a clear supersedes queued older sets. Dispatch is deferred to a
// scheduler hook the way the TS version defers with setTimeout(0).
type goalExecQueue struct {
	pending           []goalExecInput
	dispatchScheduled bool
	dispatch          func(goalExecInput)
	schedule          func(func())
	emit              Emitter
}

func newGoalExecQueue(emit Emitter, schedule func(func()), dispatch func(goalExecInput)) *goalExecQueue {
	return &goalExecQueue{
		dispatch: dispatch,
		schedule: schedule,
		emit:     emit,
	}
}

func (q *goalExecQueue) accept(input goalExecInput) {
	if input.goal != nil && input.goal.Action == "clear" {
		retained := make([]goalExecInput, 0, len(q.pending))
		for _, pending := range q.pending {
			if pending.goal != nil && pending.goal.Action == "set" && pending.goal.Revision < input.goal.Revision {
				q.emit("goal_command_superseded", "", map[string]any{
					"turnId":               pending.turnID,
					"operationId":          pending.goal.OperationID,
					"revision":             pending.goal.Revision,
					"action":               pending.goal.Action,
					"supersededByRevision": input.goal.Revision,
				})
				continue
			}
			retained = append(retained, pending)
		}
		q.pending = retained
	}
	q.pending = append(q.pending, input)
	if q.dispatchScheduled {
		return
	}
	q.dispatchScheduled = true
	q.schedule(q.drain)
}

func (q *goalExecQueue) drain() {
	q.dispatchScheduled = false
	pending := q.pending
	q.pending = nil
	for _, input := range pending {
		q.dispatch(input)
	}
}
