// Package platform is the only part of the app that knows which operating
// system it is running on. Everything above it — the bound API, the scan
// store, the frontend — works in terms of the types and the capability set
// declared here.
//
// The capability set matters as much as the functions. Several things the
// macOS build offers have no Windows counterpart (Quick Look, purgeable space,
// Time Machine snapshots, Full Disk Access) and one or two go the other way.
// Rather than stubbing those out and leaving the interface showing controls
// that do nothing, each platform declares what it can actually do and the
// interface is built from that.
package platform

// Volume is a mounted filesystem the user can scan.
type Volume struct {
	Name string `json:"name"`
	Path string `json:"path"`
	UUID string `json:"uuid"`
	// Total, Free and Used are as the operating system reports them.
	Total int64 `json:"total"`
	Free  int64 `json:"free"`
	Used  int64 `json:"used"`
	// AvailRaw is the space free without evicting anything; Purgeable is the
	// gap between that and Free. macOS only — zero elsewhere.
	AvailRaw  int64  `json:"availRaw"`
	Purgeable int64  `json:"purgeable"`
	Removable bool   `json:"removable"`
	Internal  bool   `json:"internal"`
	Network   bool   `json:"network"`
	ReadOnly  bool   `json:"readOnly"`
	IsRoot    bool   `json:"isRoot"`
	FS        string `json:"fs"`
	Icon      string `json:"icon,omitempty"`
}

// AccessStatus reports whether the app can read everything it would need to.
// On macOS that is the Full Disk Access grant; on Windows it is whether the
// process is elevated.
type AccessStatus struct {
	Granted bool   `json:"granted"`
	Probe   string `json:"probe"`
	Detail  string `json:"detail,omitempty"`
}

// Snapshot is one filesystem snapshot occupying space on a volume.
type Snapshot struct {
	Name string `json:"name"`
	Date string `json:"date"`
}

// Caps describes what this platform can do, so the interface can label itself
// correctly and hide what does not exist rather than offering dead controls.
type Caps struct {
	OS string `json:"os"`

	// Preview is a system preview panel: Quick Look on macOS. Windows has no
	// equivalent, so Space opens the file instead.
	Preview bool `json:"preview"`
	// Properties is the system's file information window.
	Properties bool `json:"properties"`
	// Snapshots is a listable, deletable set of filesystem snapshots.
	Snapshots bool `json:"snapshots"`
	// Purgeable is space the system will reclaim on its own.
	Purgeable bool `json:"purgeable"`
	// Helper is an elevated component that can read what the user cannot.
	Helper bool `json:"helper"`
	// HelperInstall is whether that helper is installed once rather than
	// started on demand. macOS registers a launchd daemon; Windows elevates a
	// process per scan.
	HelperInstall bool `json:"helperInstall"`
	// FullDiskAccess is a system privacy grant the user must give by hand.
	FullDiskAccess bool `json:"fullDiskAccess"`

	// Wording, so the interface says what the platform's users expect.
	RevealLabel     string `json:"revealLabel"`
	TrashLabel      string `json:"trashLabel"`
	TrashVerb       string `json:"trashVerb"`
	PropertiesLabel string `json:"propertiesLabel"`
	// ModKey prefixes every shortcut hint.
	ModKey string `json:"modKey"`
}

// AppDirName is the folder that holds preferences and the scan cache.
const AppDirName = "DiskSpaceSaver"

// PurgeableBytes is the gap between what the system says is available and what
// is free without evicting anything.
func PurgeableBytes(v Volume) int64 {
	if v.Free > v.AvailRaw {
		return v.Free - v.AvailRaw
	}
	return 0
}
