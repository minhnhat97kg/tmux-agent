package hook

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"tmux-agent/internal/activity"
	"tmux-agent/internal/clock"
	"tmux-agent/internal/event"
	"tmux-agent/internal/tmux"
)

// notificationCooldownMs is the minimum interval (in milliseconds) before a
// DUPLICATE fingerprint is allowed to notify again. The reference
// implementation uses 120 seconds in epoch-seconds; here it's converted to
// milliseconds to share the same unit as the anti-duplicate stamp (see
// notificationStamp below).
const notificationCooldownMs = 120_000

// notificationBodyMaxChars is the maximum length (in runes) of a
// notification body before it gets truncated and "…" appended.
const notificationBodyMaxChars = 240

// NotificationKind selects which PANE OPTION is used as the anti-duplicate
// stamp — not the event kind itself. Several event Kinds share the same
// NotificationKind: event.Stop, event.TaskCompleted and event.SessionEnd
// (reasons "logout"/"bypass_permissions_disabled") all use
// NotifyTaskCompleted, matching the reference Rust implementation (see
// session.rs::on_session_end, which uses DesktopNotificationKind::
// TaskCompleted for SessionEnd).
type NotificationKind int

const (
	NotifyTaskCompleted NotificationKind = iota
	NotifyTaskFailed
	NotifyPermissionRequired
)

// stampOptionKey returns the pane option used as the anti-duplicate stamp
// for a given NotificationKind.
func stampOptionKey(kind NotificationKind) string {
	switch kind {
	case NotifyTaskFailed:
		return tmux.PaneOSNotifyTaskFailed
	case NotifyPermissionRequired:
		return tmux.PaneOSNotifyPermissionRequired
	default:
		return tmux.PaneOSNotifyTaskCompleted
	}
}

// NotificationSettings is the desktop notification configuration, read from
// @sidebar_notifications and @sidebar_notifications_events.
type NotificationSettings struct {
	// Enabled toggles all desktop notifications on/off. Does NOT check
	// whether an OS backend is available — that's part 4's job, see
	// SendDesktopNotification.
	Enabled bool
	// Events is the set of event Kinds allowed to notify, keyed by the
	// configuration token of @sidebar_notifications_events (see
	// eventTokens). event.SessionEnd does NOT have its own key in this
	// map — it piggybacks on event.Stop's flag, see enabled() below.
	Events map[event.Kind]bool
}

// enabled is true when the event Kind is allowed to notify.
//
// event.SessionEnd piggybacks on event.Stop's flag instead of having its own
// key: the reference implementation (session.rs::on_session_end) has no
// SessionEnd variant in its on/off enum, it reuses
// DesktopNotificationEvent::Stop to check on/off even though it's really a
// different event. @sidebar_notifications_events is a contract with the
// user's tmux.conf — adding a separate "session_end" token would break that
// contract, so the behavior here mirrors the reference implementation
// exactly: turning Stop off also turns SessionEnd off.
//
// For every other Kind, a direct map lookup naturally rejects any Kind
// outside the notification universe (e.g. event.CwdChanged) because the map
// never contains that key.
func (s NotificationSettings) enabled(kind event.Kind) bool {
	if kind == event.SessionEnd {
		kind = event.Stop
	}
	return s.Events[kind]
}

// NotificationEventKinds lists the event Kinds that can emit a desktop
// notification. event.SessionEnd is present here because it's still an
// event kind with its own fingerprint/body (see HandleNotification) — only
// the on/off toggle piggybacks on Stop's flag (see enabled()), it has no
// separate "session_end" configuration token.
func NotificationEventKinds() []event.Kind {
	return []event.Kind{
		event.Stop,
		event.Notification,
		event.TaskCompleted,
		event.StopFailure,
		event.PermissionDenied,
		event.SessionEnd,
	}
}

// DefaultNotificationEvents returns the set of events enabled by default
// when @sidebar_notifications_events hasn't been set. TaskCompleted
// defaults to OFF (opt-in) — matching the reference implementation's
// DesktopNotificationEvent::DEFAULT, which doesn't list TaskCompleted
// because Stop already covers the normal completion case; TaskCompleted is
// only for an agent proactively reporting that a subtask finished. There's
// no event.SessionEnd key — it piggybacks on Stop's flag (see enabled()),
// so it needs no key of its own in the default map.
func DefaultNotificationEvents() map[event.Kind]bool {
	return map[event.Kind]bool{
		event.Stop:             true,
		event.Notification:     true,
		event.StopFailure:      true,
		event.PermissionDenied: true,
	}
}

