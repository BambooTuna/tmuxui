package main

import "strings"

var permissionPatterns = []string{
	"Allow",
	"Do you want to",
}

func detectPermission(content string) (bool, string) {
	lines := strings.Split(content, "\n")
	start := len(lines) - 20
	if start < 0 {
		start = 0
	}
	for _, line := range lines[start:] {
		for _, p := range permissionPatterns {
			if strings.Contains(line, p) {
				return true, line
			}
		}
	}
	return false, ""
}
