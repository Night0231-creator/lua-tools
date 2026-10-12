package main

import (
	"archive/zip"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseVDF(t *testing.T) {
	input := []byte(`"libraryfolders" { "0" { "path" "C:\\Program Files (x86)\\Steam" "apps" { "42" "4" } } "1" "D:\\SteamLibrary" }`)
	node, err := parseVDF(input)
	if err != nil {
		t.Fatal(err)
	}
	folders := node.obj("libraryfolders")
	if folders == nil {
		t.Fatal("libraryfolders missing")
	}
	child, ok := folders.get("0").(vdfNode)
	if !ok {
		t.Fatal("child 0 missing")
	}
	if got := child.str("PATH"); got != `C:\Program Files (x86)\Steam` {
		t.Fatalf("invalid path: %q", got)
	}
	if folders.str("1") != `D:\SteamLibrary` {
		t.Fatal("legacy library format failed")
	}
	if _, err := parseVDF([]byte(`"Broken" {`)); err == nil {
		t.Fatal("accepted broken VDF")
	}
}
func TestScanSteamMultipleLibraries(t *testing.T) {
	root := t.TempDir()
	lib := t.TempDir()
	for _, p := range []string{filepath.Join(root, "steamapps"), filepath.Join(lib, "steamapps", "common", "Game2"), filepath.Join(root, "steamapps", "common", "Game1")} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "steam.exe"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	config := `"libraryfolders" { "0" { "path" "` + strings.ReplaceAll(root, `\`, `\\`) + `" } "1" { "path" "` + strings.ReplaceAll(lib, `\`, `\\`) + `" } }`
	if err := os.WriteFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	for _, it := range []struct{ path, id, name, folder string }{{root, "42", "Jogo 1", "Game1"}, {lib, "900", "Jogo 2", "Game2"}} {
		content := `"AppState" { "appid" "` + it.id + `" "name" "` + it.name + `" "installdir" "` + it.folder + `" "StateFlags" "4" }`
		if err := os.WriteFile(filepath.Join(it.path, "steamapps", "appmanifest_"+it.id+".acf"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	scan := scanSteam(root)
	if len(scan.Libraries) != 2 || len(scan.Games) != 2 {
		t.Fatalf("unexpected scan: %#v", scan)
	}
	for _, g := range scan.Games {
		if !g.Installed {
			t.Errorf("should be installed: %#v", g)
		}
	}
}
func TestImportAndDedup(t *testing.T) {
	c, e := newCatalog(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	acf := []byte(`"AppState" { "appid" "730" "name" "Test Game" "installdir" "test" }`)
	yes, e := c.addFile("appmanifest_730.acf", "Upload", acf)
	if e != nil || !yes {
		t.Fatalf("first add: %v %v", yes, e)
	}
	yes, e = c.addFile("appmanifest_730.acf", "Upload", acf)
	if e != nil || yes {
		t.Fatalf("duplicate: %v %v", yes, e)
	}
	if _, e = c.addFile("appmanifest_730.acf", "Upload", []byte(`"AppState" { "appid" "999" }`)); e == nil {
		t.Fatal("mismatched AppID accepted")
	}
	if _, e = c.addFile("corrupt.acf", "Upload", []byte(`not valid VDF {`)); e == nil {
		t.Fatal("bad acf accepted")
	}
	if len(c.Items) != 1 || c.Items[0].AppID != "730" {
		t.Fatalf("bad catalog: %#v", c.Items)
	}
	b, e := c.backup()
	if e != nil {
		t.Fatal(e)
	}
	zr, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if e != nil || len(zr.File) != 2 {
		t.Fatalf("bad backup: %v", e)
	}
	c2, e := newCatalog(c.dir)
	if e != nil {
		t.Fatal(e)
	}
	if len(c2.Items) != 1 {
		t.Fatal("catalog did not persist")
	}
}
func TestZipImport(t *testing.T) {
	c, e := newCatalog(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	f, _ := z.Create("../../scary.lua")
	_, _ = f.Write([]byte("lua text kept inert"))
	f, _ = z.Create("file.txt")
	_, _ = f.Write([]byte("unsupported"))
	_ = z.Close()
	r, e := c.importZip("bundle.zip", buf.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	if r.Added != 1 || r.Skipped != 1 || c.Items[0].Name != "scary.lua" {
		t.Fatalf("falha na importação: %+v", r)
	}
}
func TestMultipart(t *testing.T) {
	c, e := newCatalog(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for name, content := range map[string]string{"game.lua": "some lua data", "manifest.manifest": "binary info"} {
		p, _ := writer.CreateFormFile("files", name)
		_, _ = io.WriteString(p, content)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/import", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	reader, e := req.MultipartReader()
	if e != nil {
		t.Fatal(e)
	}
	result := c.importMultipart(reader)
	if result.Added != 2 {
		t.Fatalf("expected 2 imports: %+v", result)
	}
}
