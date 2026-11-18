package scrape

import (
	"regexp"
	"strings"
	"time"

	"tmux-agent/internal/hook"
	"tmux-agent/internal/tmux"
)

// Dialog is an approval/question prompt read off an agent's screen.
type Dialog struct {
	Question string
	Options  []Option
}

// Option is one answer: the key that picks it, and its text.
type Option struct {
	Key, Label string
}

var (
	optionRe = regexp.MustCompile(`^\s*(?:[❯›>▶]\s*)?(\d)[.)]\s+(.+?)\s*$`)
	yesNoRe  = regexp.MustCompile(`(?i)[\[(]y/n[\])]`)
	boxChars = "│┃|╎╏┆┊ \t"
)

// ParseDialog finds the last prompt on screen (the blockedRe line Classify
// uses) and the numbered options under it — Claude Code's "❯ 1. Yes /
// 2. Yes, and don't ask again… / 3. No" — or a [y/n] question. ok=false
// when there's no prompt or no recognizable options.
func ParseDialog(screen string) (Dialog, bool) {
	// A capture is padded with blank lines to the pane height; drop them so
	// "the last lines" means the end of the content, not of the pane.
	lines := strings.Split(strings.TrimRight(screen, "\n \t"), "\n")
	q := -1
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-tailLines-10; i-- {
		if blockedRe.MatchString(lines[i]) && !optionRe.MatchString(lines[i]) && !strings.Contains(strings.ToLower(lines[i]), "enter to confirm") {
			q = i
			break
		}
	}
	if q < 0 {
		return Dialog{}, false
	}
	d := Dialog{Question: strings.Trim(lines[q], boxChars)}
	if yesNoRe.MatchString(lines[q]) {
		d.Options = []Option{{"y", "Yes"}, {"n", "No"}}
		return d, true
	}
	for _, l := range lines[q+1:] {
		if m := optionRe.FindStringSubmatch(l); m != nil {
			d.Options = append(d.Options, Option{Key: m[1], Label: m[2]})
		}
	}
	return d, len(d.Options) > 0
}

// AnswerKeys is what to type to pick option key: a digit selects in
// numbered menus; y/n questions want the letter then Enter.
func AnswerKeys(key string) []string {
	if key == "y" || key == "n" {
		return []string{key, "Enter"}
	}
	return []string{key}
}

// Answer types the answer for option key into pane, then moves the pane out
// of `waiting` as soon as the dialog is gone — no hook reports an answer,
// and waiting for the scraper's ticks leaves "needs approval" up for
// seconds. Running shows at once; idle must hold for idleHold, since the
// screen is briefly idle between the dialog closing and the spinner. If
// neither settles within answerWait, the scraper takes over.
func Answer(pane, key string) {
	tmux.RunTmux(append([]string{"send-keys", "-t", pane}, AnswerKeys(key)...)...)
	const (
		poll       = 100 * time.Millisecond
		idleHold   = 5 // polls
		answerWait = 20
	)
	idle := 0
	for i := 0; i < answerWait; i++ {
		time.Sleep(poll)
		screen, ok := tmux.RunTmux("capture-pane", "-p", "-t", pane)
		if !ok {
			return
		}
		next := Classify(screen)
		if next == tmux.StatusIdle {
			idle++
		} else {
			idle = 0
		}
		if next == tmux.StatusRunning || idle >= idleHold {
			if tmux.GetPaneOptionValue(pane, tmux.PaneStatusOpt) == string(tmux.StatusWaiting) {
				hook.Transition(pane, tmux.GetPaneOptionValue(pane, tmux.PaneAgent), tmux.StatusWaiting, next)
			}
			return
		}
	}
}
