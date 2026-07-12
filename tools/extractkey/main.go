// Command extractkey pulls the Owlet TUTK/Kalay SDK license key out of the
// Owlet Android app.
//
// The key is just a base64 string literal (it always starts with "AQAAA")
// baked into the app's DEX code, so there's no need to actually parse the DEX
// format -- the bytes survive as plain ASCII and a raw scan finds them.
//
// Usage (from the tools/ directory):
//
//	go run ./extractkey com.owletcare.sleep.xapk   # app bundle
//	go run ./extractkey base.apk                   # single apk
//	go run ./extractkey ./dex-dir                  # extracted classes*.dex
//	go run ./extractkey classes.dex                # a single dex
//
// Get the app from any APK mirror (e.g. `apkeep -a com.owletcare.sleep .`),
// then put the printed key into your .env as SDK_KEY.
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// TUTK license keys are base64 and begin with the version marker "AQAAA".
var keyRE = regexp.MustCompile(`AQAAA[A-Za-z0-9+/]{20,}={0,2}`)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: extractkey <owlet.xapk | .apk | .dex | dir>")
		os.Exit(2)
	}

	found := map[string]bool{}
	if err := scan(os.Args[1], found); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if len(found) == 0 {
		fmt.Fprintln(os.Stderr, `no license key found (looked for base64 starting "AQAAA")`)
		os.Exit(1)
	}

	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	// Longest first: the real key is much longer than any incidental match.
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		fmt.Printf("%s  (%d chars)\n", k, len(k))
	}
}

// scan dispatches based on what the path is: a directory of dex files, a
// zip-based container (.apk/.xapk/.apks), or a raw .dex file.
func scan(path string, found map[string]bool) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".dex") {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			match(b, found)
			return nil
		})
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if isZip(b) {
		return scanZip(b, found)
	}
	match(b, found) // assume raw .dex
	return nil
}

// scanZip walks a zip container, scanning .dex entries and recursing into any
// nested .apk entries (app bundles like .xapk/.apks wrap the base apk).
func scanZip(b []byte, found map[string]bool) error {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".dex") && !strings.HasSuffix(f.Name, ".apk") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}
		if strings.HasSuffix(f.Name, ".apk") {
			if err := scanZip(data, found); err != nil {
				return err
			}
			continue
		}
		match(data, found)
	}
	return nil
}

func match(b []byte, found map[string]bool) {
	for _, m := range keyRE.FindAll(b, -1) {
		found[string(m)] = true
	}
}

func isZip(b []byte) bool {
	return len(b) >= 2 && b[0] == 'P' && b[1] == 'K'
}
