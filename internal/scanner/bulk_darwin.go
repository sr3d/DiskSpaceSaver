//go:build darwin

package scanner

import "errors"

// errNoBulk signals that the caller should fall back to readdir+fstatat.
var errNoBulk = errors.New("getattrlistbulk: unsupported")

func errBulkUnsupported(err error) bool { return errors.Is(err, errNoBulk) }

// listDirBulk is the getattrlistbulk(2) fast path, filled in at M7. Until then
// it reports itself unsupported and the portable path handles every directory.
func (s *Scanner) listDirBulk(job dirJob, frame *Frame, children *[]dirJob) (int, error) {
	return 0, errNoBulk
}
