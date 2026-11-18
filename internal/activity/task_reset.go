package activity

// TaskResetMarker is a special tool_name written into the activity log to
// mark "treat the upcoming run as an entirely new batch of tasks" — not an
// actual tool invocation. Both the writer side (OnStop/OnStopFailure in
// internal/hook, part 2b) and the task-progress reader (the activity log
// parser, part 3-4) need this constant, so it lives in internal/activity
// (a package both sides import) instead of internal/hook.
const TaskResetMarker = "__task_reset__"
