package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// This subsystem only copies files into Astronyx's own data directory.
// It NEVER writes to Steam, installs a plug-in, executes Lua or grants licenses.
type LocalPluginFile struct {
	Name  string
	AppID string
	Size  int64
}
type LocalPluginScan struct {
	Directory string
	Files     []LocalPluginFile
}

var localLuaName = regexp.MustCompile(`^\d{1,12}\.(?i:lua|manifest)$`)

func scanPluginDir(dir string) (LocalPluginScan, error) {
	out := LocalPluginScan{Directory: dir, Files: []LocalPluginFile{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out, err
	}
	for _, e := range entries {
		if !localLuaName.MatchString(e.Name()) || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() <= 0 || info.Size() > maxSingleFile {
			continue
		}
		out.Files = append(out.Files, LocalPluginFile{Name: e.Name(), AppID: inferredID(e.Name()), Size: info.Size()})
	}
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Name < out.Files[j].Name })
	return out, nil
}
func scanLocalPlugin(override string) (LocalPluginScan, error) {
	steamRoot := detectSteam(override)
	if steamRoot == "" {
		return LocalPluginScan{}, errors.New("Steam não encontrada; confirme a instalação no Windows")
	}
	return scanPluginDir(filepath.Join(steamRoot, "config", "stplug-in"))
}

type BulkProgress struct{ Done, Total, Added, Duplicates, Skipped int }
type BulkSaver struct {
	c            *Catalog
	known        map[string]bool
	pending      []string
	savedItems   int
	pendingCount int
}

