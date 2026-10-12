package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxSingleFile = 16 << 20
const maxZipEntry = 4 << 20
const maxImportFiles = 4000

type Imported struct {
	Hash        string `json:"hash"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	AppID       string `json:"appId,omitempty"`
	DepotID     string `json:"depotId,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Size        int64  `json:"size"`
	AddedAt     string `json:"addedAt"`
	Source      string `json:"source"`
}
type Catalog struct {
	mu            sync.Mutex
	dir           string
	Items         []Imported `json:"items"`
	SteamOverride string     `json:"steamOverride"`
}

func newCatalog(dir string) (*Catalog, error) {
	for _, child := range []string{dir, filepath.Join(dir, "imports"), filepath.Join(dir, "backups")} {
		if err := os.MkdirAll(child, 0700); err != nil {
			return nil, err
		}
	}
	c := &Catalog{dir: dir, Items: []Imported{}}
	b, err := os.ReadFile(filepath.Join(dir, "catalog.json"))
	if err == nil {
		_ = json.Unmarshal(b, c)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if c.Items == nil {
		c.Items = []Imported{}
	}
	return c, nil
}
func (c *Catalog) saveLocked() error {
	b, e := json.MarshalIndent(struct {
		Items         []Imported `json:"items"`
		SteamOverride string     `json:"steamOverride"`
	}{c.Items, c.SteamOverride}, "", "  ")
	if e != nil {
		return e
	}
	tmp := filepath.Join(c.dir, "catalog.tmp")
	if e = os.WriteFile(tmp, b, 0600); e != nil {
		return e
	}
	// Windows cannot rename onto an existing target: use a backup-and-rollback strategy.
	target := filepath.Join(c.dir, "catalog.json")
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		return os.Rename(tmp, target)
	}
	previous := filepath.Join(c.dir, "catalog.previous")
	_ = os.Remove(previous)
	if err := os.Rename(target, previous); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Rename(previous, target)
		return err
	}
	_ = os.Remove(previous)
	return nil
}
func (c *Catalog) snapshot() ([]Imported, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]Imported{}, c.Items...)
	sort.Slice(out, func(i, j int) bool { return out[i].AddedAt > out[j].AddedAt })
	return out, c.SteamOverride
}
func (c *Catalog) setSteamPath(p string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	old := c.SteamOverride
	c.SteamOverride = p
	if err := c.saveLocked(); err != nil {
		c.SteamOverride = old
		return err
	}
	return nil
}

var idFilename = regexp.MustCompile(`(?i)^(?:appmanifest_)?(\d{1,12})(?:[_.-].*)?$`)

