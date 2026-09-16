//go:build darwin

package helperauth

import (
	"golang.org/x/sys/unix"
)

// getpeereid reads the credentials of the process on the other end of a Unix
// socket. Darwin exposes them through LOCAL_PEERCRED as an xucred struct.
func getpeereid(fd int) (uid, gid int, err error) {
	x, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return 0, 0, err
	}
	g := 0
	if x.Ngroups > 0 {
		g = int(x.Groups[0])
	}
	return int(x.Uid), g, nil
}
