// Package discovery は UDP multicast による room peer 発見を実装する。
//
// 設計方針 (DESIGN.md 5 章) に従い、socket の挙動を明示するため
// golang.org/x/sys/unix の低レイヤ socket API を用いる。
//
//   - 各ノードは multicast group に join し、定期的に ANNOUNCE を送る
//   - ANNOUNCE を受け取った既存ノードは送信元へ unicast で ANNOUNCE_REPLY を返す
//   - ANNOUNCE / ANNOUNCE_REPLY のどちらを受けても peer table を更新し、
//     上位レイヤへ Event を通知する
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/Yuto729/p2pboard/internal/protocol"
	"golang.org/x/sys/unix"
)

// DefaultGroup / DefaultPort は discovery plane の既定 multicast エンドポイント。
const (
	DefaultGroup = "239.255.42.99"
	DefaultPort  = 42424
)

// Config は Discovery の起動パラメータ。
type Config struct {
	RoomID     string
	PeerID     string
	Name       string
	ListenPort int // TCP message plane の listen port (ANNOUNCE で広告する)

	Group string // multicast group (空なら DefaultGroup)
	Port  int    // multicast port  (0 なら DefaultPort)

	// ANNOUNCE 送信間隔。0 なら既定の burst→steady スケジュールを使う。
	AnnounceInterval time.Duration

	Logf func(format string, args ...any)
}

// Discovery は UDP multicast socket を保持し、送受信ループを回す。
type Discovery struct {
	cfg    Config
	fd     int
	group  [4]byte
	port   int
	events chan Event
	logf   func(format string, args ...any)
}

// New は multicast socket を生成・bind し group に join する。
func New(cfg Config) (*Discovery, error) {
	group := cfg.Group
	if group == "" {
		group = DefaultGroup
	}
	port := cfg.Port
	if port == 0 {
		port = DefaultPort
	}
	logf := cfg.Logf
	if logf == nil {
		logf = log.Printf
	}

	gip := net.ParseIP(group).To4()
	if gip == nil {
		return nil, fmt.Errorf("invalid multicast group %q", group)
	}
	var g4 [4]byte
	copy(g4[:], gip)

	fd, err := openMulticastSocket(g4, port)
	if err != nil {
		return nil, err
	}

	return &Discovery{
		cfg:    cfg,
		fd:     fd,
		group:  g4,
		port:   port,
		events: make(chan Event, 64),
		logf:   logf,
	}, nil
}

// openMulticastSocket は UDP socket を作り、reuse 設定・bind・group join を行う。
func openMulticastSocket(group [4]byte, port int) (int, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, unix.IPPROTO_UDP)
	if err != nil {
		return -1, fmt.Errorf("socket: %w", err)
	}

	// 同一ホストで複数ノードを起動できるよう address/port を共有する。
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
		unix.Close(fd)
		return -1, fmt.Errorf("setsockopt SO_REUSEADDR: %w", err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1); err != nil {
		unix.Close(fd)
		return -1, fmt.Errorf("setsockopt SO_REUSEPORT: %w", err)
	}

	// multicast port に bind する。INADDR_ANY で待ち受ける。
	if err := unix.Bind(fd, &unix.SockaddrInet4{Port: port}); err != nil {
		unix.Close(fd)
		return -1, fmt.Errorf("bind :%d: %w", port, err)
	}

	// multicast group に join する (IP_ADD_MEMBERSHIP)。
	mreq := &unix.IPMreq{Multiaddr: group}
	if err := unix.SetsockoptIPMreq(fd, unix.IPPROTO_IP, unix.IP_ADD_MEMBERSHIP, mreq); err != nil {
		unix.Close(fd)
		return -1, fmt.Errorf("join multicast group: %w", err)
	}

	// 同一ホスト上の他プロセスにも自分の ANNOUNCE が見えるよう loop を有効化する。
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_MULTICAST_LOOP, 1); err != nil {
		unix.Close(fd)
		return -1, fmt.Errorf("setsockopt IP_MULTICAST_LOOP: %w", err)
	}

	return fd, nil
}

// Events は発見イベントを受け取るチャネルを返す。
func (d *Discovery) Events() <-chan Event { return d.events }

