//go:build !windows

// Package hanwentorrentfs implements the torrentfs.Backend interface using
// github.com/hanwen/go-fuse/v2.
package hanwentorrentfs

import (
	"context"
	"errors"
	"syscall"
	"time"

	fusefs "github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/anacrolix/torrent"
	torrentfs "github.com/anacrolix/torrent/fs"
)

const defaultMode = 0o555

// Backend implements torrentfs.Backend using hanwen/go-fuse.
type Backend struct {
	// Options overrides the default fs.Options used when mounting. If nil,
	// sensible defaults are used.
	Options *fusefs.Options
}

// Mount mounts tfs at mountDir and returns an Unmounter.
func (b *Backend) Mount(mountDir string, tfs *torrentfs.TorrentFS) (torrentfs.Unmounter, error) {
	opts := b.Options
	if opts == nil {
		sec := time.Second
		opts = &fusefs.Options{
			AttrTimeout:  &sec,
			EntryTimeout: &sec,
		}
	}
	return fusefs.Mount(mountDir, newRoot(tfs), opts)
}

// node holds the shared fields for all non-root filesystem nodes.
type node struct {
	path string
	tfs  *torrentfs.TorrentFS
	t    *torrent.Torrent
}

// rootNode lists all torrents.
type rootNode struct {
	fusefs.Inode
	tfs *torrentfs.TorrentFS
}

func newRoot(tfs *torrentfs.TorrentFS) *rootNode {
	return &rootNode{tfs: tfs}
}

// dirNode is a directory within a multi-file torrent.
type dirNode struct {
	fusefs.Inode
	node
}

// fileNode is a file within a torrent.
type fileNode struct {
	fusefs.Inode
	node
	f *torrent.File
}

// fileHandle provides read access to a torrent file.
type fileHandle struct {
	fn *fileNode
	tf *torrent.File
}

var _ fusefs.NodeOpener = (*fileNode)(nil)
var _ fusefs.FileReader = fileHandle{}
var _ fusefs.FileReleaser = fileHandle{}

func entryMode(isDir bool) uint32 {
	if isDir {
		return syscall.S_IFDIR
	}
	return syscall.S_IFREG
}

// --- rootNode ---

func (rn *rootNode) Getattr(_ context.Context, _ fusefs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFDIR | defaultMode
	return 0
}

func (rn *rootNode) Readdir(_ context.Context) (fusefs.DirStream, syscall.Errno) {
	entries := torrentfs.RootEntries(rn.tfs)
	fuseEntries := make([]fuse.DirEntry, len(entries))
	for i, e := range entries {
		fuseEntries[i] = fuse.DirEntry{Name: e.Name, Mode: entryMode(e.IsDir)}
	}
	return fusefs.NewListDirStream(fuseEntries), 0
}

func (rn *rootNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fusefs.Inode, syscall.Errno) {
	result, ok := torrentfs.RootLookup(rn.tfs, name)
	if !ok {
		return nil, syscall.ENOENT
	}
	n := node{tfs: rn.tfs, t: result.Torrent}
	if !result.IsDir {
		out.Mode = syscall.S_IFREG | defaultMode
		out.Size = uint64(result.File.Length())
		fn := &fileNode{node: n, f: result.File}
		return rn.NewInode(ctx, fn, fusefs.StableAttr{Mode: syscall.S_IFREG}), 0
	}
	out.Mode = syscall.S_IFDIR | defaultMode
	return rn.NewInode(ctx, &dirNode{node: n}, fusefs.StableAttr{Mode: syscall.S_IFDIR}), 0
}

// --- dirNode ---

func (dn *dirNode) Getattr(_ context.Context, _ fusefs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFDIR | defaultMode
	return 0
}

func (dn *dirNode) Readdir(_ context.Context) (fusefs.DirStream, syscall.Errno) {
	entries := torrentfs.DirEntries(dn.t, dn.path)
	fuseEntries := make([]fuse.DirEntry, len(entries))
	for i, e := range entries {
		fuseEntries[i] = fuse.DirEntry{Name: e.Name, Mode: entryMode(e.IsDir)}
	}
	return fusefs.NewListDirStream(fuseEntries), 0
}

func (dn *dirNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fusefs.Inode, syscall.Errno) {
	result, ok := torrentfs.DirLookup(dn.t, dn.path, name)
	if !ok {
		return nil, syscall.ENOENT
	}
	n := dn.node
	n.path = result.Path
	if !result.IsDir {
		out.Mode = syscall.S_IFREG | defaultMode
		out.Size = uint64(result.File.Length())
		return dn.NewInode(ctx, &fileNode{node: n, f: result.File}, fusefs.StableAttr{Mode: syscall.S_IFREG}), 0
	}
	out.Mode = syscall.S_IFDIR | defaultMode
	return dn.NewInode(ctx, &dirNode{node: n}, fusefs.StableAttr{Mode: syscall.S_IFDIR}), 0
}

// --- fileNode ---

func (fn *fileNode) Getattr(_ context.Context, _ fusefs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFREG | defaultMode
	out.Size = uint64(fn.f.Length())
	return 0
}

func (fn *fileNode) Open(_ context.Context, _ uint32) (fusefs.FileHandle, uint32, syscall.Errno) {
	return fileHandle{fn: fn, tf: fn.f}, 0, 0
}

// --- fileHandle ---

func (me fileHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	n, err := torrentfs.ReadFile(ctx, me.fn.tfs, me.tf, dest, off)
	if err != nil {
		if errors.Is(err, torrentfs.ErrDestroyed) {
			return nil, syscall.EIO
		}
		return nil, syscall.EINTR
	}
	return fuse.ReadResultData(dest[:n]), 0
}

func (me fileHandle) Release(_ context.Context) syscall.Errno {
	return 0
}