// eventTokens maps a configuration token (one item in the comma-separated
// list of @sidebar_notifications_events) to a Kind. The token is a
// configuration contract with the user — snake_case, NOT auto-generated
// from the Go constant name. There's NO "session_end" token: SessionEnd
// piggybacks on the "stop" flag (see enabled() above) instead of having its
// own token, to keep the option value the user set in their tmux.conf
// byte-for-byte intact.
var eventTokens = map[string]event.Kind{
	"stop":              event.Stop,
	"notification":      event.Notification,
	"task_completed":    event.TaskCompleted,
	"stop_failure":      event.StopFailure,
	"permission_denied": event.PermissionDenied,
}

// LoadSettings reads the notification configuration from the full set of
// current global options (one child-process call, like every other
// configuration read in the tmux package).
//
// @sidebar_notifications: "on" | "off", any other value (including missing)
// is treated as "on" — a user typo shouldn't accidentally disable
// notifications entirely.
//
// @sidebar_notifications_events: a comma-separated list of tokens (see
// eventTokens), or "all" to enable every Kind in NotificationEventKinds().
// An EMPTY string (set but left blank) disables ALL events — different from
// the option being UNSET, which uses DefaultNotificationEvents(). Unknown
// tokens (including "session_end", which no longer exists) are silently
// ignored, not an error.
//
// The OS backend (osascript/notify-send) is NOT probed here — that's part
// 4's job, once SendDesktopNotification is wired to a real call. Enabled
// here only reflects exactly what the user set in tmux, nothing more.
func LoadSettings() NotificationSettings {
	opts := tmux.GetAllGlobalOptions()
	enabled := readOnOff(opts[tmux.SidebarNotifications], true)

	raw, ok := opts[tmux.SidebarNotificationsEvents]
	if !ok {
		return NotificationSettings{Enabled: enabled, Events: DefaultNotificationEvents()}
	}
	return NotificationSettings{Enabled: enabled, Events: parseNotificationEvents(raw)}
}

// readOnOff parses "on"/"off" case-insensitively; any other value
// (including empty) returns def.
func readOnOff(raw string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on":
		return true
	case "off":
		return false
	default:
		return def
	}
}

// parseNotificationEvents parses the token list of @sidebar_notifications_events.
func parseNotificationEvents(raw string) map[event.Kind]bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		// Option set but left blank = disable every event. Different from
		// "option not set" (LoadSettings already branches to the default
		// before calling this function).
		return map[event.Kind]bool{}
	}
	if strings.EqualFold(trimmed, "all") {
		out := map[event.Kind]bool{}
		for _, k := range NotificationEventKinds() {
			out[k] = true
		}
		return out
	}
	out := map[event.Kind]bool{}
	for _, tok := range strings.Split(trimmed, ",") {
		tok = strings.ToLower(strings.TrimSpace(tok))
		if k, ok := eventTokens[tok]; ok {
			out[k] = true
		}
	}
	return out
}

// SetNotificationRunID records the start timestamp of the pane's current
// run, used as a prefix for every anti-duplicate fingerprint generated
// during that run. Called at SessionStart and UserPromptSubmit (each new
// run) — see clock.NowUnixMilli, generated specifically for this purpose.
func SetNotificationRunID(paneID string) {
	tmux.SetPaneOption(paneID, tmux.PaneNotificationRunID, strconv.FormatInt(clock.NowUnixMilli(), 10))
}

