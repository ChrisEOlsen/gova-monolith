package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Every workspace mutation — api.json, the *_gen.go files, model/handler/page
// files — is serialized by an flock on .gova-lock: LOCK_EX for mutators,
// LOCK_SH for readers. The CLI is one process per invocation, so a single
// cross-process lock covers every caller, including parallel subagents sharing
// the bind-mounted /src.
//
// The lock is held across the full read→upsert→write→regenerate transaction in
// updateManifestAt. Without it two scaffolds read the same base manifest and
// the second write silently erases the first's registration.

// lockFilePath is where the cross-process lock lives. It sits in /src, the
// bind mount every harness session shares, next to the code it protects.
const lockFilePath = "/src/.gova-lock"

// lockPath returns the flock target. GOVA_LOCK_PATH overrides it so the
// concurrency tests can point the lock at a temp dir — /src is not writable
// outside the builder container and a test must never contend with a live
// builder for the real lock file.
func lockPath() string {
	if p := os.Getenv("GOVA_LOCK_PATH"); p != "" {
		return p
	}
	return lockFilePath
}

// withWorkspaceLock runs fn holding an exclusive flock. MUST NOT BE NESTED —
// a nested call would block on itself. Code inside the critical section calls
// the *Locked variant of a helper directly.
func withWorkspaceLock(fn func() error) error { return withFlock(syscall.LOCK_EX, fn) }

// withWorkspaceRead runs fn holding a SHARED flock: concurrent readers coexist,
// and a mutator's exclusive lock still excludes them.
func withWorkspaceRead(fn func() error) error { return withFlock(syscall.LOCK_SH, fn) }

func withFlock(mode int, fn func() error) error {
	f, err := os.OpenFile(lockPath(), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("workspace lock: %w", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), mode); err != nil {
		return fmt.Errorf("workspace lock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

// atomicWriteFile replaces path with src via temp-file + rename. A concurrent
// reader either sees the old complete file or the new complete file — never a
// truncated one. The temp file is created in the destination directory so the
// rename stays within one filesystem, which is the only case rename is
// atomic in.
func atomicWriteFile(path string, src []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Clean up the temp file on any failure path; on success, rename already
	// removed it from the namespace.
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(src); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	tmpName = "" // renamed away — the deferred remove must not run
	return nil
}

// atomicWriteJSON marshals v and replaces path with it atomically.
func atomicWriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, append(data, '\n'), 0644)
}
