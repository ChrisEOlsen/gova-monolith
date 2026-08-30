package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// The builder is called by more than one process (one MCP stdio session per
// harness session — Claude Code and opencode each get their own) and each
// process is itself concurrent (mcp-go dispatches tool calls on goroutines).
// Every mutation of the workspace — api.json, routes_gen.go, pages_gen.go,
// model/handler/page files — is therefore serialized two ways:
//
//   - workspaceMu is in-process. It covers the common case: parallel tool
//     calls from one session cannot interleave their read-modify-write.
//   - withWorkspaceLock holds flocks on .gova-lock beside the code it
//     protects: LOCK_EX for mutators, LOCK_SH for readers. It covers the
//     rest — two harness sessions sharing the same bind-mounted /src. The
//     file is beside /src/app, in the mounted tree, so every session's
//     open() lands on the same inode.
//
// Both guards are held across the full read→upsert→write→regenerate
// transaction in updateManifestAt. A tool that mutated the workspace without
// holding them would reintroduce the lost-update race this closes: two
// scaffolds read the same base manifest, both write, and the second silently
// erases the first's registration — while inspect_app reports divergence and
// nothing repairs it.

// workspaceMu serializes workspace access within one builder process. Not
// re-entrant: a function called inside a locked section must use the *Locked
// variant, never call withWorkspaceLock again.
var workspaceMu sync.Mutex

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

// withWorkspaceLock runs fn holding the in-process mutex and an exclusive
// flock. MUST NOT BE NESTED — sync.Mutex is not re-entrant, and a nested call
// deadlocks against itself. Code running inside the critical section that
// needs a locked helper calls the helper's *Locked variant directly.
func withWorkspaceLock(fn func() error) error {
	workspaceMu.Lock()
	f, err := os.OpenFile(lockPath(), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		workspaceMu.Unlock()
		return fmt.Errorf("workspace lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		workspaceMu.Unlock()
		return fmt.Errorf("workspace lock: %w", err)
	}
	defer func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		workspaceMu.Unlock()
	}()
	return fn()
}

// withWorkspaceRead runs fn holding the in-process mutex and a SHARED flock.
// Shared means concurrent readers in different processes coexist; the
// exclusive flock of a mutator still excludes them. In-process the mutex
// alone already orders readers behind mutators, since a mutator holds
// workspaceMu for its whole transaction.
func withWorkspaceRead(fn func() error) error {
	workspaceMu.Lock()
	f, err := os.OpenFile(lockPath(), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		workspaceMu.Unlock()
		return fmt.Errorf("workspace lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil {
		f.Close()
		workspaceMu.Unlock()
		return fmt.Errorf("workspace lock: %w", err)
	}
	defer func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		workspaceMu.Unlock()
	}()
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