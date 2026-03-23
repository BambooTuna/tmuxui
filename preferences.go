package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
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
		// debounce と同じく少し待ってから保存
		p.save()
	}
}

func (p *Preferences) save() {
	p.mu.RLock()
	if !p.dirty {
		p.mu.RUnlock()
		return
	}
	cp := make(map[string]any, len(p.data))
	for k, v := range p.data {
		cp[k] = v
	}
	p.mu.RUnlock()

	p.mu.Lock()
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