// Run は送受信ループを開始し、ctx がキャンセルされるまでブロックする。
func (d *Discovery) Run(ctx context.Context) error {
	go d.recvLoop(ctx)
	d.sendLoop(ctx)
	return nil
}

// Close は socket を閉じる。
func (d *Discovery) Close() error {
	return unix.Close(d.fd)
}

// sendLoop は起動直後は短間隔、安定後は長間隔で ANNOUNCE を送る。
func (d *Discovery) sendLoop(ctx context.Context) {
	// burst: 起動直後の素早い相互発見用。
	burst := []time.Duration{0, 500 * time.Millisecond, 500 * time.Millisecond, 1 * time.Second}
	for _, wait := range burst {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		d.sendAnnounce(protocol.TypeAnnounce, nil)
	}

	// steady: 定常送信。
	interval := d.cfg.AnnounceInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.sendAnnounce(protocol.TypeAnnounce, nil)
		}
	}
}

// sendAnnounce は ANNOUNCE または ANNOUNCE_REPLY を送る。
// to が nil なら multicast group へ、そうでなければ unicast で送る。
func (d *Discovery) sendAnnounce(msgType string, to *unix.SockaddrInet4) {
	msg := protocol.Announce{
		Type:         msgType,
		Proto:        protocol.Proto,
		RoomID:       d.cfg.RoomID,
		PeerID:       d.cfg.PeerID,
		Name:         d.cfg.Name,
		ListenPort:   d.cfg.ListenPort,
		Capabilities: []string{protocol.CapPost, protocol.CapGossip, protocol.CapJSONL},
		Timestamp:    time.Now().Unix(),
	}
	buf, err := json.Marshal(msg)
	if err != nil {
		d.logf("discovery: marshal %s: %v", msgType, err)
		return
	}

	dst := to
	if dst == nil {
		dst = &unix.SockaddrInet4{Addr: d.group, Port: d.port}
	}
	if err := unix.Sendto(d.fd, buf, 0, dst); err != nil {
		d.logf("discovery: sendto %s: %v", msgType, err)
	}
}

// recvLoop は UDP パケットを受信し、peer table 更新イベントを流す。
func (d *Discovery) recvLoop(ctx context.Context) {
	buf := make([]byte, 65535)
	for {
		if ctx.Err() != nil {
			return
		}
		n, from, err := unix.Recvfrom(d.fd, buf, 0)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// socket close 等での中断はループ終了。
			d.logf("discovery: recvfrom: %v", err)
			return
		}
		d.handlePacket(buf[:n], from)
	}
}

// handlePacket は 1 パケットを解釈し、自 room の他 peer なら Event を送る。
func (d *Discovery) handlePacket(data []byte, from unix.Sockaddr) {
	var msg protocol.Announce
	if err := json.Unmarshal(data, &msg); err != nil {
		return // 破損パケットは無視
	}
	if msg.Proto != protocol.Proto || msg.RoomID != d.cfg.RoomID {
		return // 別プロトコル / 別 room
	}
	if msg.PeerID == d.cfg.PeerID {
		return // 自分自身
	}
	if msg.Type != protocol.TypeAnnounce && msg.Type != protocol.TypeAnnounceReply {
		return
	}

	src, ok := from.(*unix.SockaddrInet4)
	if !ok {
		return
	}

	peer := Peer{
		PeerID: msg.PeerID,
		Name:   msg.Name,
		Addr: &net.TCPAddr{
			IP:   net.IP(src.Addr[:]),
			Port: msg.ListenPort,
		},
		LastSeen: time.Now(),
	}
	select {
	case d.events <- Event{Peer: peer}:
	default:
		d.logf("discovery: event channel full, dropping peer %s", peer.PeerID)
	}

	// 新規 ANNOUNCE には unicast で ANNOUNCE_REPLY を返し、相手の発見を早める。
	if msg.Type == protocol.TypeAnnounce {
		reply := &unix.SockaddrInet4{Addr: src.Addr, Port: src.Port}
		d.sendAnnounce(protocol.TypeAnnounceReply, reply)
	}
}
