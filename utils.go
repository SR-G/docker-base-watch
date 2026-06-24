package main

import "strings"

func SplitAny(s string, separators string) []string {
	splitter := func(r rune) bool {
		return strings.ContainsRune(separators, r)
	}
	return strings.FieldsFunc(s, splitter)
}
