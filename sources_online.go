package main

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "strings"
 "time"
)

const (
 steamServiceProbe = "https://store.steampowered.com/api/appdetails"
 gameListServiceProbe = "https://api.github.com/repos/SteamTools-Team/GameList"
 gameListArchiveURL = "https://codeload.github.com/SteamTools-Team/GameList/zip/refs/heads/main"
)

// Status here only concerns public metadata services; it never confirms any license.
type ProviderHealth struct {
 Name string
 Online bool
 Detail string
 CheckedAt time.Time
}

func checkPublicSources(ctx context.Context, client *http.Client, steamURL, githubURL string) []ProviderHealth {
 result:=make([]ProviderHealth,0,2)
 steamCtx,cancel:=context.WithTimeout(ctx,12*time.Second)
 inspection,err:=steamStoreMetadata(steamCtx,client,steamURL,"440")
 cancel()
 steam:=ProviderHealth{Name:"Steam Store (metadados)",CheckedAt:time.Now()}
 if err!=nil {steam.Detail="Não acessível: "+shortStatus(err.Error())} else if inspection.AppID=="440" && inspection.Name!="" {
  steam.Online=true; steam.Detail="Consulta pública de jogos e DLCs disponível"
 } else {steam.Detail="Resposta pública inesperada"}
 result=append(result,steam)
 githubCtx,stop:=context.WithTimeout(ctx,12*time.Second)
 defer stop()
 gh:=ProviderHealth{Name:"GameList (GitHub público)",CheckedAt:time.Now()}
 req,err:=http.NewRequestWithContext(githubCtx,http.MethodGet,githubURL,nil)
 if err==nil {
  req.Header.Set("Accept","application/vnd.github+json")
  req.Header.Set("User-Agent","AstronyxSteamManager/2.3 (public-catalogue)")
  var resp *http.Response
  resp,err=client.Do(req)
  if err==nil {
   defer resp.Body.Close()
   if resp.StatusCode!=http.StatusOK {err=fmt.Errorf("HTTP %d",resp.StatusCode)} else {
    raw,readErr:=io.ReadAll(io.LimitReader(resp.Body,65537))
    if readErr!=nil {err=readErr} else if len(raw)>65536 {err=errors.New("resposta muito grande")} else {
     var info struct {FullName string `json:"full_name"`; DefaultBranch string `json:"default_branch"`}
     err=json.Unmarshal(raw,&info)
     if err==nil && info.FullName!="SteamTools-Team/GameList" {err=errors.New("repositório inesperado")}
     if err==nil && info.DefaultBranch!="main" {err=errors.New("branch padrão diferente; atualização não executada")}
    }
   }
  }
 }
 if err==nil {gh.Online=true;gh.Detail="Catálogo público acessível para sincronização"} else {gh.Detail="Não acessível: "+shortStatus(err.Error())}
 return append(result,gh)
}

func shortStatus(s string) string {
 s=strings.Map(func(r rune)rune{if r<32{return ' '};return r},s)
 if len(s)>120{return s[:120]+"..."}
 return s
}

// Imports only validated public metadata into app data; no Steam files are modified.
func downloadPublicCatalogue(ctx context.Context, client *http.Client, archiveURL, dir string)(*StoreCatalog,error){
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,archiveURL,nil)
 if err!=nil{return nil,err}
 req.Header.Set("User-Agent","AstronyxSteamManager/2.3 (public-catalogue)")
 resp,err:=client.Do(req)
 if err!=nil{return nil,err}
 defer resp.Body.Close()
 if resp.StatusCode!=http.StatusOK{return nil,fmt.Errorf("GameList respondeu HTTP %d",resp.StatusCode)}
 const maxDownload=45<<20
 data,err:=io.ReadAll(io.LimitReader(resp.Body,maxDownload+1))
 if err!=nil{return nil,err}
 if len(data)>maxDownload{return nil,errors.New("download excede 45 MB")}
 if len(data)<100{return nil,errors.New("arquivo ZIP incompleto")}
 return importStoreCatalogBytes(dir,data)
}

func newPublicHTTPClient()*http.Client{
 allow:=map[string]bool{"store.steampowered.com":true,"api.github.com":true,"github.com":true,"codeload.github.com":true}
 return &http.Client{
  Timeout:90*time.Second,
  CheckRedirect:func(req *http.Request,via []*http.Request)error{
   if len(via)>=5 || req.URL.Scheme!="https" || !allow[req.URL.Hostname()] {return errors.New("redirecionamento bloqueado fora das fontes autorizadas")}
   return nil
  },
  Transport:&http.Transport{Proxy:http.ProxyFromEnvironment,MaxIdleConns:4,MaxIdleConnsPerHost:2,IdleConnTimeout:30*time.Second,ResponseHeaderTimeout:20*time.Second},
 }
}
