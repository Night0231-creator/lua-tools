package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestVersionComparison(t *testing.T) {
	cases := []struct {
		a, b    string
		greater bool
	}{
		{"2.0.1", "2.0.0", true}, {"2.1.0", "2.0.9", true}, {"1.9.9", "2.0.0", false}, {"2.0.0", "2.0.0", false}, {"../../etc", "2.0.0", false}, {"v3.0.0", "2.99.99", true},
	}
	for _, c := range cases {
		if got := newer(c.a, c.b); got != c.greater {
			t.Errorf("newer(%q,%q)=%v; want %v", c.a, c.b, got, c.greater)
		}
	}
}
func TestSignatureAndTamperProtection(t *testing.T) {
	public, private, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	previous := updatePublicKey
	updatePublicKey = base64.StdEncoding.EncodeToString(public)
	defer func() { updatePublicKey = previous }()
	r := Release{Version: "2.1.0", URL: "https://example.org/release.exe", SHA256: hex.EncodeToString(make([]byte, 32)), Notes: "Atualização segura"}
	r.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte(releaseMessage(r))))
	if e := verifyRelease(r); e != nil {
		t.Fatalf("assinatura válida rejeitada: %v", e)
	}
	r.Notes = "conteúdo adulterado"
	if e := verifyRelease(r); e == nil || !strings.Contains(e.Error(), "assinatura") {
		t.Fatalf("assinatura adulterada aceita: %v", e)
	}
	r.Notes = "Atualização segura"
	r.URL = "http://example.org/release.exe"
	if e := verifyRelease(r); e == nil {
		t.Fatal("url http aceita")
	}
}
func TestConfigValidation(t *testing.T) {
	if validChannel(updateChannel{FeedURL: "http://localhost:1111/a", PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32))}) == nil {
		t.Fatal("HTTP inseguro aceito")
	}
	if validChannel(updateChannel{FeedURL: "https://example.org/latest.json", PublicKey: "test"}) == nil {
		t.Fatal("chave pública inválida aceita")
	}
}
