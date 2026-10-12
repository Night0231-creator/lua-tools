package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const steamStoreAPI = "https://store.steampowered.com/api/appdetails"

type StoreInspection struct {
	AppID               string   `json:"appId"`
	Name                string   `json:"name"`
	Type                string   `json:"type"`
	IsFree              bool     `json:"isFree"`
	DLCIDs              []string `json:"dlcIds"`
	StoreURL            string   `json:"storeUrl"`
	LocalInstalled      bool     `json:"localInstalled"`
	ImportedManifests   int      `json:"importedManifests"`
	LicenseStatus       string   `json:"licenseStatus"`
	DownloadMode        string   `json:"downloadMode"`
	DownloadExplanation string   `json:"downloadExplanation"`
}

// steamStoreMetadata only reads public store information. The Steam storefront
// does not attest account ownership and must never be used as proof of license.
func steamStoreMetadata(ctx context.Context, client *http.Client, baseURL, appID string) (StoreInspection, error) {
	if !numericID(appID) {
		return StoreInspection{}, errors.New("App ID inválido")
	}
	endpoint, err := url.Parse(baseURL)
	if err != nil {
		return StoreInspection{}, err
	}
	q := endpoint.Query()
	q.Set("appids", appID)
	q.Set("cc", "br")
	q.Set("l", "brazilian")
	endpoint.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return StoreInspection{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "AstronyxSteamManager/2.2 (+public-metadata-only)")
	resp, err := client.Do(req)
	if err != nil {
		return StoreInspection{}, fmt.Errorf("Steam Store indisponível: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return StoreInspection{}, fmt.Errorf("Steam Store respondeu HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if err != nil {
		return StoreInspection{}, err
	}
	if len(raw) > 2<<20 {
		return StoreInspection{}, errors.New("resposta Steam muito grande")
	}
	var result map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			SteamAppID json.Number `json:"steam_appid"`
			Name       string      `json:"name"`
			Type       string      `json:"type"`
			IsFree     bool        `json:"is_free"`
			DLC        []int64     `json:"dlc"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return StoreInspection{}, fmt.Errorf("resposta Steam inválida: %w", err)
	}
	entry, ok := result[appID]
	if !ok || !entry.Success || entry.Data.Name == "" {
		return StoreInspection{}, errors.New("App ID não encontrado na vitrine pública ou dados indisponíveis")
	}
	out := StoreInspection{
		AppID: appID, Name: entry.Data.Name, Type: entry.Data.Type,
		IsFree: entry.Data.IsFree, DLCIDs: []string{},
		StoreURL:            "https://store.steampowered.com/app/" + appID + "/",
		LicenseStatus:       "Não verificada (a loja pública não confirma posse da conta)",
		DownloadMode:        "steam_client_only",
		DownloadExplanation: "A instalação e o acesso a DLCs dependem da Steam e das licenças da conta. Nenhum arquivo será baixado ou desbloqueado por manifests.",
	}
	for _, dlc := range entry.Data.DLC {
		if dlc > 0 {
			out.DLCIDs = append(out.DLCIDs, fmt.Sprint(dlc))
		}
		if len(out.DLCIDs) >= 100 {
			break
		}
	}
	if entry.Data.IsFree && (strings.EqualFold(entry.Data.Type, "game") || strings.EqualFold(entry.Data.Type, "demo")) {
		out.DownloadMode = "free_via_steam"
		out.DownloadExplanation = "Título gratuito na loja: é possível solicitar a instalação pelo cliente oficial Steam. DLCs podem ser pagos e exigir licenças adicionais."
	}
	return out, nil
}
