// Package node は discovery / peer / store を結線し、1 つの P2PBoard ノードとして動かす。
//
// 役割:
//   - discovery の発見イベントを peer manager の接続判断へ渡す
//   - peer manager が受信した POST を重複排除し、履歴保存し、
//     未受信 peer へ gossip 転送し、上位 (CLI) へ通知する
//   - 自ノードからの投稿を採番・保存・配送する
package node

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/Yuto729/p2pboard/internal/discovery"
	"github.com/Yuto729/p2pboard/internal/peer"
	"github.com/Yuto729/p2pboard/internal/protocol"
	"github.com/Yuto729/p2pboard/internal/store"
)

// EventKind は UI へ渡すイベント種別。
type EventKind string

const (
	EventPost       EventKind = "post"        // 他ノードからの投稿
	EventSelf       EventKind = "self"        // 自ノードの投稿エコー
	EventPeerJoined EventKind = "peer_joined" // peer 接続
	EventPeerLeft   EventKind = "peer_left"   // peer 切断
	EventDuplicate  EventKind = "duplicate"   // 重複 POST を破棄した (評価ログ用)
)

// Event は node から CLI / bot へ渡す通知。
type Event struct {
	Kind   EventKind
	Post   protocol.Post
	PeerID string
	Name   string
}

// Config はノード起動パラメータ。
type Config struct {
	RoomName   string
	Passphrase string
	Name       string
	ListenPort int // 0 ならエフェメラルポート

	DiscoveryGroup string
	DiscoveryPort  int
	Logf           func(format string, args ...any)
}

// Node は 1 つの P2PBoard 参加者。
type Node struct {
	cfg    Config
	roomID string
	peerID string
	logf   func(format string, args ...any)

	disc   *discovery.Discovery
	mgr    *peer.Manager
	store  *store.Store
	events chan Event
}

// New はノードを初期化する。TCP listen を先に開いて実ポートを確定し、
// そのポートを discovery の ANNOUNCE で広告する。
func New(cfg Config) (*Node, error) {
	logf := cfg.Logf
	if logf == nil {
		logf = log.Printf
	}
	roomID := protocol.RoomID(cfg.RoomName, cfg.Passphrase)
	peerID, err := protocol.NewPeerID()
	if err != nil {
		return nil, err
	}

	mgr, err := peer.NewManager(peer.Config{
		RoomID:     roomID,
		PeerID:     peerID,
		Name:       cfg.Name,
		ListenPort: cfg.ListenPort,
		Logf:       logf,
	})
	if err != nil {
		return nil, err
	}

	disc, err := discovery.New(discovery.Config{
		RoomID:     roomID,
		PeerID:     peerID,
		Name:       cfg.Name,
		ListenPort: mgr.BoundPort(), // 確定した TCP ポートを広告
		Group:      cfg.DiscoveryGroup,
		Port:       cfg.DiscoveryPort,
		Logf:       logf,
	})
	if err != nil {
		return nil, err
	}

	return &Node{
		cfg:    cfg,
		roomID: roomID,
		peerID: peerID,
		logf:   logf,
		disc:   disc,
		mgr:    mgr,
		store:  store.New(),
		events: make(chan Event, 128),
	}, nil
}

// PeerID / RoomID / ListenPort はノードの識別情報を返す。
func (n *Node) PeerID() string  { return n.peerID }
func (n *Node) RoomID() string  { return n.roomID }
func (n *Node) ListenPort() int { return n.mgr.BoundPort() }

// Events は UI 通知チャネルを返す。
func (n *Node) Events() <-chan Event { return n.events }

// Run は全サブシステムを起動し、ctx がキャンセルされるまで結線ループを回す。
func (n *Node) Run(ctx context.Context) {
	go n.disc.Run(ctx)
	go n.mgr.Run(ctx)
	go n.discoveryLoop(ctx)
	go n.peerEventLoop(ctx)
	n.messageLoop(ctx) // 主ループ。ctx.Done でチャネルが枯れると抜ける想定
}

// Close は下位リソースを解放する。
func (n *Node) Close() {
	n.disc.Close()
}

// discoveryLoop は発見した peer を peer manager の接続判断へ渡す。
func (n *Node) discoveryLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-n.disc.Events():
			n.mgr.OnDiscovered(ctx, ev.Peer.PeerID, ev.Peer.Name, ev.Peer.Addr)
		}
	}
}

// peerEventLoop は接続状態変化を UI イベントへ変換する。
func (n *Node) peerEventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case pe := <-n.mgr.PeerEvents():
			kind := EventPeerJoined
			if !pe.Connected {
				kind = EventPeerLeft
			}
			n.emit(Event{Kind: kind, PeerID: pe.PeerID, Name: pe.Name})
		}
	}
}

// messageLoop は受信フレームを処理する。現状 POST のみ扱う。
func (n *Node) messageLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-n.mgr.Messages():
			if msg.Type == protocol.TypePost {
				n.handlePost(msg.From, msg.Raw)
			}
		}
	}
}

// handlePost は受信 POST を重複排除し、保存・転送・通知する。
func (n *Node) handlePost(from string, raw []byte) {
	var p protocol.Post
	if err := json.Unmarshal(raw, &p); err != nil {
		return
	}
	if p.RoomID != n.roomID || p.MsgID == "" {
		return
	}
	if !n.store.MarkSeen(p.MsgID) {
		// 既に見た POST。gossip の重複配送なので破棄する。
		n.emit(Event{Kind: EventDuplicate, Post: p})
		return
	}
	n.store.AddPost(p)
	n.emit(Event{Kind: EventPost, Post: p})
	// 受信元を除く全 peer へ転送 (full mesh でも将来の部分グラフでも成立)。
	n.mgr.Broadcast(p, from)
}

// Post は自ノードから新規投稿を送る。
func (n *Node) Post(body string, mentions []string) (protocol.Post, error) {
	msgID, err := protocol.NewMsgID()
	if err != nil {
		return protocol.Post{}, err
	}
	p := protocol.Post{
		Type:      protocol.TypePost,
		RoomID:    n.roomID,
		MsgID:     msgID,
		From:      n.peerID,
		Name:      n.cfg.Name,
		CreatedAt: time.Now().Unix(),
		Mentions:  mentions,
		Body:      body,
	}
	n.store.MarkSeen(msgID) // 自分の投稿が gossip で戻っても二重表示しない
	n.store.AddPost(p)
	n.emit(Event{Kind: EventSelf, Post: p})
	n.mgr.Broadcast(p, "")
	return p, nil
}

// History は保存済み投稿を返す。
func (n *Node) History() []protocol.Post { return n.store.History() }

func (n *Node) emit(ev Event) {
	select {
	case n.events <- ev:
	default:
		n.logf("node: event channel full, dropping %s", ev.Kind)
	}
}
