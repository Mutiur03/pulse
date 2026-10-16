package service

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/Mutiur03/pulse/internal/modules/sourcemaps/domain"
)

const validMap = `{"version":3,"sources":["a.ts"],"names":[],"mappings":"AAAA","debugId":"85314830-023f-4cf1-a267-535f4e37bb17"}`

func form(t *testing.T, files map[string]string) *multipart.Form {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for name, body := range files {
		fw, err := mw.CreateFormFile(name, name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write([]byte(body))
	}
	_ = mw.Close()
	f, err := multipart.NewReader(&buf, mw.Boundary()).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.RemoveAll() })
	return f
}

func TestReadUploads(t *testing.T) {
	ups, err := ReadUploads(form(t, map[string]string{"~/b.js": validMap, "~/a.js.map": validMap}), 10, 1<<20)
	if err != nil || len(ups) != 2 {
		t.Fatalf("%v %v", ups, err)
	}
	if ups[0].Name != "~/a.js.map" || ups[1].Name != "~/b.js.map" || ups[0].DebugID == "" {
		t.Fatalf("%+v", ups)
	}

	cases := map[string]struct {
		files    map[string]string
		maxFiles int
		maxSize  int64
		msg      string
		tooLarge bool
	}{
		"none":      {nil, 10, 1 << 20, "no source map files in the upload", false},
		"too many":  {map[string]string{"a.js.map": validMap, "b.js.map": validMap}, 1, 1 << 20, "too many files in one upload", false},
		"too large": {map[string]string{"a.js.map": validMap}, 10, 10, "~/a.js.map is larger than 0 MB", true},
		"not a map": {map[string]string{"a.js.map": "{}"}, 10, 1 << 20, "~/a.js.map: ", false},
	}
	for name, c := range cases {
		_, err := ReadUploads(form(t, c.files), c.maxFiles, c.maxSize)
		var ue *domain.UploadError
		if !errors.As(err, &ue) || !strings.HasPrefix(ue.Msg, c.msg) || ue.TooLarge != c.tooLarge || ue.Error() != ue.Msg {
			t.Errorf("%s: %v", name, err)
		}
	}
}

type memReleases struct {
	arts   []domain.Artifact
	failOn string
}

func (m *memReleases) EnsureRelease(_ context.Context, project int64, version string) (domain.Release, error) {
	if m.failOn == "release" {
		return domain.Release{}, errors.New("db down")
	}
	return domain.Release{ID: 9, ProjectID: project, Version: version}, nil
}

func (m *memReleases) PutArtifact(_ context.Context, a domain.Artifact) (domain.Artifact, error) {
	if m.failOn == "artifact" {
		return domain.Artifact{}, errors.New("db down")
	}
	a.ID = int64(len(m.arts) + 1)
	m.arts = append(m.arts, a)
	return a, nil
}

type memObjects map[string][]byte

func (m memObjects) Put(_ context.Context, key string, data []byte) error {
	if key == "" {
		return errors.New("no key")
	}
	m[key] = data
	return nil
}

func TestPublish(t *testing.T) {
	ctx := context.Background()
	rs, objs := &memReleases{}, memObjects{}
	ups := []domain.Upload{{Name: "a.js.map", Data: []byte(validMap), DebugID: "d"}}
	arts, err := Publish(ctx, rs, objs, 3, "v1", ups)
	if err != nil || len(arts) != 1 {
		t.Fatalf("%v %v", arts, err)
	}
	a := arts[0]
	if a.ReleaseID != 9 || a.ProjectID != 3 || a.SHA256 != domain.SHA256(ups[0].Data) || a.ObjectKey != domain.ObjectKey(3, a.SHA256) ||
		a.Size != int64(len(validMap)) || string(objs[a.ObjectKey]) != validMap {
		t.Fatalf("%+v", a)
	}
	if _, err := Publish(ctx, rs, objs, 3, "v1", nil); err == nil {
		t.Fatal("empty publish")
	}
	for _, f := range []string{"release", "artifact"} {
		if _, err := Publish(ctx, &memReleases{failOn: f}, objs, 3, "v1", ups); err == nil {
			t.Fatalf("%s failure ignored", f)
		}
	}
}
