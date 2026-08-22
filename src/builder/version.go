package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"io/fs"
	"sort"
	"strings"
	"sync"
)

// ═════════════════════════════════════════════════════════════════════════════
// WHICH TEMPLATE IS THIS APP BUILT FROM?
//
// An app does not depend on gova-monolith; it vendors a COPY of src/builder at
// scaffold time and is a fork from that moment. Fix a defect here and no
// existing app ever learns about it. That is not hypothetical — it is how one
// app spent weeks carrying three defects that had already been fixed upstream,
// and the reason nobody noticed is that there was NO SIGNAL. The app worked,
// its tests passed, and the only way the staleness surfaced was a person
// hitting the same bugs by hand and going looking.
//
// This does not solve that on its own — nothing offline can, because "am I
// behind?" needs the other side of the comparison. What it does is make the
// question answerable at all:
//
//   - Every manifest write records the version and fingerprint of the builder
//     that wrote it, so an app can always say what it was built from.
//   - inspect_app compares that record against the RUNNING builder, which
//     catches the specific mistake of syncing src/builder and forgetting that
//     the mcp image embeds its templates at IMAGE BUILD time — the stale-binary
//     trap CLAUDE.md warns about, now visible instead of silent.
//   - The version string is plain text a human or CI can diff against this
//     file in gova-monolith to answer "how far behind am I".
// ═════════════════════════════════════════════════════════════════════════════

//go:embed VERSION
var versionFile string

var (
	versionOnce sync.Once
	version     string
	fingerprint string
)

// templateVersion is the released version from the VERSION file: the first
// line that is neither blank nor a comment.
func templateVersion() string {
	loadVersion()
	return version
}

// templateFingerprint is a hash over every embedded template.
//
// The VERSION file is a promise a human makes; this is what the binary actually
// carries. They come apart in the case that matters most — someone edits a
// template locally and does not bump the version — and recording both means the
// manifest can show that the code it was generated from is not the code the
// version names.
func templateFingerprint() string {
	loadVersion()
	return fingerprint
}

func loadVersion() {
	versionOnce.Do(func() {
		version = "unknown"
		for _, line := range strings.Split(versionFile, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			version = line
			break
		}

		// Sorted, name-and-content, so the hash is stable across filesystems
		// and changes when any template does.
		names := []string{}
		_ = fs.WalkDir(templateFS, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			names = append(names, path)
			return nil
		})
		sort.Strings(names)

		h := sha256.New()
		for _, name := range names {
			body, err := templateFS.ReadFile(name)
			if err != nil {
				continue
			}
			h.Write([]byte(name))
			h.Write([]byte{0})
			h.Write(body)
			h.Write([]byte{0})
		}
		fingerprint = "sha256:" + hex.EncodeToString(h.Sum(nil))
	})
}
