//go:build !darwin

package scanner

import "errors"

var errNoBulk = errors.New("getattrlistbulk: unsupported on this platform")

func errBulkUnsupported(err error) bool { return errors.Is(err, errNoBulk) }

func (s *Scanner) listDirBulk(job dirJob, frame *Frame, children *[]dirJob) (int, error) {
	return 0, errNoBulk
}
