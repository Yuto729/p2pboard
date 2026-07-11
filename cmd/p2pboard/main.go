// Command p2pboard は P2P ホワイトボード / チャット CLI のエントリポイント。
//
// 使い方:
//
//	p2pboard join <room> --name <name> [--json]
//
// 同じ room に参加した peer を UDP multicast で自動発見し、TCP で投稿を配送する。
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/Yuto729/p2pboard/internal/cli"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "join":
		if err := cli.RunJoin(os.Args[2:]); err != nil {
			if !errors.Is(err, flag.ErrHelp) {
				fmt.Fprintln(os.Stderr, "error:", err)
			}
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `p2pboard - P2P whiteboard / chat CLI

usage:
  p2pboard join <room> --name <name> [--json] [--port N]

flags:
  --name        display name (required)
  --json        emit events as JSON lines (for bots)
  --port        TCP listen port (0 = ephemeral)
  --passphrase  optional room passphrase
  --group       discovery multicast group
  --disc-port   discovery multicast port
`)
}
