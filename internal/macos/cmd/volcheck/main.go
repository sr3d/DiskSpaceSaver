package main

import (
	"fmt"

	"github.com/sr3d/diskspacesaver/internal/macos"
)

func main() {
	vs, err := macos.ListVolumes(false)
	if err != nil {
		panic(err)
	}
	for _, v := range vs {
		fmt.Printf("%-24s %-28s total=%-10s free=%-10s purge=%-9s root=%v net=%v rm=%v fs=%s uuid=%s\n",
			v.Name, v.Path,
			macos.FormatBytes(v.Total, false), macos.FormatBytes(v.Free, false),
			macos.FormatBytes(v.Purgeable, false), v.IsRoot, v.Network, v.Removable, v.FS, v.UUID)
	}
	fmt.Println("FDA:", macos.CheckFullDiskAccess())
	fmt.Println("bundled:", macos.IsBundled(), "team:", macos.TeamID())
	sn, _ := macos.ListLocalSnapshots("/")
	fmt.Println("snapshots:", sn)
}
