package peer

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/Yuto729/p2pboard/internal/protocol"
)

func newTestManager(t *testing.T, peerID, name string) *Manager {
	t.Helper()
	m, err := NewManager(Config{
		RoomID:     protocol.RoomID("test", ""),
		PeerID:     peerID,
		Name:       name,
		ListenPort: 0,
		Logf:       t.Logf,
	})
	if err != nil {
		t.Fatalf("NewManager(%s): %v", name, err)
	}
	return m
}

// TestConnectAndBroadcast は peer_id の大小で 1 本の接続が張られ、
// その上で POST フレームが双方向に届くことを確認する。
func TestConnectAndBroadcast(t *testing.T) {
	// p_a < p_b なので a が dial する側。
	a := newTestManager(t, "p_aaaa", "alice")
	b := newTestManager(t, "p_bbbb", "bob")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)
	go b.Run(ctx)

	lo := net.IPv4(127, 0, 0, 1)
	// 双方に相手を教える。ルール上 a だけが実際に dial する。
	a.OnDiscovered(ctx, "p_bbbb", "bob", &net.TCPAddr{IP: lo, Port: b.BoundPort()})
	b.OnDiscovered(ctx, "p_aaaa", "alice", &net.TCPAddr{IP: lo, Port: a.BoundPort()})

	waitConnected(t, a.PeerEvents(), "p_bbbb")
	waitConnected(t, b.PeerEvents(), "p_aaaa")

	// a から POST を broadcast → b が受信する。
	post := protocol.Post{Type: protocol.TypePost, MsgID: "m1", From: "p_aaaa", Body: "hello"}
	a.Broadcast(post, "")

	got := waitMessage(t, b.Messages(), protocol.TypePost)
	var p protocol.Post
	if err := json.Unmarshal(got.Raw, &p); err != nil {
		t.Fatal(err)
	}
	if p.Body != "hello" || got.From != "p_aaaa" {
		t.Fatalf("unexpected post: from=%s body=%q", got.From, p.Body)
	}

	// 逆方向 (full-duplex): b から POST → a が受信する。
	b.Broadcast(protocol.Post{Type: protocol.TypePost, MsgID: "m2", From: "p_bbbb", Body: "hi"}, "")
	if got := waitMessage(t, a.Messages(), protocol.TypePost); got.From != "p_bbbb" {
		t.Fatalf("reverse post from wrong peer: %s", got.From)
	}
}

// TestSingleConnection は両側が dial を試みても接続が 1 本に統一されることを確認する。
func TestSingleConnection(t *testing.T) {
	a := newTestManager(t, "p_aaaa", "alice")
	b := newTestManager(t, "p_bbbb", "bob")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)
	go b.Run(ctx)

	lo := net.IPv4(127, 0, 0, 1)
	// 両者が相手を発見しても、大小ルールで a のみ dial。
	a.OnDiscovered(ctx, "p_bbbb", "bob", &net.TCPAddr{IP: lo, Port: b.BoundPort()})
	b.OnDiscovered(ctx, "p_aaaa", "alice", &net.TCPAddr{IP: lo, Port: a.BoundPort()})
	waitConnected(t, a.PeerEvents(), "p_bbbb")

	time.Sleep(300 * time.Millisecond)
	a.mu.Lock()
	n := len(a.conns)
	a.mu.Unlock()
	if n != 1 {
		t.Fatalf("expected exactly 1 connection, got %d", n)
	}
}

func waitConnected(t *testing.T, ev <-chan PeerEvent, want string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-ev:
			if e.PeerID == want && e.Connected {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for connection to %s", want)
		}
	}
}

func waitMessage(t *testing.T, ch <-chan Message, typ string) Message {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case m := <-ch:
			if m.Type == typ {
				return m
			}
		case <-deadline:
			t.Fatalf("timeout waiting for %s message", typ)
		}
	}
}
