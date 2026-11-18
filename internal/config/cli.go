package config

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Dispatch implements `tmux-agent config [list | get <key> | set <key>
// <value> | reset <key> | path]`; no arguments opens the settings screen.
func Dispatch(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return RunUI()
	}
	switch args[0] {
	case "list":
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "KEY\tVALUE\tDEFAULT\tGROUP")
		for _, s := range Settings {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Key, Display(Get(s)), Display(s.Default), s.Group)
		}
		w.Flush()
		return 0
	case "path":
		fmt.Fprintln(stdout, ConfigFile())
		return 0
	case "get", "set", "reset":
		if len(args) < 2 {
			break
		}
		s, ok := Find(args[1])
		if !ok {
			fmt.Fprintf(stderr, "config: unknown setting %q (see `tmux-agent config list`)\n", args[1])
			return 2
		}
		switch args[0] {
		case "get":
			fmt.Fprintln(stdout, Get(s))
		case "reset":
			old := Get(s)
			if err := Reset(s); err != nil {
				fmt.Fprintln(stderr, "config:", err)
				return 1
			}
			if s.Reload {
				ReloadKeys(old)
			}
		case "set":
			if len(args) < 3 {
				break
			}
			old := Get(s)
			v, err := Set(s, strings.Join(args[2:], " "))
			if err != nil {
				fmt.Fprintln(stderr, "config:", err)
				return 2
			}
			if s.Reload {
				ReloadKeys(old)
			}
			fmt.Fprintf(stdout, "%s = %s\n", s.Key, Display(v))
		}
		return 0
	}
	fmt.Fprintln(stderr, "usage: tmux-agent config [list | get <key> | set <key> <value> | reset <key> | path]")
	return 2
}
