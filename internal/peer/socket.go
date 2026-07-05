package peer

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

// closeFD は listen fd を閉じる。accept ループの中断に使う。
func closeFD(fd int) { _ = unix.Close(fd) }

// listenTCP は TCP listen socket を低レイヤ API で作る。
// socket() → setsockopt(SO_REUSEADDR) → bind() → listen() の流れは
// C の socket API と 1:1 に対応する。
//
// port が 0 の場合はカーネルが空きポートを割り当てるので、
// getsockname 相当で実際のポートを取得して返す。
func listenTCP(port int) (fd int, boundPort int, err error) {
	fd, err = unix.Socket(unix.AF_INET, unix.SOCK_STREAM, unix.IPPROTO_TCP)
	if err != nil {
		return -1, 0, fmt.Errorf("socket: %w", err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
		unix.Close(fd)
		return -1, 0, fmt.Errorf("setsockopt SO_REUSEADDR: %w", err)
	}
	if err := unix.Bind(fd, &unix.SockaddrInet4{Port: port}); err != nil {
		unix.Close(fd)
		return -1, 0, fmt.Errorf("bind :%d: %w", port, err)
	}
	if err := unix.Listen(fd, 128); err != nil {
		unix.Close(fd)
		return -1, 0, fmt.Errorf("listen: %w", err)
	}

	sa, err := unix.Getsockname(fd)
	if err != nil {
		unix.Close(fd)
		return -1, 0, fmt.Errorf("getsockname: %w", err)
	}
	in4, ok := sa.(*unix.SockaddrInet4)
	if !ok {
		unix.Close(fd)
		return -1, 0, fmt.Errorf("unexpected sockaddr type %T", sa)
	}
	return fd, in4.Port, nil
}

// acceptTCP は listen fd から 1 接続を受理し、net.Conn にラップして返す。
// unix.Accept はブロッキングなので専用 goroutine から呼ぶ。
func acceptTCP(listenFD int) (net.Conn, error) {
	nfd, _, err := unix.Accept(listenFD)
	if err != nil {
		return nil, err
	}
	return fdToConn(nfd, "p2pboard-inbound")
}

// dialTCP は指定アドレスへ TCP connect し、net.Conn を返す。
// socket() → connect() の低レイヤ手順を踏む。
func dialTCP(addr *net.TCPAddr) (net.Conn, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, unix.IPPROTO_TCP)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}
	var ip4 [4]byte
	copy(ip4[:], addr.IP.To4())
	if err := unix.Connect(fd, &unix.SockaddrInet4{Addr: ip4, Port: addr.Port}); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("connect %s: %w", addr, err)
	}
	return fdToConn(fd, "p2pboard-outbound")
}

// fdToConn は生の socket fd を Go の net.Conn に変換する。
// net.FileConn は fd を dup して runtime のネットワークポーラに載せるため、
// 以降は非ブロッキング I/O・deadline・bufio がそのまま使える。
func fdToConn(fd int, name string) (net.Conn, error) {
	f := os.NewFile(uintptr(fd), name)
	if f == nil {
		unix.Close(fd)
		return nil, fmt.Errorf("os.NewFile failed for fd %d", fd)
	}
	conn, err := net.FileConn(f)
	// FileConn は fd を dup するので、元 fd はここで閉じてよい。
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("file conn: %w", err)
	}
	return conn, nil
}
