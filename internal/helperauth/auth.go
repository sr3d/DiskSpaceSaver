//go:build darwin

// Package helperauth verifies who is talking to the privileged helper and
// collects the sockets launchd has already bound for it.
package helperauth

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework Security
#include <stdlib.h>
#include "auth.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
	"unsafe"
)

// Requirement builds the designated requirement a caller must satisfy.
//
// Both halves matter. The identifier alone would be satisfied by any build
// signed by anyone; the Team ID alone would be satisfied by any other app from
// the same developer. Together they name exactly one program.
func Requirement(bundleID, teamID string) string {
	return fmt.Sprintf(
		`identifier "%s" and anchor apple generic and certificate leaf[subject.OU] = "%s"`,
		bundleID, teamID)
}

func goStringFree(c *C.char) string {
	if c == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(c))
	return C.GoString(c)
}

// VerifyPID checks that a process satisfies the requirement.
func VerifyPID(pid int, requirement string) error {
	cr := C.CString(requirement)
	defer C.free(unsafe.Pointer(cr))
	if msg := goStringFree(C.dss_verify_peer(C.int(pid), cr)); msg != "" {
		return errors.New(msg)
	}
	return nil
}

// PeerPID returns the pid at the other end of a Unix socket.
func PeerPID(conn *net.UnixConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid int
	var serr error
	err = raw.Control(func(fd uintptr) {
		pid, serr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	})
	if err != nil {
		return 0, err
	}
	if serr != nil {
		return 0, serr
	}
	return pid, nil
}

// PeerUID returns the uid at the other end of a Unix socket, which is how the
// helper knows whose Trash a deleted file belongs in.
func PeerUID(conn *net.UnixConn) (uint32, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var uid uint32
	var serr error
	err = raw.Control(func(fd uintptr) {
		var u, g int
		u, g, serr = getpeereid(int(fd))
		_ = g
		uid = uint32(u)
	})
	if err != nil {
		return 0, err
	}
	return uid, serr
}

// LaunchdListeners returns listeners for the sockets launchd has already bound
// on the helper's behalf. Using them rather than binding a path ourselves is
// what lets launchd start the helper on demand and own the socket's lifetime.
func LaunchdListeners(name string) ([]net.Listener, error) {
	cn := C.CString(name)
	defer C.free(unsafe.Pointer(cn))

	var fds *C.int
	var count C.size_t
	if msg := goStringFree(C.dss_launchd_sockets(cn, &fds, &count)); msg != "" {
		return nil, errors.New(msg)
	}
	if fds == nil || count == 0 {
		return nil, errors.New("launchd handed over no sockets")
	}
	defer C.free(unsafe.Pointer(fds))

	list := unsafe.Slice((*C.int)(fds), int(count))
	out := make([]net.Listener, 0, len(list))
	for i, fd := range list {
		f := os.NewFile(uintptr(fd), fmt.Sprintf("launchd-socket-%d", i))
		ln, err := net.FileListener(f)
		f.Close() // FileListener dups; the original is no longer needed
		if err != nil {
			return nil, fmt.Errorf("adopting launchd socket %d: %w", i, err)
		}
		out = append(out, ln)
	}
	return out, nil
}
