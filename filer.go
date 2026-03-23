package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func safeFilerPath(requested, root string) (string, bool) {
	if root == "" {
		return "", false
	}
	rootClean := filepath.Clean(root)
	rootResolved, err := filepath.EvalSymlinks(rootClean)
	if err != nil {
		return "", false
	}

	if requested == "" {
		return rootResolved, true
	}
	abs := filepath.Clean(requested)
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(rootResolved, abs)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", false
	}
	if !strings.HasPrefix(resolved, rootResolved) {
		return "", false
	}
	return resolved, true
}

type filerEntry struct {
	Name      string `json:"name"`
	IsDir     bool   `json:"isDir"`
	Size      int64  `json:"size"`
	GitStatus string `json:"gitStatus,omitempty"`
}

func handleFilerList(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	dirPath, ok := safeFilerPath(r.URL.Query().Get("path"), root)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	info, err := os.Stat(dirPath)
	if err != nil || !info.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		http.Error(w, "cannot read directory", http.StatusInternalServerError)
		return
	}

	const maxEntries = 500
	var files []filerEntry
	for _, e := range entries {
		if len(files) >= maxEntries {
			break
		}
		eInfo, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, filerEntry{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Size:  eInfo.Size(),
		})
	}
	if files == nil {
		files = []filerEntry{}
	}

	gitStatuses := getGitStatuses(dirPath)
	if gitStatuses != nil {
		for i := range files {
			if s, ok := gitStatuses[files[i].Name]; ok {
				files[i].GitStatus = s
			}
		}
	}

	sort.SliceStable(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return files[i].Name < files[j].Name
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"path":    dirPath,
		"entries": files,
	})
}

func handleFilerRead(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	filePath, ok := safeFilerPath(r.URL.Query().Get("path"), root)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	info, err := os.Stat(filePath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "is a directory", http.StatusBadRequest)
		return
	}

	const maxSize = 1 << 20 // 1MB
	if info.Size() > maxSize {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"path":   filePath,
			"binary": true,
			"reason": "file too large",
			"size":   info.Size(),
		})
		return
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}

	checkLen := len(data)
	if checkLen > 8192 {
		checkLen = 8192
	}
	isBinary := !utf8.Valid(data[:checkLen]) || strings.ContainsRune(string(data[:checkLen]), 0)

	w.Header().Set("Content-Type", "application/json")
	if isBinary {
		json.NewEncoder(w).Encode(map[string]any{
			"path":   filePath,
			"binary": true,
			"reason": "binary file",
			"size":   info.Size(),
		})
	} else {
		json.NewEncoder(w).Encode(map[string]any{
			"path":    filePath,
			"binary":  false,
			"content": string(data),
			"size":    info.Size(),
		})
	}
}

func getGitStatuses(dirPath string) map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", dirPath, "status", "--porcelain=v1", ".")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	result := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		status := strings.TrimRight(line[:2], " ")
		if status == "" {
			status = line[:2]
		}
		filePath := strings.TrimSpace(line[3:])
		name := filePath
		if idx := strings.Index(name, "/"); idx >= 0 {
			name = name[:idx]
		}
		if _, exists := result[name]; !exists {
			result[name] = status
		}
	}
	return result
}

var filerRawAllowed = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".svg":  "image/svg+xml",
	".webp": "image/webp",
	".pdf":  "application/pdf",
	".ico":  "image/x-icon",
	".bmp":  "image/bmp",
	".mp4":  "video/mp4",
	".mov":  "video/quicktime",
	".webm": "video/webm",
	".m4v":  "video/mp4",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".m4a":  "audio/mp4",
	".ogg":  "audio/ogg",
	".aac":  "audio/aac",
}

const filerRawMaxSize = 50 << 20 // 50MB

func handleFilerRaw(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	filePath, ok := safeFilerPath(r.URL.Query().Get("path"), root)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	info, err := os.Stat(filePath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "is a directory", http.StatusBadRequest)
		return
	}
	if info.Size() > filerRawMaxSize {
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	mime, allowed := filerRawAllowed[ext]
	if !allowed {
		http.Error(w, "unsupported file type", http.StatusForbidden)
		return
	}

	f, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	http.ServeContent(w, r, filepath.Base(filePath), info.ModTime(), f)
}
