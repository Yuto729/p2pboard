package discovery

import (
	"net"
	"time"
)

// Peer は discovery で発見した他ノードの情報。
type Peer struct {
	PeerID   string
	Name     string
	Addr     *net.TCPAddr // 送信元 IP + ANNOUNCE の listen_port
	LastSeen time.Time
}

// Event は discovery レイヤが上位 (peer manager) へ通知するイベント。
type Event struct {
	Peer Peer
}
