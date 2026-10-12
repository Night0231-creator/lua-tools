package main

import (
	"archive/zip"
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed assets/GameList-main.zip
var builtIn embed.FS

// StoreEntry is public metadata, NOT a license, depot download or executable.
type StoreEntry struct {
	ID          uint64 `json:"appid"`
	Name        string `json:"name"`
	Modified    int64  `json:"last_modified"`
	PriceChange int64  `json:"price_change_number"`
	Kind        string `json:"-"`
	searchable  string
}
type StoreCatalog struct {
	Entries      []StoreEntry
	ByID         map[uint64]int
	CountByKind  map[string]int
	TotalGameDLC int
}

func catalogFromZIP(b []byte) (*StoreCatalog, error) {
	if len(b) < 100 || len(b) > 45<<20 {
		return nil, errors.New("Tamanho de catálogo ZIP inválido")
	}
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, err
	}
	parts := map[string]string{"games.json": "game", "dlc.json": "dlc", "software.json": "software", "hardware.json": "hardware", "videos.json": "video"}
	out := &StoreCatalog{ByID: make(map[uint64]int, 260000), CountByKind: make(map[string]int)}
	found := map[string]bool{}
	for _, f := range z.File {
		name := strings.ToLower(filepath.Base(strings.ReplaceAll(f.Name, "\\", "/")))
		kind, ok := parts[name]
		if !ok {
			continue
		}
		if found[name] {
			return nil, fmt.Errorf("arquivo duplicado no ZIP: %s", name)
		}
		found[name] = true
		if f.UncompressedSize64 > 40<<20 {
			return nil, fmt.Errorf("JSON grande demais: %s", name)
		}
		r, e := f.Open()
		if e != nil {
			return nil, e
		}
		b, e := io.ReadAll(io.LimitReader(r, 40<<20+1))
		r.Close()
		if e != nil {
			return nil, e
		}
		if len(b) > 40<<20 {
			return nil, errors.New("Catálogo excede limite por arquivo")
		}
		var entries []StoreEntry
		if err := json.Unmarshal(b, &entries); err != nil {
			return nil, fmt.Errorf("JSON inválido %s: %w", name, err)
		}
		if len(entries) > 400000 {
			return nil, errors.New("Catálogo excede 400 mil registros")
		}
		for _, rec := range entries {
			if rec.ID == 0 {
				continue
			}
			if _, exists := out.ByID[rec.ID]; exists {
				return nil, fmt.Errorf("App ID repetido: %d", rec.ID)
			}
			rec.Kind = kind
			rec.Name = strings.TrimSpace(rec.Name)
			if rec.Name == "" {
				rec.Name = "Sem nome (App ID " + strconv.FormatUint(rec.ID, 10) + ")"
			}
			if len(rec.Name) > 400 {
				rec.Name = string([]rune(rec.Name)[:min(len([]rune(rec.Name)), 200)])
			}
			rec.searchable = strings.ToLower(rec.Name)
			out.ByID[rec.ID] = len(out.Entries)
			out.Entries = append(out.Entries, rec)
			out.CountByKind[kind]++
			if kind == "game" || kind == "dlc" {
				out.TotalGameDLC++
			}
		}
	}
	if !found["games.json"] || !found["dlc.json"] {
		return nil, errors.New("ZIP deve conter games.json e dlc.json")
	}
	if len(out.Entries) == 0 {
		return nil, errors.New("Catálogo vazio")
	}
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].ID < out.Entries[j].ID })
	for i, rec := range out.Entries {
		out.ByID[rec.ID] = i
	}
	return out, nil
}
func loadStoreCatalog(dir string) (*StoreCatalog, error) {
	external := filepath.Join(dir, "store-catalog.zip")
	if b, e := os.ReadFile(external); e == nil {
		if c, err := catalogFromZIP(b); err == nil {
			return c, nil
		}
	}
	b, e := builtIn.ReadFile("assets/GameList-main.zip")
	if e != nil {
		return nil, e
	}
	return catalogFromZIP(b)
}
func importStoreCatalog(dir, src string) (*StoreCatalog, error) {
	fi, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	if fi.Size() > 45<<20 {
		return nil, errors.New("ZIP grande demais (limite 45 MB)")
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	cat, err := catalogFromZIP(b)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(dir, ".store-*.zip")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	if _, err = tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return nil, err
	}
	if err = tmp.Close(); err != nil {
		os.Remove(tmpName)
		return nil, err
	}
	target := filepath.Join(dir, "store-catalog.zip")
	// Atomic-ish replacement with rollback on Windows.
	prev := target + ".previous"
	_ = os.Remove(prev)
	if _, err = os.Stat(target); err == nil {
		if err = os.Rename(target, prev); err != nil {
			os.Remove(tmpName)
			return nil, err
		}
	}
	if err = os.Rename(tmpName, target); err != nil {
		_ = os.Rename(prev, target)
		return nil, err
	}
	_ = os.Remove(prev)
	return cat, nil
}
func (c *StoreCatalog) Search(query, tab string, offset, limit int, selected func(StoreEntry) bool) ([]StoreEntry, int) {
	if offset < 0 {
		offset = 0
	}
	if limit < 1 {
		limit = 1
	}
	query = strings.ToLower(strings.TrimSpace(query))
	idQuery := strings.ReplaceAll(query, " ", "")
	results := make([]StoreEntry, 0, limit)
	count := 0
	for _, v := range c.Entries {
		if tab != "all" && tab != "selected" && v.Kind != tab {
			continue
		}
		if tab == "selected" && !selected(v) {
			continue
		}
		if query != "" && !strings.Contains(v.searchable, query) && !strings.Contains(strconv.FormatUint(v.ID, 10), idQuery) {
			continue
		}
		if count >= offset && len(results) < limit {
			results = append(results, v)
		}
		count++
	}
	return results, count
}

