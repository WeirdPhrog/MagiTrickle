package subscriptions

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func interfaceControl(name string) (func(string, string, syscall.RawConn) error, error) {
	return func(network, address string, conn syscall.RawConn) error {
		var bindErr error
		if err := conn.Control(func(fd uintptr) {
			bindErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, name)
		}); err != nil {
			return err
		}
		return bindErr
	}, nil
}