// notificationRunID reads back the run id written by SetNotificationRunID.
// ok = false when it was never written (an old pane, or one that hasn't
// been through SessionStart) or the value is corrupt — both cases are
// treated as "no run", and the fingerprint then carries no prefix.
func notificationRunID(paneID string) (id int64, ok bool) {
	raw := tmux.GetPaneOptionValue(paneID, tmux.PaneNotificationRunID)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// runScopedFingerprint prepends the run id to the original fingerprint, in
// the form "<run_id>:<fingerprint>". With no run id, it returns the
// original fingerprint unchanged.
func runScopedFingerprint(runID int64, hasRunID bool, fingerprint string) string {
	if !hasRunID {
		return fingerprint
	}
	return strconv.FormatInt(runID, 10) + ":" + fingerprint
}

// hasRunScopedStamp is true when the NotificationKind already has an
// anti-duplicate stamp for the EXACT runID — regardless of the specific
// fingerprint (only the "<run_id>:" prefix is compared). Used to avoid
// notifying twice for the same completion when both TaskCompleted
// (explicit) and Stop (implicit) fire within the same run.
func hasRunScopedStamp(paneID string, kind NotificationKind, runID int64, hasRunID bool) bool {
	if !hasRunID || paneID == "" {
		return false
	}
	raw := tmux.GetPaneOptionValue(paneID, stampOptionKey(kind))
	stamp, ok := parseStamp(raw)
	if !ok {
		return false
	}
	return strings.HasPrefix(stamp.fingerprint, strconv.FormatInt(runID, 10)+":")
}

// notificationStamp is the content of an anti-duplicate stamp: the
// millisecond timestamp when it was written, and the normalized fingerprint
// at that time.
type notificationStamp struct {
	timestampMs int64
	fingerprint string
}

// encodeStamp/parseStamp use the format "<milliseconds>|<fingerprint>" —
// identical to the reference implementation (the "|" separator) but the
// timestamp is in milliseconds instead of seconds, to share the same unit as
// @pane_notification_run_id.
func encodeStamp(timestampMs int64, fingerprint string) string {
	return strconv.FormatInt(timestampMs, 10) + "|" + fingerprint
}

func parseStamp(raw string) (notificationStamp, bool) {
	ts, fp, found := strings.Cut(raw, "|")
	if !found {
		return notificationStamp{}, false
	}
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return notificationStamp{}, false
	}
	return notificationStamp{timestampMs: n, fingerprint: fp}, true
}

// notifyIfAllowed is the final decision point: if there's a still-valid
// anti-duplicate stamp (same fingerprint, within notificationCooldownMs),
// skip; otherwise "send" (via SendDesktopNotification) and write a new stamp
// IF sending didn't error.
func notifyIfAllowed(paneID string, kind NotificationKind, fingerprint, title, body string) bool {
	key := stampOptionKey(kind)
	normalized := activity.SanitizeLabel(fingerprint)
	now := clock.NowUnixMilli()

	if stamp, ok := parseStamp(tmux.GetPaneOptionValue(paneID, key)); ok &&
		stamp.fingerprint == normalized &&
		now-stamp.timestampMs < notificationCooldownMs {
		return false
	}

	sendRemote(title, body) // phone push (@agent_notify_url), independent of the desktop
	if err := SendDesktopNotification(title, body); err != nil {
		return false
	}
	tmux.SetPaneOption(paneID, key, encodeStamp(now, normalized))
	return true
}

// notifyCommandTimeout is the maximum time to wait for a desktop
// notification command before it's canceled — matching
// DESKTOP_NOTIFICATION_TIMEOUT (3 seconds) of the reference Rust
// implementation (desktop_notification.rs).
const notifyCommandTimeout = 3 * time.Second

// notifyGOOS is runtime.GOOS extracted into a package-level variable so
// tests can select the platform branch (macOS/Linux/other) without building
// or running on multiple real operating systems.
var notifyGOOS = runtime.GOOS

// runNotifyCommand runs an OS command to send a desktop notification,
// killing the process if it exceeds notifyCommandTimeout. Extracted into a
// package-level variable so tests can swap in a fake (the same pattern as
// tmux.Runner in internal/tmux/commands.go) — tests never call the real
// osascript/notify-send.
//
// exec.CommandContext automatically kills the process when the context
// expires, replacing the manual try_wait/sleep/kill loop of the reference
// Rust implementation (std::process has no built-in deadline-based
// cancellation) — same behavior, less code thanks to Go's context.
// Stdout/Stderr are left nil (exec.Cmd's default) so any command output is
// swallowed and never printed.
var runNotifyCommand = func(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), notifyCommandTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}

// errNotifyUnsupportedOS reports that the current platform has no supported
// desktop notification backend (every OS other than macOS/Linux — e.g.
// Windows, matching the windows/other branch of the reference
// send_desktop_notification, returns this same error rather than a
// distinct one per platform).
var errNotifyUnsupportedOS = errors.New("desktop notification: no backend for this platform")

