// Package config resolves tmuxui's runtime configuration from CLI flag values and
// environment variables (TMUXUI_TOKEN, TMUXUI_AUTOUPDATE), including auto-generating an
// auth token when none is supplied.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

// Config はmain.goがflagから読み取った値と環境変数(TMUXUI_TOKEN)・自動生成トークンを
// 集約した実行時設定。
type Config struct {
	Host  string
	Port  int
	Token string
	Dev   bool
	// HerdrMode はhttp --herdrフラグの値。"off"/"auto"、またはherdrソケットへの明示パス。
	HerdrMode string
}

// New はCLIフラグの値からConfigを組み立てる。tokenが空ならTMUXUI_TOKEN環境変数、
// それも空ならランダムな16バイトのhexトークンを自動生成する(挙動はmain.go旧実装と同一)。
func New(host string, port int, token string, dev bool, herdrMode string) (Config, error) {
	if token == "" {
		token = os.Getenv("TMUXUI_TOKEN")
	}
	if token == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return Config{}, err
		}
		token = hex.EncodeToString(b)
	}
	return Config{Host: host, Port: port, Token: token, Dev: dev, HerdrMode: herdrMode}, nil
}

// Addr はhttp.Serverに渡すbindアドレス("host:port")を返す。
func (c Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// AutoUpdateDisabled はTMUXUI_AUTOUPDATE=0のキルスイッチが設定されているかを返す。
// 環境変数はプロセス寿命中に変わらない想定だが、呼び出しごとに読み直す(テストでの
// os.Setenvによる差し替えにも対応するため、Config構築時に固定値化はしない)。
func AutoUpdateDisabled() bool {
	return os.Getenv("TMUXUI_AUTOUPDATE") == "0"
}
