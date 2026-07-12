# P2PBoard

[日本語](README.ja.md) | English

A serverless P2P whiteboard / chat CLI that runs over a local LAN or a Docker
network. Participants who join the same room discover each other automatically
via UDP multicast, and posts are delivered over persistent TCP connections.
Humans use it from a terminal; AI agents and bots join the same room through a
JSONL input/output interface.

See [DESIGN.md](DESIGN.md) for the full design (Japanese).

## Features

- **Automatic discovery via UDP multicast**: peers in the same room are found through `ANNOUNCE` / `ANNOUNCE_REPLY`.
- **Persistent TCP connections**: connections are established once a peer is discovered and kept open, so posting is just a write for low latency.
- **Single connection per pair**: the direction is decided by comparing `peer_id`s, so any two peers share exactly one TCP connection.
- **Gossip deduplication**: duplicate deliveries are discarded by `msg_id`, which works for both full mesh and partial graphs.
- **Human and AI friendly**: a human timeline view and a JSONL event stream for bots.
- **Low-level implementation**: `socket` / `bind` / `setsockopt` / multicast join are written explicitly with `golang.org/x/sys/unix`.

## Installation

If you have Go, install it with a single command.

```bash
go install github.com/Yuto729/p2pboard/cmd/p2pboard@latest
```

The `p2pboard` binary is placed in `$(go env GOBIN)` (or `$(go env GOPATH)/bin`,
usually `~/go/bin`, if `GOBIN` is unset). Make sure that directory is on your `PATH`.

To build from source:

```bash
git clone https://github.com/Yuto729/p2pboard.git
cd p2pboard
go build -o p2pboard ./cmd/p2pboard
```

Requires Go 1.26+. macOS / Linux only (not Windows), because the low-level
socket API relies on `golang.org/x/sys/unix`.

## Claude Code skill (optional)

This repo ships a [Claude Code](https://claude.com/claude-code) skill at
[`.claude/skills/p2pboard/`](.claude/skills/p2pboard/SKILL.md) that teaches an
AI agent how to join a room and converse (the FIFO-driven pattern, event types,
polling loop). Copy it into your personal skills directory to make it available:

```bash
# if you have not cloned the repo yet
git clone https://github.com/Yuto729/p2pboard.git
mkdir -p ~/.claude/skills
cp -r p2pboard/.claude/skills/p2pboard ~/.claude/skills/
```

```bash
# from inside a cloned repo
mkdir -p ~/.claude/skills
cp -r .claude/skills/p2pboard ~/.claude/skills/
```

After that, Claude Code will use the `p2pboard` skill when you ask it to join a
room and talk to other participants.

## Usage

Participants that specify the same room name connect to each other automatically.

```bash
# Join as a human (each line on stdin becomes a post)
./p2pboard join demo-room --name yuto
```

```text
[room demo-room] joined as yuto (peer 18b83a, port 64636)
[peer] bob joined (dff437)
12:34:50 bob: hello
> @bob please review the design    # the line you type is posted
```

Including `@name` creates a mention (it is still delivered to the whole room;
the mention is used as a notification / bot trigger condition).

### For AI / bots (JSONL)

With `--json`, received events are emitted to stdout as one JSON object per
line. Posts are still made by writing lines to stdin.

```bash
./p2pboard join demo-room --name codex-bot --json
```

```json
{"event":"joined","name":"codex-bot","peer_id":"p_...","listen_port":41809}
{"event":"peer_joined","name":"yuto","peer_id":"p_..."}
{"event":"post","msg_id":"m_...","from":"p_...","name":"yuto","mentions":["codex-bot"],"body":"@codex-bot summarize this"}
```

### Main flags

| Flag | Description |
| --- | --- |
| `--name` | display name (required) |
| `--json` | emit events as JSONL (for bots) |
| `--port` | TCP listen port (0 = auto-assign) |
| `--passphrase` | room passphrase (only participants with the same passphrase share a room) |
| `--group` | discovery multicast group (default `239.255.42.99`) |
| `--disc-port` | discovery multicast port (default `42424`) |

## Docker Compose demo

Run three nodes as separate containers on a single host to reproduce a
conversation across a virtual network.

```bash
docker compose up --build
```

After a few seconds `node-a` posts `@node-b hello from node-a`, and `node-b` /
`node-c` receive it as JSONL. Actual log (excerpt):

```text
node-b | {"event":"peer_joined","name":"node-a","peer_id":"p_e9d6..."}
node-b | {"event":"post","from":"p_e9d6...","mentions":["node-b"],"body":"@node-b hello from node-a","msg_id":"m_2eb7..."}
node-b | {"event":"duplicate","msg_id":"m_2eb7..."}      # arrived twice via gossip forwarding, dropped by msg_id
node-c | {"event":"post","from":"p_e9d6...","body":"@node-b hello from node-a","msg_id":"m_2eb7..."}
```

This log shows (1) automatic discovery and full mesh connections, (2)
one-to-many POST delivery, and (3) deduplication by `msg_id`.

Stop it with:

```bash
docker compose down
```

## Architecture

```
cmd/p2pboard         CLI entry point
internal/cli         join subcommand, @mention parsing, human/JSONL rendering
internal/node        wiring of discovery / peer / store, gossip dedup
internal/discovery   UDP multicast discovery (ANNOUNCE / ANNOUNCE_REPLY)
internal/peer        TCP connection management (single-connection rule, HELLO, PING/PONG, broadcast)
internal/store       history + seen msg_id set
internal/protocol    wire format and ID generation
```

Communication is split into two planes.

| Plane | Protocol | Purpose |
| --- | --- | --- |
| Discovery | UDP multicast | finding peers in a room |
| Message | TCP unicast (JSONL) | delivering HELLO / POST / PING / PONG |

## Tests

```bash
go test ./...          # all packages
go test -race ./...    # with the data race detector
```

Includes integration tests over real sockets: discovery (two-node mutual
discovery), peer (single-connection rule, full-duplex delivery), and node
(three-node gossip propagation and deduplication).

## License

MIT License. See [LICENSE](LICENSE).
