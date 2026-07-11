package node

import (
	"context"
	"testing"
	"time"
)

// startNode は同一テスト用 discovery ポートで 1 ノードを起動する。
func startNode(t *testing.T, ctx context.Context, room, name string, discPort int) *Node {
	t.Helper()
	n, err := New(Config{
		RoomName:      room,
		Name:          name,
		DiscoveryPort: discPort,
		Logf:          t.Logf,
	})
	if err != nil {
		t.Fatalf("New(%s): %v", name, err)
	}
	go n.Run(ctx)
	return n
}

// waitEvent は指定 Kind のイベントが来るまで待つ。
func waitEvent(t *testing.T, n *Node, kind EventKind, timeout time.Duration) Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-n.Events():
			if ev.Kind == kind {
				return ev
			}
		case <-deadline:
			t.Fatalf("[%s] timeout waiting for %s", "", kind)
		}
	}
}

// TestThreeNodeGossip は 3 ノードが互いを発見して接続し、
// 1 ノードの投稿が他の 2 ノードへ伝播することを確認する。
func TestThreeNodeGossip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const discPort = 42626
	room := "gossip-test"

	a := startNode(t, ctx, room, "alice", discPort)
	b := startNode(t, ctx, room, "bob", discPort)
	c := startNode(t, ctx, room, "carol", discPort)
	defer a.Close()
	defer b.Close()
	defer c.Close()

	// 3 ノードが接続するまで待つ (各ノードが 2 peer と接続)。
	waitEvent(t, b, EventPeerJoined, 5*time.Second)
	waitEvent(t, c, EventPeerJoined, 5*time.Second)
	// 接続の落ち着きを少し待つ。
	time.Sleep(500 * time.Millisecond)

	// alice が投稿 → bob と carol が受信する。
	if _, err := a.Post("hello everyone", nil); err != nil {
		t.Fatal(err)
	}

	pb := waitEvent(t, b, EventPost, 5*time.Second)
	if pb.Post.Body != "hello everyone" || pb.Post.Name != "alice" {
		t.Fatalf("bob got wrong post: %+v", pb.Post)
	}
	pc := waitEvent(t, c, EventPost, 5*time.Second)
	if pc.Post.Body != "hello everyone" {
		t.Fatalf("carol got wrong post: %+v", pc.Post)
	}

	// alice 自身は同じ投稿を EventPost として二重受信しない
	// (自分の msg_id は seen 済みなので gossip が戻っても破棄)。
	// carol は POST を 1 回だけ履歴に持つ。
	if got := len(c.History()); got != 1 {
		t.Fatalf("carol history should have exactly 1 post, got %d", got)
	}
}
