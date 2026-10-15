package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Mutiur03/pulse/internal/sourcemap"
)

// Snippet records the script's debug id under its own stack, which names the
// script's URL; the browser SDK reads __pulseDebugIds when reporting errors.
// It goes after all mapped code, so no mapping moves.
func Snippet(id string) string {
	return `;!function(){try{var g=typeof globalThis!="undefined"?globalThis:self,s=(new Error).stack;` +
		`s&&((g.__pulseDebugIds=g.__pulseDebugIds||{})[s]="` + id + `")}catch(e){}}();` +
		"\n//# debugId=" + id + "\n"
}

// DebugID derives a stable id from the script, so injecting twice or on two
// machines gives the same id for the same build.
func DebugID(js []byte) string {
	h := sha256.Sum256(js)
	h[6] = h[6]&0x0f | 0x40
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

var reMapURL = regexp.MustCompile(`(?m)^//[#@] sourceMappingURL=(\S+)\s*$`)

// Inject adds a debug id to a script and its map and returns the id. Scripts
// that already have one keep it.
func Inject(js, m []byte) (newJS, newMap []byte, id string, err error) {
	id = sourcemap.DebugIDFromSource(js)
	if id == "" {
		id = DebugID(js)
		snippet := Snippet(id)
		// Keep sourceMappingURL as the last line, where tools look for it.
		all := reMapURL.FindAllIndex(js, -1)
		if loc := lastOf(all); loc != nil && len(bytes.TrimSpace(js[loc[1]:])) == 0 {
			head := js[:loc[0]]
			if len(head) > 0 && head[len(head)-1] != '\n' {
				snippet = "\n" + snippet
			}
			newJS = append(append(append([]byte{}, head...), snippet...), js[loc[0]:]...)
		} else {
			if len(js) > 0 && js[len(js)-1] != '\n' {
				snippet = "\n" + snippet
			}
			newJS = append(append([]byte{}, js...), snippet...)
		}
	} else {
		newJS = js
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(m, &obj); err != nil {
		return nil, nil, "", fmt.Errorf("source map: %w", err)
	}
	obj["debugId"], _ = json.Marshal(id)
	newMap, err = json.Marshal(obj)
	return newJS, newMap, id, err
}

func lastOf(locs [][]int) []int {
	if len(locs) == 0 {
		return nil
	}
	return locs[len(locs)-1]
}

// mapFor finds a script's map: its sourceMappingURL if local, else file.map.
func mapFor(path string, js []byte) string {
	if all := reMapURL.FindAllSubmatch(js, -1); len(all) > 0 {
		ref := string(all[len(all)-1][1])
		if !strings.Contains(ref, ":") {
			return filepath.Join(filepath.Dir(path), filepath.FromSlash(ref))
		}
	}
	return path + ".map"
}

func injectCmd(args []string) error {
	fl := flag.NewFlagSet("inject", flag.ContinueOnError)
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() != 1 {
		return fmt.Errorf("usage: pulse-cli sourcemaps inject <dir>")
	}
	n := 0
	err := filepath.WalkDir(fl.Arg(0), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !(strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".mjs") || strings.HasSuffix(path, ".cjs")) {
			return err
		}
		js, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mp := mapFor(path, js)
		m, err := os.ReadFile(mp)
		if os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return err
		}
		newJS, newMap, id, err := Inject(js, m)
		if err != nil {
			return fmt.Errorf("%s: %w", mp, err)
		}
		if err := os.WriteFile(path, newJS, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(mp, newMap, 0o644); err != nil {
			return err
		}
		fmt.Printf("%s  %s\n", id, path)
		n++
		return nil
	})
	if err == nil {
		fmt.Printf("injected debug ids into %d files\n", n)
	}
	return err
}
