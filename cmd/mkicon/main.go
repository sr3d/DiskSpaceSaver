// Command mkicon assembles a multi-resolution Windows .ico from PNGs that are
// already the right size.
//
// Wails can generate one from build/appicon.png, but only when the file does
// not already exist, and it downscales a single 1024px source — which turns
// the three concentric rings of this artwork to mush at 16 and 32 pixels. The
// PNGs this reads are rendered from the vector at their native size by
// scripts/make-icons.sh, the same as the macOS iconset.
//
// The entries are stored as PNG rather than as a DIB. Windows has accepted
// PNG-compressed icon entries since Vista, the app requires Windows 10, and it
// keeps the file a quarter of the size with the alpha channel intact.
//
// Pure Go and no dependencies, so it runs on whichever machine is building.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"sort"
)

// iconDirEntry is one row of the ICO directory.
type iconDirEntry struct {
	Width    byte // 0 means 256
	Height   byte
	Colors   byte // 0 for a true-colour image
	Reserved byte
	Planes   uint16
	BitCount uint16
	Size     uint32 // bytes of image data
	Offset   uint32 // from the start of the file
}

type source struct {
	dim  int
	data []byte
}

func main() {
	out := flag.String("o", "build/windows/icon.ico", "destination .ico")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: mkicon [-o icon.ico] <png>...")
		os.Exit(2)
	}

	var sources []source
	for _, p := range flag.Args() {
		raw, err := os.ReadFile(p)
		if err != nil {
			fatal(err)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			fatal(fmt.Errorf("%s: %w", p, err))
		}
		if cfg.Width != cfg.Height {
			fatal(fmt.Errorf("%s is %dx%d; icon entries must be square", p, cfg.Width, cfg.Height))
		}
		if cfg.Width > 256 {
			// The directory records a size in one byte, so 256 is the ceiling.
			continue
		}
		sources = append(sources, source{dim: cfg.Width, data: raw})
	}
	if len(sources) == 0 {
		fatal(fmt.Errorf("no usable images: every entry must be square and at most 256px"))
	}
	// Smallest first, which is the order Explorer expects to read them in.
	sort.Slice(sources, func(i, j int) bool { return sources[i].dim < sources[j].dim })

	var buf bytes.Buffer
	// ICONDIR: reserved, type 1 (icon), count.
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(len(sources)))

	const dirEntrySize = 16
	offset := uint32(6 + dirEntrySize*len(sources))
	for _, s := range sources {
		dim := byte(s.dim)
		if s.dim == 256 {
			dim = 0 // 256 is encoded as zero
		}
		binary.Write(&buf, binary.LittleEndian, iconDirEntry{
			Width: dim, Height: dim,
			Planes: 1, BitCount: 32,
			Size: uint32(len(s.data)), Offset: offset,
		})
		offset += uint32(len(s.data))
	}
	for _, s := range sources {
		buf.Write(s.data)
	}

	if err := os.WriteFile(*out, buf.Bytes(), 0o644); err != nil {
		fatal(err)
	}
	dims := make([]int, len(sources))
	for i, s := range sources {
		dims[i] = s.dim
	}
	fmt.Printf("wrote %s (%d bytes) at %v\n", *out, buf.Len(), dims)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "mkicon:", err)
	os.Exit(1)
}
