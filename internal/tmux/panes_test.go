package tmux

import "testing"

func TestSidebarPaneInfoParses(t *testing.T) {
	fake := NewFake()
	fake.Set("1 1 40 30\n", "display-message", "-t", "%7", "-p",
		"#{pane_active} #{window_active} #{pane_width} #{pane_height}")
	defer SetRunner(fake)()

	paneActive, windowActive, w, h := SidebarPaneInfo("%7")
	if !paneActive || !windowActive || w != 40 || h != 30 {
		t.Fatalf("= (%v, %v, %d, %d)", paneActive, windowActive, w, h)
	}
}

// tmux error or the pane just closed → safe defaults, not 0 (divide-by-zero when drawing).
func TestSidebarPaneInfoDefaultsOnError(t *testing.T) {
	defer SetRunner(NewFake())()

	paneActive, windowActive, w, h := SidebarPaneInfo("%7")
	if paneActive || windowActive || w != defaultSidebarWidth || h != defaultSidebarHeight {
		t.Fatalf("= (%v, %v, %d, %d)", paneActive, windowActive, w, h)
	}
}

func TestActiveWindowPanesParses(t *testing.T) {
	fake := NewFake()
	fake.Set("%1|0|/home\n%2|1|/work\njunk-line\n", "list-panes", "-F",
		"#{pane_id}|#{pane_active}|#{pane_current_path}")
	defer SetRunner(fake)()

	panes := ActiveWindowPanes()
	if len(panes) != 2 {
		t.Fatalf("want 2 panes, got %+v", panes)
	}
	if panes[1] != (PaneRef{ID: "%2", Active: true, Path: "/work"}) {
		t.Fatalf("second pane = %+v", panes[1])
	}
}

func TestPickActivePane(t *testing.T) {
	cases := []struct {
		name    string
		sidebar string
		panes   []PaneRef
		want    PaneRef
		wantOK  bool
	}{
		{
			name:    "returns the active pane that is not the sidebar",
			sidebar: "%99",
			panes: []PaneRef{
				{ID: "%1", Active: false, Path: "/home"},
				{ID: "%2", Active: true, Path: "/work"},
			},
			want:   PaneRef{ID: "%2", Active: true, Path: "/work"},
			wantOK: true,
		},
		{
			name:    "skips the sidebar even if it is active",
			sidebar: "%99",
			panes:   []PaneRef{{ID: "%99", Active: true, Path: "/a"}},
			wantOK:  false,
		},
		{
			name:    "skips a pane with no path",
			sidebar: "%99",
			panes: []PaneRef{
				{ID: "%1", Active: true, Path: ""},
				{ID: "%2", Active: true, Path: "/ok"},
			},
			want:   PaneRef{ID: "%2", Active: true, Path: "/ok"},
			wantOK: true,
		},
		{name: "empty list", sidebar: "%99", wantOK: false},
		{
			name:    "no pane is active",
			sidebar: "%99",
			panes:   []PaneRef{{ID: "%1", Path: "/x"}, {ID: "%2", Path: "/y"}},
			wantOK:  false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := pickActivePane(c.sidebar, c.panes)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if ok && got != c.want {
				t.Fatalf("got = %+v, want %+v", got, c.want)
			}
		})
	}
}
