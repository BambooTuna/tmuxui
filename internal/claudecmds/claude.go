// Package claudecmds discovers Claude Code's built-in slash commands, either by scanning
// strings out of the installed `claude` binary or falling back to a hardcoded list.
package claudecmds

import (
	"os/exec"
	"regexp"
	"sort"
	"sync"
)

type Command struct {
	Name string `json:"name"`
	Desc string `json:"description"`
}

var (
	once sync.Once
	all  []Command
)

// Load はClaude Codeのスラッシュコマンド一覧を返す(初回呼び出し時にのみ実際に解析し、以降はキャッシュを返す)。
func Load() []Command {
	once.Do(func() {
		cmds := parseFromBinary()
		if len(cmds) < 10 {
			cmds = defaultCommands()
		}
		all = cmds
	})
	return all
}

var cmdPattern = regexp.MustCompile(`name:"([^"]+)",description:"([^"]+)"`)
var validName = regexp.MustCompile(`^[a-z][a-z-]*$`)

func parseFromBinary() []Command {
	path, err := exec.LookPath("claude")
	if err != nil {
		return nil
	}

	out, err := exec.Command("strings", path).Output()
	if err != nil {
		return nil
	}

	seen := map[string]bool{}
	var cmds []Command
	for _, m := range cmdPattern.FindAllStringSubmatch(string(out), -1) {
		name, desc := m[1], m[2]
		if len(name) > 25 {
			continue
		}
		if !validName.MatchString(name) {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		cmds = append(cmds, Command{Name: name, Desc: desc})
	}
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	return cmds
}

func defaultCommands() []Command {
	return []Command{
		{Name: "bug", Desc: "Report a bug"},
		{Name: "clear", Desc: "Clear conversation history"},
		{Name: "compact", Desc: "Compact conversation"},
		{Name: "config", Desc: "Open config"},
		{Name: "context", Desc: "Show context usage"},
		{Name: "copy", Desc: "Copy last response"},
		{Name: "cost", Desc: "Show token usage stats"},
		{Name: "debug", Desc: "Check debug logs"},
		{Name: "desktop", Desc: "Hand off to Desktop app"},
		{Name: "doctor", Desc: "Health check"},
		{Name: "exit", Desc: "Exit REPL"},
		{Name: "export", Desc: "Export conversation"},
		{Name: "fast", Desc: "Toggle fast mode"},
		{Name: "help", Desc: "Show help"},
		{Name: "init", Desc: "Initialize with CLAUDE.md"},
		{Name: "login", Desc: "Login"},
		{Name: "logout", Desc: "Logout"},
		{Name: "mcp", Desc: "MCP server management"},
		{Name: "memory", Desc: "Edit CLAUDE.md memory"},
		{Name: "model", Desc: "Change AI model"},
		{Name: "permissions", Desc: "Permission settings"},
		{Name: "plan", Desc: "Enter plan mode"},
		{Name: "pr-comments", Desc: "Show PR comments"},
		{Name: "rename", Desc: "Rename session"},
		{Name: "resume", Desc: "Resume conversation"},
		{Name: "review", Desc: "Code review"},
		{Name: "rewind", Desc: "Rewind conversation"},
		{Name: "stats", Desc: "Show usage stats"},
		{Name: "status", Desc: "Version and model info"},
		{Name: "statusline", Desc: "Statusline settings"},
		{Name: "tasks", Desc: "Background task management"},
		{Name: "teleport", Desc: "Resume remote session"},
		{Name: "terminal-setup", Desc: "Terminal setup"},
		{Name: "theme", Desc: "Change color theme"},
		{Name: "todos", Desc: "Show TODO list"},
		{Name: "usage", Desc: "Show plan usage limits"},
		{Name: "vim", Desc: "Toggle vim mode"},
	}
}
