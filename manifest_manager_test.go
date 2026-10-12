package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginReadOnlyScanAndBackup(t *testing.T) {
	steam := t.TempDir()
	plugin := filepath.Join(steam, "config", "stplug-in")
	if err := os.MkdirAll(plugin, 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte("-- harmless sample file; never executed")
	for _, file := range []string{"4078430.lua", "24010.manifest", "bad.lua"} {
		if err := os.WriteFile(filepath.Join(plugin, file), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	scan, err := scanPluginDir(plugin)
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Files) != 2 {
		t.Fatalf("wrong scan %#v", scan.Files)
	}
	app, err := newCatalog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	progress, err := backupLocalPlugin(context.Background(), app, scan, nil)
	if err != nil || progress.Added != 1 || progress.Duplicates != 1 {
		t.Fatalf("backup result %#v %v", progress, err)
	}
	original, err := os.ReadFile(filepath.Join(plugin, "4078430.lua"))
	if err != nil || !bytes.Equal(original, data) {
		t.Fatal("source modified")
	}
}
func TestIndexChecksAndChecksum(t *testing.T) {
	data := []byte("test manifest data")
	sha := sha256.Sum256(data)
	template := func(u, checksum string) []byte {
		return []byte(fmt.Sprintf(`{"format":"astronyx-manifests-v1","source":"user-authorized-test","authorized":true,"files":[{"name":"440.manifest","url":%q,"sha256":%q}]}`, u, checksum))
	}
	if _, err := parseAuthorizedIndex(template("http://example.org/a", hex.EncodeToString(sha[:]))); err == nil {
		t.Fatal("http accepted")
	}
	if _, err := parseAuthorizedIndex(template("https://localhost/a", hex.EncodeToString(sha[:]))); err == nil {
		t.Fatal("localhost accepted")
	}
	if _, err := parseAuthorizedIndex(template("https://example.org/a", "notahash")); err == nil {
		t.Fatal("bad hash accepted")
	}
	idx, err := parseAuthorizedIndex(template("https://example.org/a", hex.EncodeToString(sha[:])))
	if err != nil {
		t.Fatal(err)
	}
	c, err := newCatalog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
	})}
	progress, err := downloadAuthorized(context.Background(), client, c, idx, nil)
	if err != nil || progress.Added != 1 {
		t.Fatalf("download result: %#v, %v", progress, err)
	}
	if strings.ToLower(c.Items[0].Type) != "manifest" {
		t.Fatal("wrong file type")
	}
	progress, err = downloadAuthorized(context.Background(), client, c, idx, nil)
	if err != nil || progress.Duplicates != 1 {
		t.Fatalf("idempotency: %#v %v", progress, err)
	}
	idx.Files[0].SHA256 = strings.Repeat("0", 64)
	progress, err = downloadAuthorized(context.Background(), client, c, idx, nil)
	if err != nil || progress.Skipped != 1 {
		t.Fatalf("hash bypass: %#v %v", progress, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
