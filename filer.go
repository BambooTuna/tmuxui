package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
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
	".html": "text/html; charset=utf-8",
	".htm":  "text/html; charset=utf-8",
}

const filerRawMaxSize = 50 << 20 // 50MB

func handleFilerDownload(w http.ResponseWriter, r *http.Request) {
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
	f, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(filePath)}))
	http.ServeContent(w, r, filepath.Base(filePath), info.ModTime(), f)
}

func handleFilerCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir      string `json:"dir"`
		Root     string `json:"root"`
		Filename string `json:"filename"`
		Content  string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Filename == "" || req.Root == "" {
		http.Error(w, "filename and root required", http.StatusBadRequest)
		return
	}
	// .md のみ許可
	if strings.ToLower(filepath.Ext(req.Filename)) != ".md" {
		http.Error(w, "only .md files allowed", http.StatusForbidden)
		return
	}
	// ファイル名にパス区切りを含まないことを確認
	if strings.ContainsAny(req.Filename, "/\\") {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	dir := req.Dir
	if dir == "" {
		dir = req.Root
	}
	dirPath, ok := safeFilerPath(dir, req.Root)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	filePath := filepath.Join(dirPath, req.Filename)

	const maxCreateSize = 1 << 20 // 1MB
	if len(req.Content) > maxCreateSize {
		http.Error(w, "content too large", http.StatusRequestEntityTooLarge)
		return
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if os.IsExist(err) {
		http.Error(w, "file already exists", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "write error", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(req.Content); err != nil {
		http.Error(w, "write error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"name": req.Filename})
}

func handleFilerUpload(w http.ResponseWriter, r *http.Request) {
	const maxUploadSize = 50 << 20 // 50MB
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}

	// アップロード先は常に固定パス (${HOME}/.tmuxui/uploads/YYYY-MM-DD/)
	// クライアントから渡される root/path は無視する (docker で $HOME:ro マウントした際に
	// pane の CWD 配下へ保存できず upload が failed になる問題への対応)
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("HOME")
	}
	if home == "" {
		http.Error(w, "cannot determine home directory", http.StatusInternalServerError)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	filename := filepath.Base(header.Filename)
	if filename == "" || filename == "." || filename == ".." {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}
	// パストラバーサル防止
	if strings.ContainsAny(filename, "/\\") {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	now := time.Now().Local()
	uploadDir := filepath.Join(home, ".tmuxui", "uploads", now.Format("2006-01-02"))
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		http.Error(w, "cannot create upload dir", http.StatusInternalServerError)
		return
	}

	destName := now.Format("150405") + "_" + filename
	destPath := filepath.Join(uploadDir, destName)

	// 同名ファイルがある場合はリネーム (同一秒内の複数アップロード対策)
	if _, err := os.Stat(destPath); err == nil {
		ext := filepath.Ext(destName)
		base := strings.TrimSuffix(destName, ext)
		for i := 1; ; i++ {
			candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
			destPath = filepath.Join(uploadDir, candidate)
			if _, err := os.Stat(destPath); os.IsNotExist(err) {
				break
			}
			if i > 100 {
				http.Error(w, "too many duplicates", http.StatusConflict)
				return
			}
		}
	}

	dst, err := os.Create(destPath)
	if err != nil {
		http.Error(w, "write error", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		os.Remove(destPath)
		http.Error(w, "write error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"path": destPath,
		"size": written,
	})
}

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
	// html はプレビュー用途なので inline script/style/画像を許可する。
	// 他形式 (画像・PDF等) は従来通り厳しい CSP を維持。
	if ext == ".html" || ext == ".htm" {
		w.Header().Set("Content-Security-Policy", "default-src 'self' data: blob:; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; font-src 'self' data: https:")
	} else {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	}
	http.ServeContent(w, r, filepath.Base(filePath), info.ModTime(), f)
}
