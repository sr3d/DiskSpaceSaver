package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// SF_DATALESS marks an item whose contents live somewhere else and would have
// to be fetched. golang.org/x/sys/unix does not export it.
const sfDataless = 0x40000000

// Cloud file providers (iCloud Drive, Google Drive, Dropbox, OneDrive) project
// their contents into the data volume through the FileProvider framework. They
// are indistinguishable from ordinary APFS directories: same st_dev, same
// statfs, no mount entry, no flag on the root.
//
// Walking into one is wrong twice over. The bytes are not on this disk, so
// counting them overstates what the user could reclaim; and stat-ing an
// unmaterialised item blocks on the network, which parks every worker and
// makes the whole scan look frozen. Scanning cloud storage properly means
// talking to each provider's API, which is a separate feature.
//
// They are still reported as entries, flagged, so the chart can say what they
// are instead of silently omitting them.
var (
	cloudOnce    sync.Once
	cloudParents []string
	cloudExact   []string
)

// cloudRootsFor lists, for one home directory, the directories whose children
// are provider roots and the provider roots themselves.
func cloudRootsFor(home string) (parents, exact []string) {
	if home == "" {
		return nil, nil
	}
	return []string{
			// Every macOS 12+ file provider domain appears here as a child.
			filepath.Join(home, "Library", "CloudStorage"),
		}, []string{
			filepath.Join(home, "Library", "Mobile Documents"), // iCloud Drive
		}
}

func cloudPaths() ([]string, []string) {
	cloudOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		cloudParents, cloudExact = cloudRootsFor(home)
	})
	return cloudParents, cloudExact
}

// isCloudRoot reports whether path is a cloud provider's root directory.
func isCloudRoot(path string) bool {
	parents, exact := cloudPaths()
	return matchesCloudRoot(path, parents, exact)
}

func matchesCloudRoot(path string, parents, exact []string) bool {
	for _, e := range exact {
		if path == e {
			return true
		}
	}
	dir := filepath.Dir(path)
	for _, p := range parents {
		if dir == p {
			return true
		}
	}
	return false
}

// isDataless reports whether an item's contents are not present locally.
func isDataless(st *unix.Stat_t) bool {
	return st.Flags&sfDataless != 0
}

// DescribeCloudSkip explains a skipped provider in the UI.
func DescribeCloudSkip(name string) string {
	switch {
	case strings.HasPrefix(name, "GoogleDrive-"):
		return "Google Drive — stored online, not on this disk"
	case strings.HasPrefix(name, "Dropbox"):
		return "Dropbox — stored online, not on this disk"
	case strings.HasPrefix(name, "OneDrive"):
		return "OneDrive — stored online, not on this disk"
	case strings.HasPrefix(name, "Box-"):
		return "Box — stored online, not on this disk"
	case strings.HasPrefix(name, "iCloudDrive") || name == "Mobile Documents":
		return "iCloud Drive — stored online, not on this disk"
	default:
		return "Cloud storage — stored online, not on this disk"
	}
}
