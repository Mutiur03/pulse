package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// batchBytes keeps each request under the server's 50 MB limit.
const batchBytes = 40 << 20

type mapFile struct {
	path, name string
	size       int64
}

// ArtifactName is the name a map is stored under: the URL prefix the files
// are served from, plus the path inside dir. A prefix without a scheme is
// matched against the URL path on any host ("~/").
func ArtifactName(prefix, rel string) string {
	rel = filepath.ToSlash(rel)
	if strings.Contains(prefix, "://") {
		return strings.TrimSuffix(prefix, "/") + "/" + rel
	}
	p := strings.Trim(strings.TrimPrefix(prefix, "~"), "/")
	if p == "" {
		return "~/" + rel
	}
	return "~/" + p + "/" + rel
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func uploadCmd(args []string) error {
	fl := flag.NewFlagSet("upload", flag.ContinueOnError)
	release := fl.String("release", os.Getenv("PULSE_RELEASE"), "release the maps belong to")
	prefix := fl.String("url-prefix", "~/", "URL prefix the files are served under")
	base := fl.String("url", env("PULSE_URL", "http://localhost:8081"), "Pulse API base URL")
	project := fl.String("project", os.Getenv("PULSE_PROJECT"), "project id")
	token := fl.String("token", os.Getenv("PULSE_UPLOAD_TOKEN"), "upload token")
	del := fl.Bool("delete", false, "delete the maps after uploading")
	// Allow flags after the directory too.
	var dirs []string
	for len(args) > 0 {
		if err := fl.Parse(args); err != nil {
			return err
		}
		if fl.NArg() == 0 {
			break
		}
		dirs = append(dirs, fl.Arg(0))
		args = fl.Args()[1:]
	}
	if len(dirs) != 1 || *release == "" || *project == "" || *token == "" {
		return fmt.Errorf("usage: pulse-cli sourcemaps upload --release <version> --project <id> --token <token> [--url-prefix ~/assets/] <dir>")
	}
	dir := dirs[0]
	var files []mapFile
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".map") {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files = append(files, mapFile{path: path, name: ArtifactName(*prefix, rel), size: info.Size()})
		return nil
	})
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no .map files under %s", dir)
	}
	endpoint := strings.TrimSuffix(*base, "/") + "/v1/projects/" + url.PathEscape(*project) +
		"/releases/" + url.PathEscape(*release) + "/sourcemaps"
	client := &http.Client{Timeout: 2 * time.Minute}
	for start := 0; start < len(files); {
		end, size := start, int64(0)
		for end < len(files) && (end == start || size+files[end].size <= batchBytes) {
			size += files[end].size
			end++
		}
		if err := send(client, endpoint, *token, files[start:end]); err != nil {
			return err
		}
		start = end
	}
	for _, f := range files {
		fmt.Printf("uploaded %s\n", f.name)
		if *del {
			if err := os.Remove(f.path); err != nil {
				return err
			}
		}
	}
	fmt.Printf("uploaded %d source maps to release %s\n", len(files), *release)
	return nil
}

func send(client *http.Client, endpoint, token string, files []mapFile) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			return err
		}
		part, err := w.CreateFormFile(f.name, filepath.Base(f.path))
		if err != nil {
			return err
		}
		if _, err := part.Write(data); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusCreated {
		var e struct{ Error string }
		if json.Unmarshal(msg, &e) == nil && e.Error != "" {
			return fmt.Errorf("upload failed (%d): %s", resp.StatusCode, e.Error)
		}
		return fmt.Errorf("upload failed (%d)", resp.StatusCode)
	}
	return nil
}