// SendDesktopNotification calls the real OS command to display a desktop
// notification. Never panics (every process creation/execution error is
// returned by exec.Cmd.Run as an error, not a panic) and never blocks
// longer than notifyCommandTimeout (the context kills the child process if
// it hangs). Errors returned here are silently swallowed by
// notifyIfAllowed — no logging, no breaking the session.
//
// macOS: osascript runs the AppleScript "display notification ... with
// title ..." (see desktop_notification.rs::send_desktop_notification, the
// target_os = "macos" branch). The content must go through
// escapeAppleScript before being embedded in the AppleScript string because
// osascript receives the script as text via "-e", not through a separate
// parameter list like notify-send.
//
// Linux: notify-send, title/body passed as separate exec.Command parameters
// (not through a shell) so no escaping is needed — matching the reference
// implementation's target_os = "linux" branch.
//
// Other platforms: no backend yet, returns errNotifyUnsupportedOS
// immediately without calling runNotifyCommand.
func SendDesktopNotification(title, body string) error {
	switch notifyGOOS {
	case "darwin":
		script := `display notification "` + escapeAppleScript(body) + `" with title "` + escapeAppleScript(title) + `"`
		return runNotifyCommand("osascript", "-e", script)
	case "linux":
		return runNotifyCommand("notify-send", "--app-name=tmux-agent-sidebar", "--urgency=normal", title, body)
	default:
		return errNotifyUnsupportedOS
	}
}

// escapeAppleScript escapes a string so it can be safely embedded in an
// AppleScript string literal in the command `osascript -e "...<value>..."`.
// The order matches the reference implementation
// (desktop_notification.rs::escape_applescript):
//  1. Newlines (\n, \r) become spaces — AppleScript doesn't accept raw
//     newlines inside a string literal.
//  2. Backslashes (\) are doubled FIRST — so that step 3 (escaping double
//     quotes) doesn't escape on top of the backslashes just added.
//  3. Double quotes (") get a \ prefix — otherwise a double quote in the
//     body would end the AppleScript string early and the remainder would
//     be misinterpreted as code.
func escapeAppleScript(value string) string {
	value = strings.NewReplacer("\n", " ", "\r", " ").Replace(value)
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}

// notificationTitle generates the notification title.
//
// DECISION DIFFERING FROM THE REFERENCE IMPLEMENTATION: the Rust version
// composes "repo (branch) / agent" (desktop_notification.rs::format_title),
// reading repo/branch from the session context or from pane options
// (@pane_worktree_branch, @pane_cwd inferring the repo name) when the event
// doesn't carry a full AgentContext (TaskCompleted, SessionEnd). That
// reading and inference is outside the eight functions locked in for this
// task, and the title string content was invisible in part 2 since
// SendDesktopNotification was a stub. Using the agent name as the title for
// now; part 4 can upgrade this at the same time it wires in the real OS
// call. SessionEnd carries no Agent (see event.Event) so it falls back to
// "tmux-agent".
func notificationTitle(evt event.Event) string {
	if evt.Agent != "" {
		return evt.Agent
	}
	return "tmux-agent"
}

func truncateBody(text string) string {
	runes := []rune(text)
	if len(runes) <= notificationBodyMaxChars {
		return text
	}
	return string(runes[:notificationBodyMaxChars]) + "…"
}

func stopBody(lastMessage string) string {
	trimmed := strings.TrimSpace(lastMessage)
	if trimmed == "" {
		return "Task completed"
	}
	return truncateBody(trimmed)
}

func stopFailureFingerprint(errText string) string {
	if errText == "" {
		return "task-failed"
	}
	return errText
}

func stopFailureBody(errText string) string {
	if errText == "" {
		return "Task failed"
	}
	return "Task failed: " + errText
}

func notificationFingerprint(waitReason string) string {
	if waitReason == "" {
		return "notification"
	}
	return waitReason
}

// notificationBody does NOT translate wait_reason into a human-readable
// sentence (the reference implementation does that via
// ui/text.rs::wait_reason_label, e.g. "permission_prompt" → "permission
// required"). That lookup table belongs to the display layer, is outside
// the scope of the hook package, and was unobservable in part 2 (the stub
// swallowed every title/body) — returns wait_reason as-is; part 4 or the
// TUI layer can wire in the label table if nicer wording is needed.
func notificationBody(waitReason string) string {
	if waitReason == "" {
		return "Permission required"
	}
	return waitReason
}

