package logging

import (
	"fmt"
	"sync"
	"time"
)

type jobState struct {
	failed  bool
	message string
}

var (
	jobStateMu sync.Mutex
	jobStates  = map[string]jobState{}
	valueState = map[string]string{}
)

// AppendJobLog writes a single internal background job event to job.log.
func AppendJobLog(job string, event string, outcome string, message string, fields map[string]any) {
	rotateConfiguredLogs()

	entry := map[string]any{
		"time":    time.Now().Format(time.RFC3339Nano),
		"type":    "job_event",
		"job":     defaultString(job, "job"),
		"event":   defaultString(event, "event"),
		"outcome": defaultString(outcome, "info"),
	}
	if message = sanitizeLogString(message, actionMessageLogLimit); message != "" {
		entry["message"] = message
	}
	for key, value := range fields {
		if key == "" || value == nil {
			continue
		}
		entry[key] = value
	}
	writeJSONLog(resolveJobLogPath(), &jobLogMu, entry)
}

// RecordJobResult logs only meaningful transitions: first failure, changed
// failure message, and recovery from a previous failure.
func RecordJobResult(job string, err error, fields map[string]any) {
	key := defaultString(job, "job")
	if err != nil {
		message := err.Error()
		if markJobFailureChanged(key, message) {
			AppendJobLog(key, "failed", "error", message, fields)
		}
		return
	}
	if markJobRecovered(key) {
		AppendJobLog(key, "recovered", "success", "job recovered", fields)
	}
}

func RecordJobPanic(job string, recovered any, fields map[string]any) {
	message := fmt.Sprint(recovered)
	if markJobFailureChanged(defaultString(job, "job"), message) {
		AppendJobLog(job, "panic", "error", message, fields)
	}
}

// RecordJobState logs the first observed value and subsequent value changes for
// a named state, without writing repeated unchanged samples.
func RecordJobState(job string, state string, value string, fields map[string]any) {
	if value == "" {
		return
	}
	key := defaultString(job, "job") + ":" + defaultString(state, "state")
	previous, changed, observed := markJobStateValue(key, value)
	if !changed {
		return
	}

	event := "state_observed"
	message := state + " observed"
	outcome := "info"
	if !observed {
		event = "state_changed"
		message = state + " changed"
		outcome = "change"
	}

	eventFields := map[string]any{
		"state": state,
		"value": value,
	}
	if previous != "" {
		eventFields["previous"] = previous
	}
	for key, fieldValue := range fields {
		eventFields[key] = fieldValue
	}
	AppendJobLog(job, event, outcome, message, eventFields)
}

func markJobFailureChanged(job string, message string) bool {
	jobStateMu.Lock()
	defer jobStateMu.Unlock()

	current := jobStates[job]
	if current.failed && current.message == message {
		return false
	}
	jobStates[job] = jobState{failed: true, message: message}
	return true
}

func markJobStateValue(key string, value string) (string, bool, bool) {
	jobStateMu.Lock()
	defer jobStateMu.Unlock()

	previous, ok := valueState[key]
	if ok && previous == value {
		return previous, false, false
	}
	valueState[key] = value
	return previous, true, !ok
}

func markJobRecovered(job string) bool {
	jobStateMu.Lock()
	defer jobStateMu.Unlock()

	current := jobStates[job]
	if !current.failed {
		return false
	}
	delete(jobStates, job)
	return true
}
