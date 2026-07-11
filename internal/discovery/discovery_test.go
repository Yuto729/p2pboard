package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/Yuto729/p2pboard/internal/protocol"
)

// TestMutualDiscovery は同一ホスト上の 2 ノードが multicast で
// 互いを発見できることを確認する。
func TestMutualDiscovery(t *testing.T) {
	room := protocol.RoomID("test-room", "")
	// 他テストとの干渉を避けるためテスト専用ポートを使う。
	const port = 42525

	mk := func(peerID, name string, listen int) *Discovery {
		d, err := New(Config{
			RoomID:           room,
			PeerID:           peerID,
			Name:             name,
			ListenPort:       listen,
			Port:             port,
			AnnounceInterval: 200 * time.Millisecond,
			Logf:             func(string, ...any) {},
		})
		if err != nil {
			t.Fatalf("New(%s): %v", name, err)
		}
		return d
	}

	a := mk("p_aaaa", "alice", 9001)
	b := mk("p_bbbb", "bob", 9002)
	defer a.Close()
	defer b.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)
	go b.Run(ctx)

	if peer := waitPeer(t, a.Events(), "p_bbbb"); peer.Addr.Port != 9002 {
		t.Fatalf("alice saw bob with wrong port: %d", peer.Addr.Port)
	}
	if peer := waitPeer(t, b.Events(), "p_aaaa"); peer.Addr.Port != 9001 {
		t.Fatalf("bob saw alice with wrong port: %d", peer.Addr.Port)
	}
}

func waitPeer(t *testing.T, ev <-chan Event, want string) Peer {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-ev:
			if e.Peer.PeerID == want {
				return e.Peer
			}
		case <-deadline:
			t.Fatalf("timeout waiting to discover %s", want)
		}
	}
}
