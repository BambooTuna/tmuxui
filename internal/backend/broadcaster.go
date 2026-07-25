package backend

import "sync"

// Broadcaster はターゲット1つ分の購読者集合を管理する共通ヘルパー。
// TmuxControlBackend(control-modeの%output)とHerdrBackend(ポーリング差分)の両方が、
// 「chanが満杯なら溜めずにonce.Doでcloseしてcancel扱いにする」という同一パターンを持つため、
// ここに集約する。二重close防止(*sync.Once)のセマンティクスは元の実装と同一に保つ:
// Add時に渡されたOnceを、Publishでのoverflow時とAdd呼び出し元のcancel処理の両方で
// 使い回すことで、どちらが先にcloseしても安全にする。
type Broadcaster struct {
	mu   sync.Mutex
	subs map[chan []byte]*sync.Once
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subs: map[chan []byte]*sync.Once{}}
}

// Add は新しい購読チャネルとその close-guard(Once)を登録する。
func (b *Broadcaster) Add(ch chan []byte, once *sync.Once) {
	b.mu.Lock()
	b.subs[ch] = once
	b.mu.Unlock()
}

// Remove はchを購読集合から取り除く(closeはしない。呼び出し側がOnceで行う)。
func (b *Broadcaster) Remove(ch chan []byte) {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
}

// Len は現在の購読者数を返す。0ならこのBroadcasterはもう不要と判断できる。
func (b *Broadcaster) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// Publish はdataを全購読者に配信する。満杯のチャネルは溜めずにOnceでcloseして
// 購読集合から取り除く(ブロッキングも無制限バッファリングもしない)。
func (b *Broadcaster) Publish(data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var overflowed []chan []byte
	for ch, once := range b.subs {
		select {
		case ch <- data:
		default:
			overflowed = append(overflowed, ch)
			once.Do(func() { close(ch) })
		}
	}
	for _, ch := range overflowed {
		delete(b.subs, ch)
	}
}
