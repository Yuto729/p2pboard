// Package store はノードのローカル状態を保持する。
//
//   - 受信済み msg_id の集合 (gossip の重複排除に使う)
//   - 表示・レポート用の投稿履歴
//
// 少人数の room を対象とするためインメモリ実装とし、履歴は起動中のみ保持する。
package store

import (
	"sync"

	"github.com/Yuto729/p2pboard/internal/protocol"
)

// Store はスレッドセーフなインメモリ履歴 + seen 集合。
type Store struct {
	mu      sync.Mutex
	seen    map[string]struct{}
	history []protocol.Post
}

// New は空の Store を返す。
func New() *Store {
	return &Store{seen: make(map[string]struct{})}
}

// MarkSeen は msg_id を初めて見たとき true を返し、その msg_id を記録する。
// 既に見ていれば false を返す。gossip 転送の可否判定に使う。
func (s *Store) MarkSeen(msgID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[msgID]; ok {
		return false
	}
	s.seen[msgID] = struct{}{}
	return true
}

// AddPost は投稿を履歴に追加する。
func (s *Store) AddPost(p protocol.Post) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, p)
}

// History は現在の履歴のコピーを返す。
func (s *Store) History() []protocol.Post {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]protocol.Post, len(s.history))
	copy(out, s.history)
	return out
}

// SeenCount は記録済み msg_id 数を返す (評価ログ用)。
func (s *Store) SeenCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}
