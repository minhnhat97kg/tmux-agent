package hook

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"tmux-agent/internal/tmux"
)

// Remote notifications: the same events as the desktop notification, pushed
// to your phone when you're away from the machine.
//
//	set -g @agent_notify_url https://ntfy.sh/<your-topic>
//	set -g @agent_notify_url 'https://api.telegram.org/bot<TOKEN>/sendMessage?chat_id=<ID>'
//
// ntfy (or anything accepting a plain-text POST) gets the body with the
// title in a Title header; a Telegram bot URL gets a `text` form field.
const notifyURLOption = "@agent_notify_url"

var remoteClient = &http.Client{Timeout: 3 * time.Second} // hooks wait on this

// sendRemote posts the notification to @agent_notify_url, if set. Errors
// are swallowed: a phone push must never break a hook.
func sendRemote(title, body string) {
	target, ok := tmux.GetOption(notifyURLOption)
	if !ok {
		return
	}
	postRemote(target, title, body)
}

func postRemote(target, title, body string) error {
	var req *http.Request
	var err error
	if strings.Contains(target, "api.telegram.org/bot") {
		form := url.Values{"text": {title + "\n" + body}}
		req, err = http.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	} else {
		req, err = http.NewRequest(http.MethodPost, target, strings.NewReader(body))
		if err == nil {
			req.Header.Set("Title", title)
			req.Header.Set("Tags", "robot")
		}
	}
	if err != nil {
		return err
	}
	resp, err := remoteClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Notify sends a one-off notification (desktop + phone) for events the
// hook handlers don't cover — the watchdog's stuck/conflict warnings. It
// respects @sidebar_notifications; the caller dedupes.
func Notify(title, body string) {
	if !LoadSettings().Enabled {
		return
	}
	sendRemote(title, body)
	SendDesktopNotification(title, body)
}
