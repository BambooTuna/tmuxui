package backend

import (
	"log"
	"runtime/debug"
)

// RecoverAndLog はdeferで呼び出し、panicがあればスタックトレース付きでログに残して
// 揉み消す。1つのバックグラウンドgoroutine(control-modeの読み取りループ、herdrの
// ポーラー、WebSocketの読み書きポンプ等)のpanicがプロセス全体を落とすのを防ぐために使う。
func RecoverAndLog(where string) {
	if r := recover(); r != nil {
		log.Printf("panic recovered in %s: %v\n%s", where, r, debug.Stack())
	}
}
