package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Inject these at build time using -ldflags. Never put private keys in the application.
var (
	appVersion      = "2.2.0"
	updateFeedURL   = ""
	updatePublicKey = ""
)

type Release struct {
	Version   string `json:"version"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Notes     string `json:"notes"`
	Signature string `json:"signature"`
}
type UpdateStatus struct {
	Current    string `json:"current"`
	Configured bool   `json:"configured"`
	Available  bool   `json:"available"`
	Latest     string `json:"latest,omitempty"`
	Notes      string `json:"notes,omitempty"`
	Error      string `json:"error,omitempty"`
}
type Updater struct {
	mu     sync.Mutex
	last   time.Time
	cached Release
	status UpdateStatus
}

func newUpdater() *Updater { return &Updater{status: UpdateStatus{Current: appVersion}} }
func releaseMessage(r Release) string {
	return r.Version + "\n" + r.URL + "\n" + r.SHA256 + "\n" + r.Notes + "\n"
}
func secureHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == ""
}
func parseVersion(s string) ([3]int, error) {
	var nums [3]int
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return nums, errors.New("versão deve ser MAJOR.MINOR.PATCH")
	}
	for i, p := range parts {
		if p == "" || len(p) > 8 {
			return nums, errors.New("versão inválida")
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return nums, errors.New("versão inválida")
			}
		}
		n, e := strconv.Atoi(p)
		if e != nil {
			return nums, e
		}
		nums[i] = n
	}
	return nums, nil
}
func newer(a, b string) bool {
	x, err := parseVersion(a)
	if err != nil {
		return false
	}
	y, err := parseVersion(b)
	if err != nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if x[i] > y[i] {
			return true
		}
		if x[i] < y[i] {
			return false
		}
	}
	return false
}
func verifyRelease(r Release) error {
	if _, e := parseVersion(r.Version); e != nil {
		return e
	}
	if !secureHTTPURL(r.URL) {
		return errors.New("URL do executável deve ser HTTPS")
	}
	if len(r.Notes) > 6000 {
		return errors.New("notas muito longas")
	}
	if len(r.SHA256) != 64 {
		return errors.New("hash SHA-256 inválido")
	}
	if _, err := hex.DecodeString(r.SHA256); err != nil {
		return errors.New("hash SHA-256 inválido")
	}
	key, err := base64.StdEncoding.DecodeString(updatePublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return errors.New("chave pública de atualizações não configurada")
	}
	sig, err := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(ed25519.PublicKey(key), []byte(releaseMessage(r)), sig) {
		return errors.New("assinatura digital da atualização inválida")
	}
	return nil
}

var updateClient = &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 || !secureHTTPURL(req.URL.String()) {
		return errors.New("redirecionamento de atualização inválido")
	}
	return nil
}}

func fetchRelease(ctx context.Context) (Release, error) {
	var rel Release
	if !secureHTTPURL(updateFeedURL) {
		return rel, errors.New("feed HTTPS não configurado")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, updateFeedURL, nil)
	if err != nil {
		return rel, err
	}
	response, err := updateClient.Do(request)
	if err != nil {
		return rel, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return rel, fmt.Errorf("feed retornou HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 32*1024))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&rel); e != nil {
		return rel, e
	}
	if e := verifyRelease(rel); e != nil {
		return rel, e
	}
	return rel, nil
}
func (u *Updater) check(ctx context.Context, force bool) UpdateStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !force && !u.last.IsZero() && time.Since(u.last) < 45*time.Second {
		return u.status
	}
	u.last = time.Now()
	state := UpdateStatus{Current: appVersion, Configured: secureHTTPURL(updateFeedURL) && updatePublicKey != ""}
	if !state.Configured {
		state.Error = "Canal de atualização ainda não publicado"
		u.status = state
		return state
	}
	release, err := fetchRelease(ctx)
	if err != nil {
		state.Error = "Verificação indisponível: " + err.Error()
		u.status = state
		return state
	}
	state.Latest = release.Version
	state.Available = newer(release.Version, appVersion)
	state.Notes = release.Notes
	u.cached = release
	u.status = state
	return state
}
func downloadUpdate(ctx context.Context, r Release) (string, error) {
	if err := verifyRelease(r); err != nil {
		return "", err
	}
	// Downloads may take longer than the metadata check on slower connections.
	downloadClient := &http.Client{Timeout: 5 * time.Minute, CheckRedirect: updateClient.CheckRedirect}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if e != nil {
		return "", e
	}
	resp, e := downloadClient.Do(req)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("servidor retornou HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > 200<<20 {
		return "", errors.New("arquivo acima de 200 MB")
	}
	folder, e := os.MkdirTemp("", "astronyx-update-*")
	if e != nil {
		return "", e
	}
	file := filepath.Join(folder, "Astronyx-Steam-Manager-new.exe")
	f, e := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if e != nil {
		os.RemoveAll(folder)
		return "", e
	}
	hash := sha256.New()
	n, cpErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, 200<<20+1))
	closeErr := f.Close()
	if cpErr != nil || closeErr != nil || n > 200<<20 {
		os.RemoveAll(folder)
		return "", errors.New("download incompleto ou muito grande")
	}
	expected, _ := hex.DecodeString(r.SHA256)
	if subtle.ConstantTimeCompare(hash.Sum(nil), expected) != 1 {
		os.RemoveAll(folder)
		return "", errors.New("SHA-256 do download não confere")
	}
	check, e := os.Open(file)
	if e != nil {
		os.RemoveAll(folder)
		return "", e
	}
	var magic [2]byte
	_, e = io.ReadFull(check, magic[:])
	check.Close()
	if e != nil || string(magic[:]) != "MZ" {
		os.RemoveAll(folder)
		return "", errors.New("arquivo de atualização não parece executável Windows")
	}
	return file, nil
}

// A copied updater helper runs after the main executable exits. This avoids replacing
// a running Windows EXE; the user's private app data and backups are not touched.
func launchInstaller(staged string, sha string) error {
	if runtime.GOOS != "windows" {
		return errors.New("instalação automática disponível somente no Windows")
	}
	target, err := os.Executable()
	if err != nil {
		return err
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Ext(target), ".exe") {
		return errors.New("executável original inválido")
	}
	helper := filepath.Join(filepath.Dir(staged), "Astronyx-Updater-Helper.exe")
	input, err := os.Open(target)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(helper, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	_, err = io.Copy(output, input)
	closeErr := output.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	cmd := exec.Command(helper, "--apply-update", target, staged, sha)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
func applyUpdateHelper(args []string) error {
	if runtime.GOOS != "windows" {
		return errors.New("helper somente Windows")
	}
	if len(args) != 3 {
		return errors.New("parâmetros inválidos")
	}
	target, staged, checksum := args[0], args[1], args[2]
	if !filepath.IsAbs(target) || !filepath.IsAbs(staged) || len(checksum) != 64 || !strings.EqualFold(filepath.Ext(target), ".exe") {
		return errors.New("parâmetros inválidos")
	}
	expected, err := hex.DecodeString(checksum)
	if err != nil {
		return err
	}
	f, err := os.Open(staged)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, err = io.Copy(hash, io.LimitReader(f, 200<<20+1))
	f.Close()
	if err != nil || subtle.ConstantTimeCompare(hash.Sum(nil), expected) != 1 {
		return errors.New("arquivo temporário falhou na verificação")
	}
	// Stage on the same volume as the target to make the final rename reliable.
	candidate, err := os.CreateTemp(filepath.Dir(target), ".astronyx-next-*.exe")
	if err != nil {
		return fmt.Errorf("sem permissão de escrita na pasta do programa: %w", err)
	}
	candidatePath := candidate.Name()
	source, err := os.Open(staged)
	if err != nil {
		candidate.Close()
		os.Remove(candidatePath)
		return err
	}
	_, err = io.Copy(candidate, source)
	source.Close()
	closeErr := candidate.Close()
	if err != nil || closeErr != nil {
		os.Remove(candidatePath)
		return errors.New("não foi possível preparar a atualização")
	}
	backup := target + ".previous"
	var renameErr error
	for i := 0; i < 90; i++ {
		// A leftover backup is preserved for manual recovery.
		if _, e := os.Stat(backup); e == nil {
			os.Remove(candidatePath)
			return errors.New("existe uma versão anterior .previous; confira antes de atualizar")
		}
		renameErr = os.Rename(target, backup)
		if renameErr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if renameErr != nil {
		os.Remove(candidatePath)
		return fmt.Errorf("não foi possível substituir a versão atual: %w", renameErr)
	}
	if err := os.Rename(candidatePath, target); err != nil {
		_ = os.Rename(backup, target)
		_ = os.Remove(candidatePath)
		return fmt.Errorf("instalação falhou; versão antiga restaurada: %w", err)
	}
	cmd := exec.Command(target)
	if err := cmd.Start(); err != nil {
		_ = os.Remove(target)
		_ = os.Rename(backup, target)
		return fmt.Errorf("falha ao abrir atualização: %w", err)
	}
	_ = cmd.Process.Release()
	// Only delete the previous executable after a successful launch.
	_ = os.Remove(backup)
	return nil
}