func importName(file string) string {
	// Forward and backslash separators are treated as untrusted and never preserved.
	file = strings.ReplaceAll(file, "\\", "/")
	return filepath.Base(file)
}
func inferredID(file string) string {
	base := strings.TrimSuffix(file, filepath.Ext(file))
	if m := idFilename.FindStringSubmatch(base); len(m) == 2 {
		return m[1]
	}
	return ""
}
func fileType(file string) string {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".acf":
		return "ACF"
	case ".lua":
		return "LUA"
	case ".manifest":
		return "MANIFEST"
	}
	return ""
}
func (c *Catalog) addFile(filename, source string, data []byte) (bool, error) {
	filename = importName(filename)
	typ := fileType(filename)
	if typ == "" {
		return false, fmt.Errorf("tipo não suportado: %s", filename)
	}
	if len(data) == 0 {
		return false, fmt.Errorf("arquivo vazio: %s", filename)
	}
	if len(data) > maxSingleFile {
		return false, fmt.Errorf("arquivo excede 16 MB: %s", filename)
	}
	item := Imported{Name: filename, Type: typ, AppID: inferredID(filename), Size: int64(len(data)), AddedAt: time.Now().UTC().Format(time.RFC3339), Source: source}
	// Steam depot manifests are not game licenses and a depot ID must never be
	// confused with a game's App ID. These records remain unassociated until
	// independently mapped using trustworthy Steam metadata.
	if typ == "MANIFEST" {
		item.DepotID = item.AppID
		item.AppID = ""
	}
	if typ == "ACF" {
		n, err := parseVDF(data)
		if err != nil {
			return false, fmt.Errorf("ACF inválido (%s): %w", filename, err)
		}
		state := n.obj("AppState")
		if state == nil {
			return false, fmt.Errorf("ACF sem AppState: %s", filename)
		}
		id := state.str("appid")
		if id == "" || !numericID(id) {
			return false, fmt.Errorf("ACF sem AppID válido: %s", filename)
		}
		item.AppID = id
		item.DisplayName = state.str("name")
		if m := appFileRE.FindStringSubmatch(filename); len(m) == 2 && m[1] != id {
			return false, fmt.Errorf("AppID do nome não corresponde ao ACF: %s", filename)
		}
	}
	sum := sha256.Sum256(data)
	item.Hash = hex.EncodeToString(sum[:])
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, it := range c.Items {
		if it.Hash == item.Hash {
			return false, nil
		}
	}
	name := item.Hash + strings.ToLower(filepath.Ext(filename))
	path := filepath.Join(c.dir, "imports", name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return false, err
	}
	c.Items = append(c.Items, item)
	if err := c.saveLocked(); err != nil {
		c.Items = c.Items[:len(c.Items)-1]
		_ = os.Remove(path)
		return false, err
	}
	return true, nil
}
func numericID(s string) bool {
	if s == "" || len(s) > 12 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

type ImportResult struct {
	Added      int      `json:"added"`
	Duplicates int      `json:"duplicates"`
	Skipped    int      `json:"skipped"`
	Errors     []string `json:"errors"`
}

func (r *ImportResult) err(e error) {
	if len(r.Errors) < 25 {
		r.Errors = append(r.Errors, e.Error())
	}
	r.Skipped++
}
func (c *Catalog) importMultipart(reader *multipart.Reader) ImportResult {
	result := ImportResult{Errors: []string{}}
	total := 0
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			result.err(err)
			break
		}
		if part.FormName() != "files" {
			part.Close()
			continue
		}
		total++
		if total > maxImportFiles {
			result.err(fmt.Errorf("limite de %d arquivos atingido", maxImportFiles))
			part.Close()
			break
		}
		original := importName(part.FileName())
		if original == "." || original == "" {
			part.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, maxSingleFile+1))
		part.Close()
		if err != nil {
			result.err(err)
			continue
		}
		if len(data) > maxSingleFile {
			result.err(fmt.Errorf("arquivo excede 16 MB: %s", original))
			continue
		}
		if strings.EqualFold(filepath.Ext(original), ".zip") {
			sub, err := c.importZip(original, data)
			if err != nil {
				result.err(err)
				continue
			}
			result.Added += sub.Added
			result.Duplicates += sub.Duplicates
			result.Skipped += sub.Skipped
			for _, e := range sub.Errors {
				if len(result.Errors) < 25 {
					result.Errors = append(result.Errors, e)
				}
			}
			continue
		}
		added, e := c.addFile(original, "Upload", data)
		if e != nil {
			result.err(e)
		} else if added {
			result.Added++
		} else {
			result.Duplicates++
		}
	}
	return result
}
func (c *Catalog) importZip(zipname string, data []byte) (ImportResult, error) {
	r := ImportResult{Errors: []string{}}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return r, fmt.Errorf("ZIP inválido: %s", zipname)
	}
	if len(z.File) > maxImportFiles {
		return r, fmt.Errorf("ZIP contém mais que %d entradas", maxImportFiles)
	}
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := importName(f.Name)
		if fileType(name) == "" {
			r.Skipped++
			continue
		}
		if f.UncompressedSize64 > maxZipEntry {
			r.err(fmt.Errorf("entrada ZIP excede 4 MB: %s", name))
			continue
		}
		stream, e := f.Open()
		if e != nil {
			r.err(e)
			continue
		}
		payload, e := io.ReadAll(io.LimitReader(stream, maxZipEntry+1))
		_ = stream.Close()
		if e != nil {
			r.err(e)
			continue
		}
		if len(payload) > maxZipEntry {
			r.err(fmt.Errorf("entrada ZIP muito grande: %s", name))
			continue
		}
		added, e := c.addFile(name, "ZIP: "+zipname, payload)
		if e != nil {
			r.err(e)
		} else if added {
			r.Added++
		} else {
			r.Duplicates++
		}
	}
	return r, nil
}
func (c *Catalog) backup() ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var buf bytes.Buffer
	out := zip.NewWriter(&buf)
	metadata, err := json.MarshalIndent(struct {
		Items         []Imported `json:"items"`
		SteamOverride string     `json:"steamOverride"`
	}{c.Items, c.SteamOverride}, "", "  ")
	if err != nil {
		return nil, err
	}
	w, err := out.Create("catalog.json")
	if err != nil {
		return nil, err
	}
	if _, err = w.Write(metadata); err != nil {
		return nil, err
	}
	for _, item := range c.Items {
		filename := item.Hash + strings.ToLower(filepath.Ext(item.Name))
		if len(item.Hash) != 64 || fileType(item.Name) == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.dir, "imports", filename))
		if err != nil {
			continue
		}
		w, err := out.Create("imports/" + filename)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write(data); err != nil {
			return nil, err
		}
	}
	if err := out.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
