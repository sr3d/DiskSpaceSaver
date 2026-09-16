// Command scan is the M1 harness: it exercises the scanner and tree without
// any UI, reports timing and the largest files, and can emit the same frame
// stream the privileged helper speaks.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/sr3d/diskspacesaver/internal/helperproto"
	"github.com/sr3d/diskspacesaver/internal/scanner"
	"github.com/sr3d/diskspacesaver/internal/tree"
)

func main() {
	top := flag.Int("top", 20, "show the N largest files")
	workers := flag.Int("workers", 0, "worker count (0 = auto)")
	bulk := flag.Bool("bulk", false, "use the getattrlistbulk fast path")
	frames := flag.Bool("frames", false, "write the msgpack frame stream to stdout instead of a report")
	quiet := flag.Bool("quiet", false, "no progress output")
	bench := flag.Bool("bench", false, "walk only: drain frames without building a tree")
	flag.Parse()

	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}
	root = scanner.ResolveScanRoot(root)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	s := scanner.New(scanner.Options{Root: root, Workers: *workers, UseBulk: *bulk})
	start := time.Now()

	go func() {
		for p := range s.Progress() {
			if *quiet || *frames {
				continue
			}
			fmt.Fprintf(os.Stderr, "\r%8d dirs  %9d files  %10s  %s\033[K",
				p.Dirs, p.Files, humanBytes(p.Bytes), trunc(p.CurrentPath, 48))
		}
		if !*quiet && !*frames {
			fmt.Fprintln(os.Stderr)
		}
	}()

	if *frames {
		w := helperproto.NewFrameWriter(os.Stdout)
		go s.Run(ctx)
		n := 0
		for f := range s.Frames() {
			if err := w.Write(f); err != nil {
				fatal(err)
			}
			n++
		}
		if err := w.Close(); err != nil {
			fatal(err)
		}
		fmt.Fprintf(os.Stderr, "%d frames in %s\n", n, time.Since(start).Round(time.Millisecond))
		return
	}

	if *bench {
		go s.Run(ctx)
		var n, e int64
		for f := range s.Frames() {
			n++
			e += int64(len(f.Entry))
		}
		d := time.Since(start)
		fmt.Printf("walk-only   %d frames  %d entries  %s  (%.0f entries/sec)\n",
			n, e, d.Round(time.Millisecond), float64(e)/d.Seconds())
		return
	}

	t := tree.New(root)
	go s.Run(ctx)
	for f := range s.Frames() {
		t.Apply(f)
	}
	if err := s.Err(); err != nil {
		fatal(err)
	}
	walk := time.Since(start)
	t.Finalize()
	total := time.Since(start)

	rootNode, _ := t.Snapshot(t.Root)
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	fmt.Printf("\nroot        %s\n", root)
	fmt.Printf("total       %s allocated  (%s logical)\n", humanBytes(rootNode.Alloc), humanBytes(rootNode.Logical))
	fmt.Printf("entries     %d files  %d dirs  (%d nodes)\n", rootNode.Files, rootNode.Dirs, t.Count())
	fmt.Printf("errors      %d inaccessible\n", t.Errors())
	fmt.Printf("walk        %s   sort %s   total %s\n",
		walk.Round(time.Millisecond), (total - walk).Round(time.Millisecond), total.Round(time.Millisecond))
	if secs := total.Seconds(); secs > 0 {
		fmt.Printf("throughput  %.0f entries/sec\n", float64(t.Count())/secs)
	}
	fmt.Printf("memory      %s heap in use, %s total alloc\n", humanBytes(int64(ms.HeapInuse)), humanBytes(int64(ms.TotalAlloc)))

	if *top > 0 {
		fmt.Printf("\nlargest %d files:\n", *top)
		for _, n := range t.TopN(*top) {
			fmt.Printf("  %10s  %s\n", humanBytes(n.Size), t.Path(n.ID))
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "scan:", err)
	os.Exit(1)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n+3:]
}

func humanBytes(b int64) string {
	const u = 1000
	if b < u {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(u), 0
	for n := b / u; n >= u; n /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}
