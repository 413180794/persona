//go:build linux

package netaddr

import (
	"errors"
	"os"
	"syscall"
)

// The nonblocking file uses Go's poller, so Close interrupts an idle receive.
type routeSocket struct {
	file *os.File
	raw  syscall.RawConn
}

func openRouteSocket(groups uint32) (*routeSocket, error) {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC|syscall.SOCK_NONBLOCK, syscall.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	if err := syscall.Bind(fd, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK, Groups: groups}); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "netaddr-netlink")
	raw, err := file.SyscallConn()
	if err != nil {
		file.Close()
		return nil, err
	}
	return &routeSocket{file, raw}, nil
}

func (s *routeSocket) send(data []byte) error {
	var sendErr error
	err := s.raw.Write(func(fd uintptr) bool {
		for {
			sendErr = syscall.Sendto(int(fd), data, 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK})
			if sendErr != syscall.EINTR {
				break
			}
		}
		return sendErr != syscall.EAGAIN
	})
	if err != nil {
		return err
	}
	return sendErr
}

func (s *routeSocket) receive(buf []byte) ([]byte, error) {
	var n, flags int
	var sender syscall.Sockaddr
	var recvErr error
	err := s.raw.Read(func(fd uintptr) bool {
		for {
			n, _, flags, sender, recvErr = syscall.Recvmsg(int(fd), buf, nil, 0)
			if recvErr != syscall.EINTR {
				break
			}
		}
		return recvErr != syscall.EAGAIN
	})
	if err != nil {
		return nil, err
	}
	if recvErr != nil {
		return nil, recvErr
	}
	if flags&syscall.MSG_TRUNC != 0 {
		return nil, errDumpInterrupted
	}
	source, ok := sender.(*syscall.SockaddrNetlink)
	if !ok || source.Pid != 0 {
		return nil, errors.New("netaddr: non-kernel netlink sender")
	}
	return buf[:n], nil
}
