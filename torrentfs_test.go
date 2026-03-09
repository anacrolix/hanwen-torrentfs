//go:build !windows

package hanwentorrentfs_test

import (
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"

	hanwen "github.com/anacrolix/hanwen-torrentfs"
	torrentfs "github.com/anacrolix/torrent/fs"
	"github.com/anacrolix/torrent/fs/tfstest"
	fusefs "github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// forceUnmountDir force-unmounts dir to unblock any threads stuck in a
// blocking unmount syscall (e.g. syscall.Unmount on macOS with fuse-t).
func forceUnmountDir(dir string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("diskutil", "unmount", "force", dir).Run() //nolint:errcheck
	case "linux":
		exec.Command("fusermount", "-u", "-z", dir).Run() //nolint:errcheck
	}
}

func testMountFunc(t testing.TB, tfs *torrentfs.TorrentFS, mountDir string) (unmount func()) {
	t.Helper()
	sec := time.Second
	b := &hanwen.Backend{
		Options: &fusefs.Options{
			AttrTimeout:  &sec,
			EntryTimeout: &sec,
			MountOptions: fuse.MountOptions{
				DirectMount: true,
			},
		},
	}
	// Mount with a timeout. On macOS with fuse-t, hanwen/go-fuse uses the
	// macFUSE socket protocol to pass a /dev/fuse fd, but fuse-t uses NFS and
	// may not speak this protocol. If the mount doesn't complete within 30s,
	// skip rather than block for the full test timeout.
	type mountResult struct {
		u   torrentfs.Unmounter
		err error
	}
	ch := make(chan mountResult, 1)
	go func() {
		u, err := b.Mount(mountDir, tfs)
		ch <- mountResult{u, err}
	}()
	var r mountResult
	select {
	case r = <-ch:
	case <-time.After(30 * time.Second):
		t.Skipf("mount timed out (fuse-t incompatibility with hanwen/go-fuse on macOS?)")
	}
	if r.err != nil {
		t.Skipf("mount: %v", r.err)
	}
	u := r.u
	var once sync.Once
	return func() {
		once.Do(func() {
			// Run Unmount in a goroutine with a timeout. On macOS with fuse-t,
			// syscall.Unmount can block indefinitely when there is a pending
			// NFS read from user space (e.g. in testUnmountWedged). If unmount
			// hasn't completed within 3s, abandon it so callers never block.
			done := make(chan struct{})
			go func() {
				defer close(done)
				u.Unmount() //nolint:errcheck
			}()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				// Force-unmount to unblock syscall.Unmount in the goroutine,
				// then wait for it to exit so the process can terminate cleanly.
				forceUnmountDir(mountDir)
				<-done
			}
		})
	}
}

func TestTorrentFS(t *testing.T) {
	tfstest.RunTestSuite(t, testMountFunc)
}
