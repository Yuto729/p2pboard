// Package peer は TCP message plane を管理する。
//
// 責務 (DESIGN.md 6 章):
//   - TCP listen socket を開き inbound 接続を受理する
//   - discovery が見つけた peer へ peer_id の大小ルールに従い outbound 接続する
//   - HELLO を交換し room / peer を確認する
//   - 接続を張りっぱなしにし、JSONL フレームを送受信する
//   - PING / PONG で keepalive し、離脱を検知する
//
// 接続確立後のフレームは Messages() チャネルへ流し、gossip / 表示など
// アプリ層の処理は上位 (node パッケージ) が行う。
package peer

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Yuto729/p2pboard/internal/protocol"
)

const (
	pingInterval = 5 * time.Second
	readTimeout  = 15 * time.Second // この間フレームが来なければ切断とみなす
)

// Config は Manager の起動パラメータ。
type Config struct {
	RoomID     string
	PeerID     string
	Name       string
	ListenPort int // 0 ならエフェメラルポート。BoundPort() で確定値を取得
	Logf       func(format string, args ...any)
}

// Message は受信した 1 フレーム。From は送信元 peer_id。
type Message struct {
	From string
	Type string
	Raw  []byte
}

// PeerEvent は接続状態の変化 (connected / disconnected) を表す。
type PeerEvent struct {
	PeerID    string
	Name      string
	Connected bool
}

// conn は 1 本の確立済み TCP 接続。
type conn struct {
	peerID string
	name   string
	nc     net.Conn
	wmu    sync.Mutex // 書き込みの直列化
}

func (c *conn) writeJSON(v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.nc.Write(buf)
	return err
}

// Manager は全 peer 接続を保持する。
type Manager struct {
	cfg       Config
	logf      func(format string, args ...any)
	listenFD  int
	boundPort int

	mu    sync.Mutex
	conns map[string]*conn // peer_id -> conn

	messages chan Message
	peerEv   chan PeerEvent
}

// NewManager は TCP listen socket を開く。BoundPort() で実ポートを得る。
func NewManager(cfg Config) (*Manager, error) {
	logf := cfg.Logf
	if logf == nil {
		logf = log.Printf
	}
	fd, port, err := listenTCP(cfg.ListenPort)
	if err != nil {
		return nil, err
	}
	return &Manager{
		cfg:       cfg,
		logf:      logf,
		listenFD:  fd,
		boundPort: port,
		conns:     make(map[string]*conn),
		messages:  make(chan Message, 128),
		peerEv:    make(chan PeerEvent, 64),
	}, nil
}

// BoundPort は listen socket が実際に bind したポートを返す。
func (m *Manager) BoundPort() int { return m.boundPort }

// Messages は受信フレーム (PING/PONG/HELLO を除く) のチャネル。
func (m *Manager) Messages() <-chan Message { return m.messages }

// PeerEvents は接続状態変化のチャネル。
func (m *Manager) PeerEvents() <-chan PeerEvent { return m.peerEv }

// Run は accept ループを開始し、ctx がキャンセルされるまでブロックする。
func (m *Manager) Run(ctx context.Context) {
	go func() {
		<-ctx.Done()
		closeFD(m.listenFD)
	}()
	for {
		nc, err := acceptTCP(m.listenFD)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			m.logf("peer: accept: %v", err)
			return
		}
		go m.handshake(ctx, nc, "")
	}
}

// OnDiscovered は discovery が peer を発見したとき呼ぶ。
// peer_id の大小ルールで、自分が小さいときだけ outbound 接続する。
func (m *Manager) OnDiscovered(ctx context.Context, peerID, name string, addr *net.TCPAddr) {
	if peerID == m.cfg.PeerID {
		return
	}
	m.mu.Lock()
	_, exists := m.conns[peerID]
	m.mu.Unlock()
	if exists {
		return // 既に接続済み
	}
	if m.cfg.PeerID >= peerID {
		return // 相手からの inbound を待つ側
	}
	go func() {
		nc, err := dialTCP(addr)
		if err != nil {
			m.logf("peer: dial %s (%s): %v", name, addr, err)
			return
		}
		m.handshake(ctx, nc, peerID)
	}()
}

