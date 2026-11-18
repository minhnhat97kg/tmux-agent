package main

import (
	"reflect"
	"testing"
)

func TestParseTeam(t *testing.T) {
	n, r := parseTeam(" w1  rev@reviewer ")
	if !reflect.DeepEqual(n, []string{"w1", "rev"}) || !reflect.DeepEqual(r, []string{"", "reviewer"}) {
		t.Fatalf("got %v %v", n, r)
	}
}
