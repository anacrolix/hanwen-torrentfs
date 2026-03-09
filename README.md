# hanwen-torrentfs

A [torrentfs](https://pkg.go.dev/github.com/anacrolix/torrent/fs) backend that
mounts a torrent client as a read-only FUSE filesystem using
[hanwen/go-fuse/v2](https://github.com/hanwen/go-fuse).

## Why does this exist?

The `torrent/fs` package defines a FUSE-library-agnostic `Backend` interface.
This module provides one concrete implementation.  A second implementation,
[og-torrentfs](https://github.com/anacrolix/og-torrentfs), uses
`anacrolix/fuse` instead.  Having both lets users pick the FUSE library that
works best on their platform.

Note: `hanwen/go-fuse` uses the macFUSE socket protocol on macOS, which is
incompatible with fuse-t.  If you need fuse-t support on macOS, use
`og-torrentfs`.

## Usage

```go
import (
    hanwen    "github.com/anacrolix/hanwen-torrentfs"
    torrentfs "github.com/anacrolix/torrent/fs"
)

tfs := torrentfs.New(cl)
defer tfs.Destroy()

b := &hanwen.Backend{}
u, err := b.Mount("/mnt/torrents", tfs)
if err != nil {
    log.Fatal(err)
}
defer u.Unmount()
```
