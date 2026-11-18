package main

import "testing"

func TestPlanPlacement(t *testing.T) {
	cases := []struct {
		name, panes string
		max         int
		want        placement
	}{
		{"no pool window yet", "@1||sidebar|%1|30|40\n@1|||%2|150|40", 4, placement{}},
		{"one wide pane splits side by side",
			"@5|1|sidebar|%9|30|40\n@5|1||%10|150|40", 4, placement{"%10", "-h"}},
		{"largest of two stacked panes splits",
			"@5|1||%10|75|40\n@5|1||%11|75|20", 4, placement{"%10", "-v"}},
		{"full window opens a new one (sidebar not counted)",
			"@5|1|sidebar|%9|30|40\n@5|1||%10|60|20\n@5|1||%11|60|20", 2, placement{}},
		{"full first window, room in the second",
			"@5|1||%10|60|20\n@5|1||%11|60|20\n@7|1||%20|150|40", 2, placement{"%20", "-h"}},
	}
	for _, c := range cases {
		if got := planPlacement(c.panes, c.max); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}
