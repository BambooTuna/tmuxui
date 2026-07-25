package httpapi

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSafeFilerPathNormalCases(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	file := filepath.Join(sub, "note.txt")
	if err := os.WriteFile(file, []byte("hi"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks(root): %v", err)
	}

	// requested == "" resolves to root itself.
	if got, ok := safeFilerPath("", root); !ok || got != rootResolved {
		t.Errorf("safeFilerPath(\"\", root) = (%q, %v), want (%q, true)", got, ok, rootResolved)
	}

	// relative path under root resolves to the joined, symlink-resolved path.
	want := filepath.Join(rootResolved, "sub", "note.txt")
	if got, ok := safeFilerPath("sub/note.txt", root); !ok || got != want {
		t.Errorf("safeFilerPath(sub/note.txt, root) = (%q, %v), want (%q, true)", got, ok, want)
	}

	// absolute path that happens to already be inside root also resolves fine.
	if got, ok := safeFilerPath(file, root); !ok || got != want {
		t.Errorf("safeFilerPath(abs inside root) = (%q, %v), want (%q, true)", got, ok, want)
	}
}

func TestSafeFilerPathRejectsTraversalOutsideRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cases := []string{
		"../outside.txt",
		"../../etc/passwd",
		"sub/../../escape.txt",
	}
	for _, requested := range cases {
		if _, ok := safeFilerPath(requested, root); ok {
			t.Errorf("safeFilerPath(%q, root) = ok, want forbidden (path traversal)", requested)
		}
	}
}

func TestSafeFilerPathRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	// Requesting through the in-root symlink that points outside root must be rejected,
	// even though the symlink file itself lives inside root.
	if _, ok := safeFilerPath("escape/secret.txt", root); ok {
		t.Error("safeFilerPath via symlink escaping root = ok, want forbidden")
	}
}

func TestSafeFilerPathEmptyRootIsForbidden(t *testing.T) {
	if _, ok := safeFilerPath("anything", ""); ok {
		t.Error("safeFilerPath with empty root = ok, want forbidden")
	}
}

func TestSafeFilerPathNonexistentRootIsForbidden(t *testing.T) {
	if _, ok := safeFilerPath("x", filepath.Join(t.TempDir(), "does-not-exist")); ok {
		t.Error("safeFilerPath with nonexistent root = ok, want forbidden")
	}
}
