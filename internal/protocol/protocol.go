// Package protocol は P2PBoard のワイヤフォーマットを定義する。
//
// 通信は 2 種類に分かれる (DESIGN.md 参照)。
//
//   - Discovery plane: UDP multicast 上の ANNOUNCE / ANNOUNCE_REPLY
//   - Message plane:   TCP 上で 1 行 1 JSON (JSON Lines) の HELLO / POST / PING / PONG
//
// いずれのメッセージも共通の envelope として Type と Proto を持ち、
// 受信側はまず Type を見てから対応する構造体へデコードする。
package protocol

// Proto は本プロトコルのバージョン識別子。互換性のない変更で更新する。
const Proto = "p2pboard/0.1"

// メッセージ種別。JSON の "type" フィールドに入る。
const (
	TypeAnnounce      = "ANNOUNCE"
	TypeAnnounceReply = "ANNOUNCE_REPLY"
	TypeHello         = "HELLO"
	TypePost          = "POST"
	TypePing          = "PING"
	TypePong          = "PONG"
)

// Capability は peer が提供できる機能を表す。
const (
	CapPost   = "post"
	CapGossip = "gossip"
	CapJSONL  = "jsonl"
)

// Envelope は全メッセージ共通の先頭フィールド。
// Type だけを先に読み取りたい場合のデコード用にも使う。
type Envelope struct {
	Type  string `json:"type"`
	Proto string `json:"proto,omitempty"`
}

// Announce は UDP discovery plane で流れる ANNOUNCE / ANNOUNCE_REPLY。
// 両者はフィールド構成が同じで Type だけが異なる。
type Announce struct {
	Type         string   `json:"type"`
	Proto        string   `json:"proto"`
	RoomID       string   `json:"room_id"`
	PeerID       string   `json:"peer_id"`
	Name         string   `json:"name"`
	ListenPort   int      `json:"listen_port"`
	Capabilities []string `json:"capabilities"`
	Timestamp    int64    `json:"timestamp"`
}

// Hello は TCP 接続直後に交換し、room と peer を確認する。
type Hello struct {
	Type   string `json:"type"`
	Proto  string `json:"proto"`
	RoomID string `json:"room_id"`
	PeerID string `json:"peer_id"`
	Name   string `json:"name"`
}

// Post は room 全体へ gossip される投稿。
type Post struct {
	Type      string   `json:"type"`
	RoomID    string   `json:"room_id"`
	MsgID     string   `json:"msg_id"`
	From      string   `json:"from"`
	Name      string   `json:"name"`
	CreatedAt int64    `json:"created_at"`
	ReplyTo   string   `json:"reply_to,omitempty"`
	Mentions  []string `json:"mentions,omitempty"`
	Body      string   `json:"body"`
}

// Ping / Pong は張りっぱなし TCP 接続上の keepalive。
type Ping struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"`
}

type Pong struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"`
}
