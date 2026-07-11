// Package cli は p2pboard のサブコマンドを実装する。
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Yuto729/p2pboard/internal/node"
)

// RunJoin は `p2pboard join <room> --name <name>` を実行する。
//
// 標準入力の各行を投稿として送り、他ノードの投稿・参加・離脱を表示する。
// --json 指定時はイベントを JSONL で stdout に出し、bot から扱えるようにする。
func RunJoin(args []string) error {
	fs := flag.NewFlagSet("join", flag.ContinueOnError)
	name := fs.String("name", "", "display name (required)")
	jsonOut := fs.Bool("json", false, "emit events as JSON lines (for bots)")
	port := fs.Int("port", 0, "TCP listen port (0 = ephemeral)")
	passphrase := fs.String("passphrase", "", "optional room passphrase")
	group := fs.String("group", "", "discovery multicast group")
	discPort := fs.Int("disc-port", 0, "discovery multicast port")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: p2pboard join <room> --name <name> [--json] [--port N]\n")
		fs.PrintDefaults()
	}
	// room 名は最初の位置引数。flag は先頭の非フラグで解析を止めるため、
	// room を先に取り出してから残りをフラグとして解析する。
	var room string
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		room = args[0]
		rest = args[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if room == "" {
		fs.Usage()
		return fmt.Errorf("room name required")
	}
	if *name == "" {
		return fmt.Errorf("--name is required")
	}

	// bot が投稿する際の名前解決に使うため、自分の表示名を渡す。
	logf := func(format string, a ...any) { fmt.Fprintf(os.Stderr, "[log] "+format+"\n", a...) }

	n, err := node.New(node.Config{
		RoomName:       room,
		Passphrase:     *passphrase,
		Name:           *name,
		ListenPort:     *port,
		DiscoveryGroup: *group,
		DiscoveryPort:  *discPort,
		Logf:           logf,
	})
	if err != nil {
		return err
	}
	defer n.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go n.Run(ctx)

	r := newRenderer(*jsonOut, *name, n.PeerID())
	r.banner(room, n)

	go r.consume(ctx, n)

	// 標準入力の各行を投稿にする。
	stdin := bufio.NewReader(os.Stdin)
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		for {
			line, err := stdin.ReadString('\n')
			line = strings.TrimRight(line, "\r\n")
			if line != "" {
				mentions := parseMentions(line)
				if _, perr := n.Post(line, mentions); perr != nil {
					logf("post failed: %v", perr)
				}
			}
			if err != nil {
				if err != io.EOF {
					logf("stdin: %v", err)
				}
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-inputDone:
	}
	return nil
}

// renderer はイベントを人間向け / JSONL 向けに描画する。
type renderer struct {
	jsonOut bool
	name    string
	peerID  string
	w       *bufio.Writer
}

func newRenderer(jsonOut bool, name, peerID string) *renderer {
	return &renderer{jsonOut: jsonOut, name: name, peerID: peerID, w: bufio.NewWriter(os.Stdout)}
}

func (r *renderer) banner(room string, n *node.Node) {
	if r.jsonOut {
		r.emitJSON(map[string]any{
			"event": "joined", "room": room, "name": r.name,
			"peer_id": n.PeerID(), "listen_port": n.ListenPort(),
		})
		return
	}
	fmt.Fprintf(r.w, "[room %s] joined as %s (peer %s, port %d)\n",
		room, r.name, shortID(n.PeerID()), n.ListenPort())
	r.w.Flush()
}

func (r *renderer) consume(ctx context.Context, n *node.Node) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-n.Events():
			if r.jsonOut {
				r.renderJSON(ev)
			} else {
				r.renderHuman(ev)
			}
		}
	}
}

func (r *renderer) renderHuman(ev node.Event) {
	switch ev.Kind {
	case node.EventPeerJoined:
		fmt.Fprintf(r.w, "[peer] %s joined (%s)\n", ev.Name, shortID(ev.PeerID))
	case node.EventPeerLeft:
		fmt.Fprintf(r.w, "[peer] %s left (%s)\n", ev.Name, shortID(ev.PeerID))
	case node.EventPost:
		fmt.Fprintf(r.w, "%s %s: %s\n", clock(ev.Post.CreatedAt), ev.Post.Name, ev.Post.Body)
	case node.EventSelf:
		// 自分の投稿は入力エコーで十分なので表示しない。
	case node.EventDuplicate:
		// 重複破棄は通常表示しない (評価時はログで観察)。
	}
	r.w.Flush()
}

func (r *renderer) renderJSON(ev node.Event) {
	switch ev.Kind {
	case node.EventPeerJoined, node.EventPeerLeft:
		r.emitJSON(map[string]any{
			"event": string(ev.Kind), "peer_id": ev.PeerID, "name": ev.Name,
		})
	case node.EventPost, node.EventSelf:
		r.emitJSON(map[string]any{
			"event": string(ev.Kind), "msg_id": ev.Post.MsgID, "from": ev.Post.From,
			"name": ev.Post.Name, "mentions": ev.Post.Mentions, "body": ev.Post.Body,
			"created_at": ev.Post.CreatedAt,
		})
	case node.EventDuplicate:
		r.emitJSON(map[string]any{"event": "duplicate", "msg_id": ev.Post.MsgID})
	}
}

func (r *renderer) emitJSON(v any) {
	buf, _ := json.Marshal(v)
	r.w.Write(buf)
	r.w.WriteByte('\n')
	r.w.Flush()
}

// clock は unix 秒を HH:MM:SS へ整形する。
func clock(sec int64) string { return time.Unix(sec, 0).Format("15:04:05") }

// shortID は peer_id を短縮表示する。
func shortID(id string) string {
	s := strings.TrimPrefix(id, "p_")
	if len(s) > 6 {
		s = s[:6]
	}
	return s
}
