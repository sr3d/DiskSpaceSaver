package tree

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// The cache format writes nodes in slab order as flat varint-packed records.
//
// Slab order is what makes this cheap: a node is always allocated after its
// parent, so replaying the records in order means every parent already exists
// and the tree rebuilds in one pass with no fixups and no random access.
const (
	codecMagic   = "DSSC"
	codecVersion = 1
)

var errBadCache = errors.New("scan cache is not in a format this version understands")

// Encode writes the tree to w. The caller supplies any compression.
func (t *Tree) Encode(w io.Writer) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	bw := bufio.NewWriterSize(w, 1<<20)
	if _, err := bw.WriteString(codecMagic); err != nil {
		return err
	}
	var hdr [binary.MaxVarintLen64]byte
	putU := func(v uint64) error {
		n := binary.PutUvarint(hdr[:], v)
		_, err := bw.Write(hdr[:n])
		return err
	}
	putI := func(v int64) error {
		n := binary.PutVarint(hdr[:], v)
		_, err := bw.Write(hdr[:n])
		return err
	}
	putS := func(s string) error {
		if err := putU(uint64(len(s))); err != nil {
			return err
		}
		_, err := bw.WriteString(s)
		return err
	}

	if err := putU(codecVersion); err != nil {
		return err
	}
	if err := putS(t.RootPath); err != nil {
		return err
	}
	if err := putU(uint64(t.n)); err != nil {
		return err
	}

	for id := uint32(1); id <= t.n; id++ {
		n := t.node(id)
		if err := putS(n.Name); err != nil {
			return err
		}
		for _, v := range []int64{n.Alloc, n.Logical, n.Self, n.SelfLog, n.Mtime} {
			if err := putI(v); err != nil {
				return err
			}
		}
		if err := putU(uint64(n.Parent)); err != nil {
			return err
		}
		if err := putI(int64(n.Files)); err != nil {
			return err
		}
		if err := putI(int64(n.Dirs)); err != nil {
			return err
		}
		if err := bw.WriteByte(n.Kind); err != nil {
			return err
		}
		if err := bw.WriteByte(n.Flags); err != nil {
			return err
		}
		if err := putS(n.Err); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// Decode rebuilds a tree written by Encode.
func Decode(r io.Reader) (*Tree, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	magic := make([]byte, len(codecMagic))
	if _, err := io.ReadFull(br, magic); err != nil {
		return nil, errBadCache
	}
	if string(magic) != codecMagic {
		return nil, errBadCache
	}
	getU := func() (uint64, error) { return binary.ReadUvarint(br) }
	getI := func() (int64, error) { return binary.ReadVarint(br) }
	getS := func() (string, error) {
		n, err := getU()
		if err != nil {
			return "", err
		}
		if n > 1<<16 {
			return "", errBadCache // no filesystem has names this long
		}
		b := make([]byte, n)
		if _, err := io.ReadFull(br, b); err != nil {
			return "", err
		}
		return string(b), nil
	}

	ver, err := getU()
	if err != nil {
		return nil, err
	}
	if ver != codecVersion {
		return nil, fmt.Errorf("%w (version %d)", errBadCache, ver)
	}
	rootPath, err := getS()
	if err != nil {
		return nil, err
	}
	count, err := getU()
	if err != nil {
		return nil, err
	}
	if count > 1<<28 {
		return nil, errBadCache
	}

	t := &Tree{RootPath: rootPath, Root: 1}
	for id := uint32(1); id <= uint32(count); id++ {
		var n Node
		if n.Name, err = getS(); err != nil {
			return nil, err
		}
		vals := make([]int64, 5)
		for i := range vals {
			if vals[i], err = getI(); err != nil {
				return nil, err
			}
		}
		n.Alloc, n.Logical, n.Self, n.SelfLog, n.Mtime = vals[0], vals[1], vals[2], vals[3], vals[4]
		p, err := getU()
		if err != nil {
			return nil, err
		}
		n.Parent = uint32(p)
		if n.Parent >= id && n.Parent != Invalid {
			return nil, errBadCache // a parent must precede its child
		}
		f, err := getI()
		if err != nil {
			return nil, err
		}
		d, err := getI()
		if err != nil {
			return nil, err
		}
		n.Files, n.Dirs = int32(f), int32(d)
		if n.Kind, err = br.ReadByte(); err != nil {
			return nil, err
		}
		if n.Flags, err = br.ReadByte(); err != nil {
			return nil, err
		}
		if n.Err, err = getS(); err != nil {
			return nil, err
		}

		got := t.alloc(n)
		if got != id {
			return nil, errBadCache
		}
		if n.Parent != Invalid {
			pn := t.node(n.Parent)
			pn.Children = append(pn.Children, id)
		}
	}
	// Encode wrote children in their sorted order, so the rebuilt tree is
	// already sorted; saying so skips a full re-sort on load.
	t.sorted.Store(true)
	return t, nil
}