func newBulkSaver(c *Catalog) *BulkSaver {
	c.mu.Lock()
	defer c.mu.Unlock()
	known := make(map[string]bool, len(c.Items)+500)
	for _, item := range c.Items {
		known[item.Hash] = true
	}
	return &BulkSaver{c: c, known: known, savedItems: len(c.Items)}
}
func (s *BulkSaver) add(name, source string, raw []byte) (bool, error) {
	if fileType(name) != "LUA" && fileType(name) != "MANIFEST" {
		return false, errors.New("somente arquivos Lua e manifest são aceitos")
	}
	if !localLuaName.MatchString(name) {
		return false, errors.New("nome inválido: esperado 123456.lua ou 123456.manifest")
	}
	if len(raw) == 0 || len(raw) > maxSingleFile {
		return false, errors.New("tamanho de arquivo inválido")
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.known[hash] {
		return false, nil
	}
	path := filepath.Join(s.c.dir, "imports", hash+strings.ToLower(filepath.Ext(name)))
	if err := os.WriteFile(path, raw, 0600); err != nil {
		return false, err
	}
	item := Imported{Name: name, Type: fileType(name), Size: int64(len(raw)), Hash: hash, AppID: inferredID(name), AddedAt: time.Now().UTC().Format(time.RFC3339), Source: source}
	if item.Type == "MANIFEST" {
		item.DepotID = item.AppID
		item.AppID = ""
	}
	s.c.Items = append(s.c.Items, item)
	s.known[hash] = true
	s.pending = append(s.pending, path)
	s.pendingCount++
	if s.pendingCount >= 100 {
		return true, s.flushLocked()
	}
	return true, nil
}
func (s *BulkSaver) flushLocked() error {
	if s.pendingCount == 0 {
		return nil
	}
	if err := s.c.saveLocked(); err != nil {
		for _, f := range s.pending {
			_ = os.Remove(f)
		}
		for _, item := range s.c.Items[s.savedItems:] {
			delete(s.known, item.Hash)
		}
		s.c.Items = s.c.Items[:s.savedItems]
		s.pending = nil
		s.pendingCount = 0
		return err
	}
	s.savedItems = len(s.c.Items)
	s.pending = nil
	s.pendingCount = 0
	return nil
}
func (s *BulkSaver) close() error {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	return s.flushLocked()
}
func backupLocalPlugin(ctx context.Context, c *Catalog, scan LocalPluginScan, progress func(BulkProgress)) (BulkProgress, error) {
	r := BulkProgress{Total: len(scan.Files)}
	saver := newBulkSaver(c)
	for _, item := range scan.Files {
		if err := ctx.Err(); err != nil {
			return r, errors.Join(err, saver.close())
		}
		path := filepath.Join(scan.Directory, item.Name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxSingleFile || info.Size() <= 0 {
			r.Skipped++
			r.Done++
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			r.Skipped++
			r.Done++
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(file, maxSingleFile+1))
		file.Close()
		if err != nil || len(raw) > maxSingleFile {
			r.Skipped++
			r.Done++
			continue
		}
		added, err := saver.add(item.Name, "Backup somente leitura (Steam stplug-in)", raw)
		if err != nil {
			r.Skipped++
		} else if added {
			r.Added++
		} else {
			r.Duplicates++
		}
		r.Done++
		if progress != nil && (r.Done%20 == 0 || r.Done == r.Total) {
			progress(r)
		}
	}
	return r, saver.close()
}

// Users may import a locally authored list of HTTPS files for which they have
// distribution rights. This is NOT a connector to LuaTools, Ryuu, Luie or DRM bypasses.
type DownloadItem struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}
type AuthorizedIndex struct {
	Format     string         `json:"format"`
	Source     string         `json:"source"`
	Authorized bool           `json:"authorized"`
	Files      []DownloadItem `json:"files"`
}

var hexSHA256 = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

func validDownloadURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return errors.New("é obrigatório HTTPS válido sem credenciais")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.Contains(host, "\\") {
		return errors.New("host local não permitido")
	}
	if ip := net.ParseIP(host); ip != nil {
		return errors.New("endereços IP diretos não são aceitos")
	}
	if u.Port() != "" && u.Port() != "443" {
		return errors.New("somente a porta HTTPS padrão é permitida")
	}
	return nil
}
func parseAuthorizedIndex(data []byte) (AuthorizedIndex, error) {
	var idx AuthorizedIndex
	if len(data) > 15<<20 {
		return idx, errors.New("índice muito grande")
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		return idx, fmt.Errorf("JSON inválido: %w", err)
	}
	if idx.Format != "astronyx-manifests-v1" || !idx.Authorized || strings.TrimSpace(idx.Source) == "" {
		return idx, errors.New("índice exige formato correto, fonte e confirmação de autorização")
	}
	if len(idx.Files) == 0 || len(idx.Files) > 60000 {
		return idx, errors.New("índice deve conter entre 1 e 60.000 arquivos")
	}
	names := map[string]bool{}
	for _, item := range idx.Files {
		if !localLuaName.MatchString(item.Name) || filepath.Base(item.Name) != item.Name || strings.ContainsAny(item.Name, "/\\") {
			return idx, fmt.Errorf("nome inválido: %q", item.Name)
		}
		if !hexSHA256.MatchString(item.SHA256) {
			return idx, fmt.Errorf("SHA-256 inválido em %s", item.Name)
		}
		if err := validDownloadURL(item.URL); err != nil {
			return idx, fmt.Errorf("URL inválida em %s: %w", item.Name, err)
		}
		k := strings.ToLower(item.Name)
		if names[k] {
			return idx, fmt.Errorf("nome duplicado: %s", item.Name)
		}
		names[k] = true
	}
	return idx, nil
}
func newManifestHTTPClient() *http.Client {
	return &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errors.New("redirecionamentos demais")
		}
		if err := validDownloadURL(req.URL.String()); err != nil {
			return err
		}
		if req.URL.Hostname() != via[0].URL.Hostname() {
			return errors.New("redirecionamento para outro domínio bloqueado")
		}
		return nil
	}}
}
func downloadAuthorized(ctx context.Context, client *http.Client, c *Catalog, idx AuthorizedIndex, progress func(BulkProgress)) (BulkProgress, error) {
	r := BulkProgress{Total: len(idx.Files)}
	saver := newBulkSaver(c)
	var totalBytes int64
	for _, item := range idx.Files {
		if err := ctx.Err(); err != nil {
			return r, errors.Join(err, saver.close())
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
		if err != nil {
			r.Skipped++
			r.Done++
			continue
		}
		request.Header.Set("User-Agent", "AstronyxSteamManager/2.4 (authorized-file-import)")
		resp, err := client.Do(request)
		var data []byte
		if err == nil {
			if resp.StatusCode != http.StatusOK {
				err = fmt.Errorf("HTTP %d", resp.StatusCode)
			} else {
				data, err = io.ReadAll(io.LimitReader(resp.Body, maxSingleFile+1))
			}
			resp.Body.Close()
		}
		if err != nil || len(data) == 0 || len(data) > maxSingleFile {
			r.Skipped++
			r.Done++
			continue
		}
		totalBytes += int64(len(data))
		if totalBytes > 1<<30 {
			return r, errors.Join(errors.New("limite de 1 GB por execução"), saver.close())
		}
		sum := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), item.SHA256) {
			r.Skipped++
			r.Done++
			continue
		}
		added, err := saver.add(item.Name, "HTTPS autorizado: "+idx.Source, data)
		if err != nil {
			r.Skipped++
		} else if added {
			r.Added++
		} else {
			r.Duplicates++
		}
		r.Done++
		if progress != nil && (r.Done%10 == 0 || r.Done == r.Total) {
			progress(r)
		}
	}
	return r, saver.close()
}

type AsyncManifestReport struct {
	sync.Mutex
	Scan   LocalPluginScan
	Index  AuthorizedIndex
	Report BulkProgress
	Err    error
	Action string
}
