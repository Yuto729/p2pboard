package protocol

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// RoomID は room 名 (と任意の passphrase) から決定的に room_id を導出する。
//
//	room_id = "r_" + hex(SHA256(name + ":" + passphrase))[0:16]
//
// 同じ name/passphrase を持つ参加者だけが同じ room_id を共有する。
func RoomID(name, passphrase string) string {
	sum := sha256.Sum256([]byte(name + ":" + passphrase))
	return "r_" + hex.EncodeToString(sum[:])[:16]
}

// NewPeerID はプロセス起動ごとに一意な peer_id を生成する。
// peer_id は接続方向の決定 (大小比較) にも使うため、辞書順で比較できればよい。
func NewPeerID() (string, error) {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate peer id: %w", err)
	}
	return "p_" + hex.EncodeToString(b[:]), nil
}

// NewMsgID は POST 用の一意な msg_id を生成する。gossip の重複排除キーになる。
func NewMsgID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate msg id: %w", err)
	}
	return "m_" + hex.EncodeToString(b[:]), nil
}
