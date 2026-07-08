package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Preferences struct {
	mu       sync.RWMutex
	data     map[string]any
	dirty    bool
	saveCh   chan struct{}
	filePath string
}

func newPreferences() *Preferences {
	home, _ := os.UserHomeDir()
	p := &Preferences{
		data:     map[string]any{},
		saveCh:   make(chan struct{}, 1),
		filePath: filepath.Join(home, ".config", "tmuxui", "preferences.json"),
	}
	p.load()
	go p.saveLoop()
	return p
}

func (p *Preferences) load() {
	data, err := os.ReadFile(p.filePath)
	if err != nil {
		return
	}
	json.Unmarshal(data, &p.data)
}

func (p *Preferences) GetAll() map[string]any {
	p.mu.RLock()
	defer p.mu.RUnlock()
	cp := make(map[string]any, len(p.data))
	for k, v := range p.data {
		cp[k] = v
	}
	return cp
}

func (p *Preferences) Merge(incoming map[string]any) {
	p.mu.Lock()
	for k, v := range incoming {
		p.data[k] = v
	}
	p.dirty = true
	p.mu.Unlock()
	select {
	case p.saveCh <- struct{}{}:
	default:
	}
}

func (p *Preferences) saveLoop() {
	for range p.saveCh {
		time.Sleep(1 * time.Second)
		p.save()
	}
}

func (p *Preferences) updatePinned(transform func([]string) []string) {
	p.mu.Lock()
	cur, _ := p.data["pinnedSessions"].([]any)
	names := make([]string, 0, len(cur))
	for _, v := range cur {
		if s, ok := v.(string); ok {
			names = append(names, s)
		}
	}
	next := transform(names)
	if next == nil {
		p.mu.Unlock()
		return
	}
	out := make([]any, len(next))
	for i, s := range next {
		out[i] = s
	}
	p.data["pinnedSessions"] = out
	p.dirty = true
	p.mu.Unlock()
	select {
	case p.saveCh <- struct{}{}:
	default:
	}
}

func removePinnedSession(name string) {
	if globalPreferences == nil {
		return
	}
	globalPreferences.updatePinned(func(cur []string) []string {
		out := cur[:0]
		changed := false
		for _, n := range cur {
			if n == name {
				changed = true
				continue
			}
			out = append(out, n)
		}
		if !changed {
			return nil
		}
		return out
	})
}

func renamePinnedSession(oldName, newName string) {
	if globalPreferences == nil {
		return
	}
	globalPreferences.updatePinned(func(cur []string) []string {
		changed := false
		for i, n := range cur {
			if n == oldName {
				cur[i] = newName
				changed = true
			}
		}
		if !changed {
			return nil
		}
		return cur
	})
}

func (p *Preferences) save() {
	p.mu.Lock()
	if !p.dirty {
		p.mu.Unlock()
		return
	}
	cp := make(map[string]any, len(p.data))
	for k, v := range p.data {
		cp[k] = v
	}
	p.dirty = false
	p.mu.Unlock()

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(p.filePath)
	os.MkdirAll(dir, 0755)
	os.WriteFile(p.filePath, data, 0644)
}