type Collection struct {
	mu          sync.Mutex
	dir         string
	AllGamesDLC bool            `json:"allGamesDLC"`
	Added       map[uint64]bool `json:"added"`
	Removed     map[uint64]bool `json:"removed"`
}

func loadCollection(dir string) (*Collection, error) {
	c := &Collection{dir: dir, Added: map[uint64]bool{}, Removed: map[uint64]bool{}}
	b, err := os.ReadFile(filepath.Join(dir, "collection.json"))
	if err == nil {
		if err = json.Unmarshal(b, c); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if c.Added == nil {
		c.Added = map[uint64]bool{}
	}
	if c.Removed == nil {
		c.Removed = map[uint64]bool{}
	}
	return c, nil
}
func (c *Collection) selected(v StoreEntry) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.selectedLocked(v)
}
func (c *Collection) selectedLocked(v StoreEntry) bool {
	if c.Removed[v.ID] {
		return false
	}
	return c.Added[v.ID] || (c.AllGamesDLC && (v.Kind == "game" || v.Kind == "dlc"))
}
func (c *Collection) writeLocked() error {
	b, err := json.MarshalIndent(struct {
		AllGamesDLC bool            `json:"allGamesDLC"`
		Added       map[uint64]bool `json:"added"`
		Removed     map[uint64]bool `json:"removed"`
	}{c.AllGamesDLC, c.Added, c.Removed}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.dir, ".collection-*.json")
	if err != nil {
		return err
	}
	path := tmp.Name()
	if _, err = tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(path)
		return err
	}
	if err = tmp.Close(); err != nil {
		os.Remove(path)
		return err
	}
	target := filepath.Join(c.dir, "collection.json")
	old := target + ".previous"
	_ = os.Remove(old)
	if _, e := os.Stat(target); e == nil {
		if err = os.Rename(target, old); err != nil {
			os.Remove(path)
			return err
		}
	}
	if err = os.Rename(path, target); err != nil {
		_ = os.Rename(old, target)
		return err
	}
	_ = os.Remove(old)
	return nil
}

// AddAllGamesDLC marks the user's local collection; it does not touch Steam.
func (c *Collection) AddAllGamesDLC() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	oldAll, oldAdded, oldRemoved := c.AllGamesDLC, c.Added, c.Removed
	c.AllGamesDLC = true
	c.Added = map[uint64]bool{}
	c.Removed = map[uint64]bool{}
	if err := c.writeLocked(); err != nil {
		c.AllGamesDLC = oldAll
		c.Added = oldAdded
		c.Removed = oldRemoved
		return err
	}
	return nil
}
func (c *Collection) Toggle(v StoreEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	previously := c.selectedLocked(v)
	oldAdded, oldRemoved := c.Added[v.ID], c.Removed[v.ID]
	if previously {
		delete(c.Added, v.ID)
		c.Removed[v.ID] = true
	} else {
		c.Added[v.ID] = true
		delete(c.Removed, v.ID)
	}
	if err := c.writeLocked(); err != nil {
		if oldAdded {
			c.Added[v.ID] = true
		} else {
			delete(c.Added, v.ID)
		}
		if oldRemoved {
			c.Removed[v.ID] = true
		} else {
			delete(c.Removed, v.ID)
		}
		return err
	}
	return nil
}
func (c *Collection) Count(store *StoreCatalog) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, v := range store.Entries {
		if c.selectedLocked(v) {
			count++
		}
	}
	return count
}
