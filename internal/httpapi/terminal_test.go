package httpapi

import (
	"embed"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BambooTuna/tmuxui/internal/backend"
	"github.com/BambooTuna/tmuxui/internal/hub"
	"github.com/BambooTuna/tmuxui/internal/prefs"
)

func TestTerminalRoute(t *testing.T) {
	reg := backend.NewBackendRegistry("tmux")
	reg.Register("tmux", &fakeBackend{})
	h := hub.New()
	h.SetRegistry(reg)
	// Dev: true で os.DirFS("web") を使うので、リポジトリルート実行前提。
	handler := New(Config{
		Token:       "t",
		Dev:         true,
		WebFS:       embed.FS{},
		Hub:         h,
		Registry:    reg,
		Preferences: prefs.New(),
	})
	// リポジトリルート("../..")配下で go test が走るため、Dev=true の os.DirFS("web") は
	// httpapiパッケージのCWDから見て存在しない。ここは "/terminal" が middleware で
	// 認証対象になっている(=tokenなしで403、tokenありで到達する)ことだけ確認する。
	req := httptest.NewRequest("GET", "/terminal", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("without token: status = %d, want 403", w.Code)
	}
	req2 := httptest.NewRequest("GET", "/terminal?token=t", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	// token通過後は route が到達する。ファイルが見つからない場合は 404、見つかれば 200。
	// どちらでも「middlewareが/terminalを認証対象として扱っている」ことは示せる。
	if w2.Code == http.StatusForbidden {
		t.Fatalf("with token: still forbidden (%d)", w2.Code)
	}
}
