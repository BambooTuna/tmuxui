package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type ActivityTracker struct {
	mu        sync.RWMutex
	activity  map[string]time.Time
	dirty     bool
	saveCh    chan struct{}
	filePath  string
}

func newActivityTracker() *ActivityTracker {
	home, _ := os.UserHomeDir()
	at := &ActivityTracker{
		activity: map[string]time.Time{},
		saveCh:   make(chan struct{}, 1),
		filePath: filepath.Join(home, ".config", "tmuxui", "activity.json"),
	}
	at.load()
	go at.saveLoop()
	return at
}

func (at *ActivityTracker) load() {
	data, err := os.ReadFile(at.filePath)
	if err != nil {
		return
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	for k, v := range raw {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			at.activity[k] = t
		}
	}
}

func (at *ActivityTracker) Touch(sessionName string) {
	at.mu.Lock()
	at.activity[sessionName] = time.Now()
	at.dirty = true
	at.mu.Unlock()
	select {
	case at.saveCh <- struct{}{}:
	default:
	}
}

func (at *ActivityTracker) Get(sessionName string) (int64, bool) {
	at.mu.RLock()
	t, ok := at.activity[sessionName]
	at.mu.RUnlock()
	if !ok {
		return 0, false
	}
	return t.Unix(), true
}

func (at *ActivityTracker) BuildMap(sessions []Session) map[string]int64 {
	at.mu.RLock()
	defer at.mu.RUnlock()
	m := map[string]int64{}
	for _, s := range sessions {
		if t, ok := at.activity[s.Name]; ok {
			m[s.Name] = t.Unix()
		}
	}
	return m
}

func (at *ActivityTracker) Remove(sessionName string) {
	at.mu.Lock()
	delete(at.activity, sessionName)
	at.dirty = true
	at.mu.Unlock()
	select {
	case at.saveCh <- struct{}{}:
	default:
	}
}

func (at *ActivityTracker) Rename(oldName, newName string) {
	at.mu.Lock()
	if t, ok := at.activity[oldName]; ok {
		at.activity[newName] = t
		delete(at.activity, oldName)
		at.dirty = true
	}
	at.mu.Unlock()
	select {
	case at.saveCh <- struct{}{}:
	default:
	}
}

func (at *ActivityTracker) Cleanup(validSessions []string) {
	valid := map[string]struct{}{}
	for _, s := range validSessions {
		valid[s] = struct{}{}
	}
	at.mu.Lock()
	changed := false
	for k := range at.activity {
		if _, ok := valid[k]; !ok {
			delete(at.activity, k)
			changed = true
		}
	}
	if changed {
		at.dirty = true
	}
	at.mu.Unlock()
	if changed {
		select {
		case at.saveCh <- struct{}{}:
		default:
		}
	}
}

func (at *ActivityTracker) saveLoop() {
	for range at.saveCh {
		time.Sleep(3 * time.Second)
		at.save()
	}
}

func (at *ActivityTracker) save() {
	at.mu.Lock()
	if !at.dirty {
		at.mu.Unlock()
		return
	}
	raw := make(map[string]string, len(at.activity))
	for k, v := range at.activity {
		raw[k] = v.Format(time.RFC3339)
	}
	at.dirty = false
	at.mu.Unlock()

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(at.filePath)
	os.MkdirAll(dir, 0755)
	os.WriteFile(at.filePath, data, 0644)
}