func taskCompletedFingerprint(taskID, taskSubject string) string {
	if taskID != "" {
		return taskID
	}
	if taskSubject != "" {
		return taskSubject
	}
	return "task-completed"
}

func taskCompletedBody(taskSubject string) string {
	if taskSubject == "" {
		return "Task completed"
	}
	return "Task completed: " + taskSubject
}

func sessionEndFingerprint(endReason string) string {
	if endReason == "" {
		return "session-ended"
	}
	return "session-ended:" + endReason
}

func sessionEndBody(endReason string) string {
	if endReason == "" {
		return "Session ended"
	}
	return "Session ended: " + endReason
}

// sessionEndReasonNotifies is the whitelist of session-end reasons WORTH
// notifying about — matching session.rs::on_session_end:
// `matches!(end_reason, "logout" | "bypass_permissions_disabled")`. "Soft"
// reasons (clear, resume, prompt_input_exit, other, or empty) don't notify,
// they end silently.
func sessionEndReasonNotifies(endReason string) bool {
	return endReason == "logout" || endReason == "bypass_permissions_disabled"
}

// HandleNotification decides whether an event.Event should emit a desktop
// notification, and if so, does it: computes the run-scoped fingerprint,
// checks the anti-duplicate stamp, calls SendDesktopNotification (currently
// a stub, see above), then writes a new stamp if "sending" didn't error.
// Returns true when a notification is ACTUALLY considered sent (the stamp
// was written), false on every skip branch.
//
// Never panics, never logs: every tmux read/write error is swallowed by the
// internal/tmux functions themselves that are called (RunTmux swallows
// errors, see the tmux package).
func HandleNotification(paneID string, evt event.Event, settings NotificationSettings) bool {
	if !settings.Enabled || paneID == "" {
		return false
	}
	if !settings.enabled(evt.Kind) {
		return false
	}

	var kind NotificationKind
	var fingerprintSuffix, body string

	switch evt.Kind {
	case event.Stop:
		kind = NotifyTaskCompleted
		fingerprintSuffix = "stop"
		body = stopBody(evt.LastMessage)

	case event.StopFailure:
		kind = NotifyTaskFailed
		fingerprintSuffix = stopFailureFingerprint(evt.Error)
		body = stopFailureBody(evt.Error)

	case event.Notification:
		if evt.MetaOnly {
			// idle_prompt and similar: only refreshes metadata, not an event
			// worth notifying about — matching Event.MetaOnly, see
			// internal/event/event.go.
			return false
		}
		kind = NotifyPermissionRequired
		fingerprintSuffix = notificationFingerprint(evt.WaitReason)
		body = notificationBody(evt.WaitReason)

	case event.PermissionDenied:
		kind = NotifyPermissionRequired
		fingerprintSuffix = "permission_denied"
		body = "Permission required"

	case event.TaskCompleted:
		kind = NotifyTaskCompleted
		fingerprintSuffix = taskCompletedFingerprint(evt.TaskID, evt.TaskSubject)
		body = taskCompletedBody(evt.TaskSubject)

	case event.SessionEnd:
		if !sessionEndReasonNotifies(evt.EndReason) {
			return false
		}
		kind = NotifyTaskCompleted
		fingerprintSuffix = sessionEndFingerprint(evt.EndReason)
		body = sessionEndBody(evt.EndReason)

	default:
		// Not part of the desktop notification universe (e.g.
		// event.CwdChanged, event.SubagentStart, ...). settings.enabled()
		// above usually already blocks this before we get here since Events
		// never contains these Kinds; this branch is a second safety net.
		return false
	}

	runID, hasRunID := notificationRunID(paneID)
	if evt.Kind == event.Stop && hasRunScopedStamp(paneID, NotifyTaskCompleted, runID, hasRunID) {
		// An explicit TaskCompleted already fired for this exact run — Stop
		// doesn't notify again for the same completion (matching the
		// reference implementation's on_stop).
		return false
	}

	fingerprint := runScopedFingerprint(runID, hasRunID, fingerprintSuffix)
	return notifyIfAllowed(paneID, kind, fingerprint, notificationTitle(evt), body)
}
