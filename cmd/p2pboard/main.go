// Command p2pboard は P2P ホワイトボード / チャット CLI のエントリポイント。
//
// 使い方 (実装が進むにつれ拡張する):
//
//	p2pboard join <room> --name <name>   room に参加して対話する
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "join":
		fmt.Fprintln(os.Stderr, "join: not implemented yet")
		os.Exit(1)
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
  p2pboard join <room> --name <name> [--json]
`)
}
