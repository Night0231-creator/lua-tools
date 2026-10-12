package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strings"
)

type Release struct {
	Version   string `json:"version"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Notes     string `json:"notes"`
	Signature string `json:"signature"`
}

func main() {
	version := flag.String("version", "", "SemVer 0.0.0")
	asset := flag.String("url", "", "URL publica HTTPS do EXE")
	path := flag.String("file", "", "Caminho do EXE")
	notes := flag.String("notes", "Atualização estável Astronyx.", "Notas da versão")
	flag.Parse()
	u, e := url.Parse(*asset)
	if e != nil || u.Scheme != "https" || u.Host == "" {
		log.Fatal("URL HTTPS inválida")
	}
	if *version == "" || *path == "" || strings.ContainsAny(*version, "\n\r") || strings.Count(*version, ".") != 2 {
		log.Fatal("versão/caminho inválido")
	}
	keyBytes, e := base64.StdEncoding.DecodeString(os.Getenv("ASTRONYX_UPDATE_PRIVATE_KEY"))
	if e != nil || len(keyBytes) != ed25519.PrivateKeySize {
		log.Fatal("ASTRONYX_UPDATE_PRIVATE_KEY precisa de uma chave Ed25519 base64 (64 bytes)")
	}
	pubExpected := os.Getenv("UPDATE_PUBLIC_KEY")
	if pubExpected != "" && base64.StdEncoding.EncodeToString(ed25519.PrivateKey(keyBytes).Public().(ed25519.PublicKey)) != pubExpected {
		log.Fatal("UPDATE_PUBLIC_KEY não corresponde à chave privada; publicação cancelada")
	}
	file, e := os.Open(*path)
	if e != nil {
		log.Fatal(e)
	}
	defer file.Close()
	h := sha256.New()
	if _, e = io.Copy(h, file); e != nil {
		log.Fatal(e)
	}
	r := Release{Version: *version, URL: *asset, SHA256: hex.EncodeToString(h.Sum(nil)), Notes: *notes}
	msg := r.Version + "\n" + r.URL + "\n" + r.SHA256 + "\n" + r.Notes + "\n"
	r.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(keyBytes), []byte(msg)))
	out, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		log.Fatal(e)
	}
	fmt.Println(string(out))
}
