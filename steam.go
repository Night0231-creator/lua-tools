package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The app only reads Steam metadata. It does not change Steam files, entitlements or DRM.
type Game struct {
	AppID      string `json:"appId"`
	Name       string `json:"name"`
	InstallDir string `json:"installDir"`
	Library    string `json:"library"`
	Installed  bool   `json:"installed"`
	State      string `json:"state"`
	Size       int64  `json:"size"`
}
type steamScan struct {
	Root      string   `json:"root"`
	Libraries []string `json:"libraries"`
	Games     []Game   `json:"games"`
	Warning   string   `json:"warning,omitempty"`
}

type vdfNode map[string]any

func (n vdfNode) get(key string) any {
	for k, v := range n {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return nil
}
func (n vdfNode) str(key string) string {
	v, ok := n.get(key).(string)
	if ok {
		return v
	}
	return ""
}
func (n vdfNode) obj(key string) vdfNode {
	v, ok := n.get(key).(vdfNode)
	if ok {
		return v
	}
	return nil
}

type lexer struct {
	src []byte
	i   int
}

func (l *lexer) token() (string, bool, error) {
	for l.i < len(l.src) {
		c := l.src[l.i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == 0xef || c == 0xbb || c == 0xbf {
			l.i++
			continue
		}
		if c == '/' && l.i+1 < len(l.src) && l.src[l.i+1] == '/' {
			for l.i < len(l.src) && l.src[l.i] != '\n' {
				l.i++
			}
			continue
		}
		if c == '{' || c == '}' {
			l.i++
			return string(c), true, nil
		}
		if c == '"' {
			l.i++
			var b strings.Builder
			for l.i < len(l.src) {
				ch := l.src[l.i]
				l.i++
				if ch == '"' {
					return b.String(), true, nil
				}
				if ch == '\\' && l.i < len(l.src) {
					n := l.src[l.i]
					if n == '\\' || n == '"' {
						b.WriteByte(n)
						l.i++
						continue
					}
				}
				b.WriteByte(ch)
			}
			return "", false, fmt.Errorf("string VDF sem aspas de fechamento")
		}
		start := l.i
		for l.i < len(l.src) && !strings.ContainsRune(" \t\r\n{}", rune(l.src[l.i])) {
			l.i++
		}
		if l.i == start {
			l.i++
			continue
		}
		return string(l.src[start:l.i]), true, nil
	}
	return "", false, nil
}
func parseVDF(b []byte) (vdfNode, error) {
	l := &lexer{src: b}
	count := 0
	var readObj func(depth int, stop bool) (vdfNode, error)
	readObj = func(depth int, stop bool) (vdfNode, error) {
		if depth > 30 {
			return nil, fmt.Errorf("VDF muito profundo")
		}
		m := vdfNode{}
		for {
			key, ok, err := l.token()
			if err != nil {
				return nil, err
			}
			if !ok {
				if stop {
					return nil, fmt.Errorf("VDF incompleto")
				}
				return m, nil
			}
			if key == "}" {
				if !stop {
					return nil, fmt.Errorf("VDF com fechamento extra")
				}
				return m, nil
			}
			if key == "{" {
				return nil, fmt.Errorf("VDF inválido")
			}
			count++
			if count > 50000 {
				return nil, fmt.Errorf("VDF muito grande")
			}
			val, ok, err := l.token()
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("VDF sem valor")
			}
			if val == "{" {
				sub, err := readObj(depth+1, true)
				if err != nil {
					return nil, err
				}
				m[key] = sub
			} else if val == "}" {
				return nil, fmt.Errorf("VDF sem valor para chave")
			} else {
				m[key] = val
			}
		}
	}
	return readObj(0, false)
}
func readVDF(path string) (vdfNode, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) > 8<<20 {
		return nil, fmt.Errorf("VDF excede 8 MB")
	}
	return parseVDF(b)
}

var regExp = regexp.MustCompile(`(?i)\s+REG_\w+\s+(.+)$`)

