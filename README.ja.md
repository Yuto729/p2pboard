# P2PBoard

日本語 | [English](README.md)

同一 LAN / Docker ネットワーク上で動作する、中央サーバ不要の P2P ホワイトボード / チャット CLI。
同じ room に参加した peer を UDP multicast で自動発見し、TCP の張りっぱなし接続で投稿を配送する。
人間はターミナルから、AI エージェントや bot は JSONL 入出力を通じて、同じ room に参加できる。

設計の詳細は [DESIGN.md](DESIGN.md) を参照。

## 特徴

- **UDP multicast による自動発見**: 同じ room の peer を `ANNOUNCE` / `ANNOUNCE_REPLY` で発見する
- **TCP 張りっぱなし接続**: peer 発見後に接続を確立し、以降は書き込みのみで低遅延配送
- **接続の 1 本化**: `peer_id` の大小で接続方向を決め、任意の 2 peer 間の接続を 1 本に統一
- **gossip 重複排除**: `msg_id` で重複配送を破棄し、full mesh でも部分グラフでも成立
- **人間 / AI 両対応**: 人間向けタイムライン表示と、bot 向け JSONL イベントストリーム
- **低レイヤ実装**: `golang.org/x/sys/unix` で socket / bind / setsockopt / multicast join を明示

## インストール

Go がある環境なら 1 コマンドで導入できる。

```bash
go install github.com/Yuto729/p2pboard/cmd/p2pboard@latest
```

`p2pboard` バイナリが `$(go env GOBIN)`（未設定なら `$(go env GOPATH)/bin`、通常 `~/go/bin`）に入る。
このディレクトリを `PATH` に通しておくこと。

ソースからビルドする場合:

```bash
git clone https://github.com/Yuto729/p2pboard.git
cd p2pboard
go build -o p2pboard ./cmd/p2pboard
```

Go 1.26 以降。socket の低レイヤ API に `golang.org/x/sys/unix` を用いるため macOS / Linux のみ対応（Windows 非対応）。

## 使い方

同じ room 名を指定した参加者どうしが自動的につながる。

```bash
# 人間として参加 (標準入力の各行が投稿になる)
./p2pboard join demo-room --name yuto
```

```text
[room demo-room] joined as yuto (peer 18b83a, port 64636)
[peer] bob joined (dff437)
12:34:50 bob: こんにちは
> @bob 設計を見てください        # 入力した行が投稿される
```

`@name` を含めるとメンションになる (配送は room 全体、通知条件として使う)。

### AI / bot 向け (JSONL)

`--json` を付けると、受信イベントを 1 行 1 JSON で標準出力に流す。投稿は標準入力の各行で行う。

```bash
./p2pboard join demo-room --name codex-bot --json
```

```json
{"event":"joined","name":"codex-bot","peer_id":"p_...","listen_port":41809}
{"event":"peer_joined","name":"yuto","peer_id":"p_..."}
{"event":"post","msg_id":"m_...","from":"p_...","name":"yuto","mentions":["codex-bot"],"body":"@codex-bot 要約して"}
```

### 主なフラグ

| フラグ | 説明 |
| --- | --- |
| `--name` | 表示名 (必須) |
| `--json` | イベントを JSONL で出力する (bot 向け) |
| `--port` | TCP listen ポート (0 = 自動割当) |
| `--passphrase` | room の合言葉 (同じ合言葉の参加者だけが同じ room になる) |
| `--group` | discovery の multicast group (既定 `239.255.42.99`) |
| `--disc-port` | discovery の multicast port (既定 `42424`) |

## Docker Compose デモ

1 台のホスト上で 3 ノードを別コンテナとして起動し、仮想ネットワーク越しの会話を再現する。

```bash
docker compose up --build
```

`node-a` が数秒後に `@node-b hello from node-a` を投稿し、`node-b` / `node-c` が JSONL で受信する。
実際のログ (抜粋):

```text
node-b | {"event":"peer_joined","name":"node-a","peer_id":"p_e9d6..."}
node-b | {"event":"post","from":"p_e9d6...","mentions":["node-b"],"body":"@node-b hello from node-a","msg_id":"m_2eb7..."}
node-b | {"event":"duplicate","msg_id":"m_2eb7..."}      # gossip 転送で二重着したが msg_id で破棄
node-c | {"event":"post","from":"p_e9d6...","body":"@node-b hello from node-a","msg_id":"m_2eb7..."}
```

このログから、(1) 自動発見と full mesh 接続、(2) 1 対多の POST 配送、(3) `msg_id` による重複排除、が確認できる。

停止:

```bash
docker compose down
```

## アーキテクチャ

```
cmd/p2pboard         CLI エントリポイント
internal/cli         join サブコマンド、@mention パース、人間/JSONL 描画
internal/node        discovery / peer / store の結線、gossip 重複排除
internal/discovery   UDP multicast 発見 (ANNOUNCE / ANNOUNCE_REPLY)
internal/peer        TCP 接続管理 (接続 1 本化、HELLO、PING/PONG、broadcast)
internal/store       履歴 + seen msg_id 集合
internal/protocol    ワイヤフォーマットと ID 生成
```

通信は 2 層に分かれる。

| Plane | プロトコル | 用途 |
| --- | --- | --- |
| Discovery | UDP multicast | room 内 peer の発見 |
| Message | TCP unicast (JSONL) | HELLO / POST / PING / PONG の配送 |

## テスト

```bash
go test ./...          # 全パッケージ
go test -race ./...    # データ競合検出付き
```

discovery (2 ノード相互発見)、peer (接続 1 本化・全二重配送)、node (3 ノード gossip 伝播と重複排除) を
実 socket で検証する統合テストを含む。

## ライセンス

MIT License. 詳細は [LICENSE](LICENSE) を参照。