// handshake は接続直後に HELLO を交換し、成立したら接続を登録する。
// expectPeerID が非空 (outbound) の場合、相手 peer_id の一致を確認する。
func (m *Manager) handshake(ctx context.Context, nc net.Conn, expectPeerID string) {
	hello := protocol.Hello{
		Type:   protocol.TypeHello,
		Proto:  protocol.Proto,
		RoomID: m.cfg.RoomID,
		PeerID: m.cfg.PeerID,
		Name:   m.cfg.Name,
	}
	buf, _ := json.Marshal(hello)
	buf = append(buf, '\n')
	nc.SetWriteDeadline(time.Now().Add(readTimeout))
	if _, err := nc.Write(buf); err != nil {
		nc.Close()
		return
	}
	nc.SetWriteDeadline(time.Time{})

	r := bufio.NewReader(nc)
	nc.SetReadDeadline(time.Now().Add(readTimeout))
	line, err := r.ReadBytes('\n')
	if err != nil {
		nc.Close()
		return
	}
	nc.SetReadDeadline(time.Time{})

	var remote protocol.Hello
	if err := json.Unmarshal(line, &remote); err != nil ||
		remote.Type != protocol.TypeHello ||
		remote.Proto != protocol.Proto ||
		remote.RoomID != m.cfg.RoomID ||
		remote.PeerID == m.cfg.PeerID {
		m.logf("peer: bad HELLO, closing")
		nc.Close()
		return
	}
	if expectPeerID != "" && remote.PeerID != expectPeerID {
		m.logf("peer: peer_id mismatch (want %s got %s)", expectPeerID, remote.PeerID)
		nc.Close()
		return
	}

	c := &conn{peerID: remote.PeerID, name: remote.Name, nc: nc}

	m.mu.Lock()
	if _, dup := m.conns[remote.PeerID]; dup {
		// 二重接続。既存を優先し、新規は閉じる。
		m.mu.Unlock()
		nc.Close()
		return
	}
	m.conns[remote.PeerID] = c
	m.mu.Unlock()

	m.logf("peer: connected %s (%s)", remote.Name, remote.PeerID)
	m.emitPeer(PeerEvent{PeerID: remote.PeerID, Name: remote.Name, Connected: true})

	go m.pingLoop(ctx, c, r)
	m.readLoop(c, r)
}

// readLoop は接続から JSONL を読み続け、フレームを振り分ける。
func (m *Manager) readLoop(c *conn, r *bufio.Reader) {
	defer m.dropConn(c)
	for {
		c.nc.SetReadDeadline(time.Now().Add(readTimeout))
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var env protocol.Envelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue // 破損行は無視
		}
		switch env.Type {
		case protocol.TypePing:
			c.writeJSON(protocol.Pong{Type: protocol.TypePong, Timestamp: time.Now().Unix()})
		case protocol.TypePong:
			// liveness は read deadline のリセットで担保するので何もしない
		default:
			cp := make([]byte, len(line))
			copy(cp, line)
			select {
			case m.messages <- Message{From: c.peerID, Type: env.Type, Raw: cp}:
			default:
				m.logf("peer: message channel full, dropping %s from %s", env.Type, c.peerID)
			}
		}
	}
}

// pingLoop は定期的に PING を送り、相手の生存を確かめる。
func (m *Manager) pingLoop(ctx context.Context, c *conn, _ *bufio.Reader) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.writeJSON(protocol.Ping{Type: protocol.TypePing, Timestamp: time.Now().Unix()}); err != nil {
				c.nc.Close() // readLoop 側で dropConn される
				return
			}
		}
	}
}

// Broadcast は全接続へフレームを送る。except に含まれる peer_id へは送らない
// (gossip で受信元へ送り返さないため)。
func (m *Manager) Broadcast(v any, except string) {
	m.mu.Lock()
	targets := make([]*conn, 0, len(m.conns))
	for id, c := range m.conns {
		if id == except {
			continue
		}
		targets = append(targets, c)
	}
	m.mu.Unlock()
	for _, c := range targets {
		if err := c.writeJSON(v); err != nil {
			m.logf("peer: write to %s: %v", c.peerID, err)
			c.nc.Close()
		}
	}
}

// dropConn は接続を破棄し disconnected イベントを流す。
func (m *Manager) dropConn(c *conn) {
	c.nc.Close()
	m.mu.Lock()
	if cur, ok := m.conns[c.peerID]; ok && cur == c {
		delete(m.conns, c.peerID)
	}
	m.mu.Unlock()
	m.logf("peer: disconnected %s (%s)", c.name, c.peerID)
	m.emitPeer(PeerEvent{PeerID: c.peerID, Name: c.name, Connected: false})
}

func (m *Manager) emitPeer(ev PeerEvent) {
	select {
	case m.peerEv <- ev:
	default:
	}
}
