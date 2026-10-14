//go:build !linux

package subscriptions

import (
	"fmt"
	"syscall"
)

func interfaceControl(name string) (func(string, string, syscall.RawConn) error, error) {
	return nil, fmt.Errorf("binding subscription downloads to an interface requires Linux")
}