func fromRegistry() string {
	checks := [][3]string{
		{`HKCU\Software\Valve\Steam`, `SteamPath`, `/reg:64`},
		{`HKCU\Software\Valve\Steam`, `SteamPath`, `/reg:32`},
		{`HKLM\SOFTWARE\Valve\Steam`, `InstallPath`, `/reg:32`},
		{`HKLM\SOFTWARE\Valve\Steam`, `InstallPath`, `/reg:64`},
	}
	for _, q := range checks {
		b, e := exec.Command("reg.exe", "query", q[0], "/v", q[1], q[2]).CombinedOutput()
		if e != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if m := regExp.FindStringSubmatch(line); len(m) == 2 {
				p := strings.TrimSpace(m[1])
				if fileExists(filepath.Join(p, "steam.exe")) {
					return p
				}
			}
		}
	}
	return ""
}
func fileExists(path string) bool { s, e := os.Stat(path); return e == nil && !s.IsDir() }
func dirExists(path string) bool  { s, e := os.Stat(path); return e == nil && s.IsDir() }
func detectSteam(override string) string {
	paths := []string{}
	if override != "" {
		paths = append(paths, override)
	}
	if p := os.Getenv("STEAM_PATH"); p != "" {
		paths = append(paths, p)
	}
	if p := fromRegistry(); p != "" {
		paths = append(paths, p)
	}
	if p := os.Getenv("ProgramFiles(x86)"); p != "" {
		paths = append(paths, filepath.Join(p, "Steam"))
	}
	if p := os.Getenv("ProgramFiles"); p != "" {
		paths = append(paths, filepath.Join(p, "Steam"))
	}
	paths = append(paths, `C:\Program Files (x86)\Steam`, `C:\Program Files\Steam`)
	for _, p := range paths {
		if fileExists(filepath.Join(p, "steam.exe")) && dirExists(filepath.Join(p, "steamapps")) {
			return filepath.Clean(p)
		}
	}
	return ""
}
func getLibraries(root string) []string {
	if root == "" {
		return nil
	}
	paths := []string{root}
	for _, candidate := range []string{filepath.Join(root, "steamapps", "libraryfolders.vdf"), filepath.Join(root, "config", "libraryfolders.vdf")} {
		n, err := readVDF(candidate)
		if err != nil {
			continue
		}
		lf := n.obj("libraryfolders")
		if lf == nil {
			continue
		}
		for k, v := range lf {
			if _, err := strconv.Atoi(k); err != nil {
				continue
			}
			p := ""
			if node, ok := v.(vdfNode); ok {
				p = node.str("path")
			}
			if s, ok := v.(string); ok {
				p = s
			}
			if p != "" {
				paths = append(paths, p)
			}
		}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, p := range paths {
		p = filepath.Clean(strings.TrimSpace(p))
		key := strings.ToLower(p)
		if !seen[key] && dirExists(filepath.Join(p, "steamapps")) {
			seen[key] = true
			out = append(out, p)
		}
	}
	return out
}

var appFileRE = regexp.MustCompile(`(?i)^appmanifest_([0-9]+)\.acf$`)

func scanSteam(override string) steamScan {
	root := detectSteam(override)
	r := steamScan{Root: root, Libraries: []string{}, Games: []Game{}}
	if root == "" {
		r.Warning = "Steam não encontrada. Configure a pasta da Steam para localizar jogos instalados."
		return r
	}
	r.Libraries = getLibraries(root)
	for _, lib := range r.Libraries {
		entries, err := os.ReadDir(filepath.Join(lib, "steamapps"))
		if err != nil {
			continue
		}
		for _, f := range entries {
			if f.IsDir() {
				continue
			}
			m := appFileRE.FindStringSubmatch(f.Name())
			if len(m) != 2 {
				continue
			}
			n, err := readVDF(filepath.Join(lib, "steamapps", f.Name()))
			if err != nil {
				continue
			}
			state := n.obj("AppState")
			if state == nil {
				continue
			}
			id := state.str("appid")
			if id == "" {
				id = m[1]
			}
			if id != m[1] {
				continue
			}
			name := strings.TrimSpace(state.str("name"))
			if name == "" {
				name = "App " + id
			}
			folder := state.str("installdir")
			goodFolder := folder != "" && folder != "." && folder != ".." && filepath.Base(folder) == folder && !strings.ContainsAny(folder, `/\:`)
			install := ""
			installed := false
			if goodFolder {
				install = filepath.Join(lib, "steamapps", "common", folder)
				installed = dirExists(install)
			}
			size, _ := strconv.ParseInt(state.str("SizeOnDisk"), 10, 64)
			flag, _ := strconv.Atoi(state.str("StateFlags"))
			status := "Arquivos ausentes"
			if installed {
				status = "Localizado"
				if flag&4 == 0 {
					status = "Instalação pendente"
				}
			}
			r.Games = append(r.Games, Game{AppID: id, Name: name, InstallDir: install, Library: lib, Installed: installed, State: status, Size: size})
		}
	}
	sort.Slice(r.Games, func(i, j int) bool { return strings.ToLower(r.Games[i].Name) < strings.ToLower(r.Games[j].Name) })
	return r
}
