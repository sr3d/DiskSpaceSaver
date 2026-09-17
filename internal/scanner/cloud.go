package scanner

import (
	"os"
	"path/filepath"
	"strings"
)

// Cloud file providers (iCloud Drive, Google Drive, Dropbox, OneDrive) project
// their contents into the local filesystem: through the FileProvider framework
// on macOS, through cloud filter placeholders on Windows. Either way they are
// hard to tell from ordinary directories — on macOS they share the data
// volume's st_dev and have no mount entry or flag on the root.
//
// Walking into one is wrong twice over. The bytes are not on this disk, so
// counting them overstates what the user could reclaim; and stat-ing an
// unmaterialised item blocks on the network, which parks every worker and
// makes the whole scan look frozen. Scanning cloud storage properly means
// talking to each provider's API, which is a separate feature.
//
// They are still reported as entries, flagged, so the chart can say what they
// are instead of silently omitting them.
// homeCloudRoots resolves the current user's provider locations. Each Scanner
// resolves this once at construction rather than caching it in a package-level
// variable: a process-wide cache would make the answer depend on whichever
// caller happened to run first.
func homeCloudRoots() (parents, exact []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}
	return cloudRootsFor(home)
}

// matchesCloudRoot reports whether path is a cloud provider's root directory.
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
