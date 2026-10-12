package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type updateChannel struct {
	FeedURL   string `json:"feedUrl"`
	PublicKey string `json:"publicKey"`
}

func updateChannelPath(dir string) string { return filepath.Join(dir, "update-channel.json") }
func validChannel(v updateChannel) error {
	if !secureHTTPURL(v.FeedURL) {
		return errors.New("feed deve usar HTTPS")
	}
	b, e := base64.StdEncoding.DecodeString(v.PublicKey)
	if e != nil || len(b) != ed25519.PublicKeySize {
		return errors.New("chave Ed25519 pública inválida")
	}
	return nil
}
func loadChannel(dir string) error {
	if updateFeedURL != "" || updatePublicKey != "" {
		return nil
	} // Immutable embedded configuration wins.
	b, e := os.ReadFile(updateChannelPath(dir))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	var v updateChannel
	if e := json.Unmarshal(b, &v); e != nil {
		return e
	}
	if e := validChannel(v); e != nil {
		return e
	}
	updateFeedURL = v.FeedURL
	updatePublicKey = v.PublicKey
	return nil
}
func saveChannel(dir string, v updateChannel) error {
	if e := validChannel(v); e != nil {
		return e
	}
	if updateFeedURL != "" && updatePublicKey != "" {
		return errors.New("canal já incorporado ao executável; altere na compilação")
	}
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	tmp, e := os.CreateTemp(dir, ".update-channel-*.json")
	if e != nil {
		return e
	}
	path := tmp.Name()
	if _, e = tmp.Write(b); e != nil {
		tmp.Close()
		os.Remove(path)
		return e
	}
	if e = tmp.Close(); e != nil {
		os.Remove(path)
		return e
	}
	if e = os.Rename(path, updateChannelPath(dir)); e != nil {
		os.Remove(path)
		return e
	}
	return nil
}
