package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/sr3d/diskspacesaver/internal/macos"
)

// SnapshotInfo is one APFS local Time Machine snapshot.
type SnapshotInfo struct {
	Name string `json:"name"`
	Date string `json:"date"`
}

// HiddenSpaceReport explains the gap between what a volume says is used and
// what the scan could see. That gap is the single most common reason people
// distrust a disk analyser, so it is accounted for explicitly rather than
// hand-waved.
type HiddenSpaceReport struct {
	Volume string `json:"volume"`
	Mount  string `json:"mount"`
	Total  int64  `json:"total"`
	Used   int64  `json:"used"`
	Free   int64  `json:"free"`

	// Visible is the scanned total, when a finished scan of this volume is
	// open. Zero means nothing has been scanned yet.
	Visible  int64 `json:"visible"`
	HasScan  bool  `json:"hasScan"`
	Scanning bool  `json:"scanning"`

	Purgeable     int64          `json:"purgeable"`
	Snapshots     []SnapshotInfo `json:"snapshots"`
	SnapshotBytes int64          `json:"snapshotBytes"`

	// Protected is space in trees the scan was refused. Inaccessible counts
	// the entries behind it.
	Protected    int64 `json:"protected"`
	Inaccessible int64 `json:"inaccessible"`

	// Unaccounted is whatever is left: APFS clones sharing blocks, metadata
	// overhead, and anything else the walk cannot attribute. It can be
	// negative when clone sharing means the volume stores less than the sum
	// of the file sizes, which is itself worth showing.
	Unaccounted int64 `json:"unaccounted"`

	Notes []string `json:"notes"`
}

// HiddenSpace builds the report for a mounted volume.
func (a *App) HiddenSpace(mount string) (*HiddenSpaceReport, error) {
	vols, err := macos.ListVolumes(false)
	if err != nil {
		return nil, err
	}
	var vol *macos.Volume
	for i := range vols {
		if vols[i].Path == mount || (vols[i].IsRoot && (mount == "/" || mount == "/System/Volumes/Data")) {
			vol = &vols[i]
			break
		}
	}
	if vol == nil {
		return nil, fmt.Errorf("%s is not a mounted volume", mount)
	}

	rep := &HiddenSpaceReport{
		Volume: vol.Name, Mount: vol.Path,
		Total: vol.Total, Used: vol.Used, Free: vol.Free,
		Purgeable: vol.Purgeable,
		Snapshots: []SnapshotInfo{},
		Notes:     []string{},
	}

	// Snapshots.
	snaps, _ := macos.ListLocalSnapshots(vol.Path)
	for _, s := range snaps {
		rep.Snapshots = append(rep.Snapshots, SnapshotInfo{Name: s.Name, Date: s.Date})
	}
	rep.SnapshotBytes = snapshotBytes(vol.Path)

	// The scanned total, if a finished scan of this volume is open.
	for _, sum := range a.scans.List() {
		if sum.VolumeUUID != vol.UUID || sum.VolumeUUID == "" {
			continue
		}
		s, ok := a.scans.Get(sum.ID)
		if !ok {
			continue
		}
		if root, ok := s.Tree.Snapshot(s.Tree.Root); ok {
			rep.Visible = root.Alloc
			rep.HasScan = true
			rep.Scanning = s.Scanning()
			rep.Inaccessible = s.Tree.Errors()
		}
		break
	}

	if rep.HasScan {
		rep.Unaccounted = rep.Used - rep.Visible - rep.Purgeable - rep.SnapshotBytes
		if rep.Inaccessible > 0 {
			rep.Notes = append(rep.Notes, fmt.Sprintf(
				"%d items could not be read. Grant Full Disk Access or scan as administrator to include them.",
				rep.Inaccessible))
		}
		if rep.Unaccounted < 0 {
			rep.Notes = append(rep.Notes,
				"The volume stores less than the files add up to. APFS clones and snapshots share blocks between files, so copies on this disk can be almost free.")
		}
	} else if !rep.Scanning {
		rep.Notes = append(rep.Notes, "Scan this volume to see how its used space breaks down.")
	}
	if rep.Purgeable > 0 {
		rep.Notes = append(rep.Notes,
			"Purgeable space holds caches and iCloud files macOS will evict automatically when the disk fills up.")
	}
	if len(rep.Snapshots) > 0 {
		rep.Notes = append(rep.Notes, fmt.Sprintf(
			"%d Time Machine snapshots are stored on this volume. macOS removes them on its own, but you can delete them now.",
			len(rep.Snapshots)))
	}
	return rep, nil
}

// snapshotBytes asks diskutil how much a volume's snapshots occupy. The figure
// is not exposed anywhere else, and snapshots sharing blocks with live files
// makes it impossible to derive from the walk.
func snapshotBytes(mount string) int64 {
	// macOS exposes no supported figure for what local snapshots occupy —
	// they share blocks with live files, so there is no single honest number —
	// and diskutil only reports one on some volume types. Zero here means "not
	// known", and the panel lists and deletes snapshots without claiming a
	// size it cannot substantiate.
	out, err := exec.Command("/usr/sbin/diskutil", "info", mount).Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "Snapshot") || !strings.Contains(line, "Bytes") {
			continue
		}
		fields := strings.Fields(line)
		for i, f := range fields {
			if n, err := strconv.ParseInt(f, 10, 64); err == nil && i > 0 {
				return n
			}
		}
	}
	return 0
}

// DeleteLocalSnapshots removes APFS local snapshots by name.
func (a *App) DeleteLocalSnapshots(mount string, names []string) (Result, error) {
	res := newResult()
	if len(names) == 0 {
		return res, errors.New("no snapshots selected")
	}
	for _, n := range names {
		err := macos.DeleteLocalSnapshot(mount, n)
		if err != nil && a.help.Available() {
			// Some snapshots need root even though most do not.
			err = a.help.DeleteSnapshots([]string{n})
		}
		if err != nil {
			res.Failed[n] = err.Error()
			continue
		}
		res.Succeeded = append(res.Succeeded, n)
	}
	a.emit("volumes:changed", nil)
	if len(res.Succeeded) == 0 {
		return res, firstFailure(res, "no snapshots could be deleted")
	}
	return res, nil
}

// ListLocalSnapshots returns the APFS local snapshots on a volume.
func (a *App) ListLocalSnapshots(mount string) ([]SnapshotInfo, error) {
	snaps, err := macos.ListLocalSnapshots(mount)
	if err != nil {
		return nil, err
	}
	out := make([]SnapshotInfo, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, SnapshotInfo{Name: s.Name, Date: s.Date})
	}
	return out, nil
}

// OpenStorageSettings opens the system's own storage management pane.
func (a *App) OpenStorageSettings() error {
	return macos.OpenURL("x-apple.systempreferences:com.apple.settings.Storage")
}
