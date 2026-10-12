package main

import (
 "archive/zip"
 "bytes"
 "context"
 "fmt"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func tinySourceZIP(t *testing.T) []byte {
 t.Helper()
 var data bytes.Buffer
 z:=zip.NewWriter(&data)
 for _,entry:=range []struct{name,value string}{
  {"GameList-main/games.json",`[{"appid":440,"name":"Team Fortress 2"}]`},
  {"GameList-main/dlc.json",`[{"appid":999999,"name":"Example DLC"}]`},
 } {
  file,err:=z.Create(entry.name); if err!=nil{t.Fatal(err)}
  if _,err=file.Write([]byte(entry.value));err!=nil{t.Fatal(err)}
 }
 if err:=z.Close();err!=nil{t.Fatal(err)}
 return data.Bytes()
}

func TestPublicSourceProbes(t *testing.T){
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path=="/steam"{
   if r.URL.Query().Get("appids")!="440"{t.Error("wrong AppID")}
   fmt.Fprint(w,`{"440":{"success":true,"data":{"steam_appid":440,"name":"Team Fortress 2","type":"game","is_free":true}}}`)
  }else{fmt.Fprint(w,`{"full_name":"SteamTools-Team/GameList","default_branch":"main"}`)}
 }))
 defer srv.Close()
 health:=checkPublicSources(context.Background(),srv.Client(),srv.URL+"/steam",srv.URL+"/github")
 if len(health)!=2 || !health[0].Online || !health[1].Online{t.Fatalf("invalid status: %#v",health)}
}

func TestOnlineSyncRetainsPreviousOnError(t *testing.T){
 dir:=t.TempDir()
 original:=tinySourceZIP(t)
 if _,err:=importStoreCatalogBytes(dir,original);err!=nil{t.Fatal(err)}
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.Write([]byte(strings.Repeat("bad",200)))}))
 defer srv.Close()
 if _,err:=downloadPublicCatalogue(context.Background(),srv.Client(),srv.URL,dir);err==nil{t.Fatal("invalid ZIP accepted")}
 got,err:=os.ReadFile(filepath.Join(dir,"store-catalog.zip"));if err!=nil{t.Fatal(err)}
 if !bytes.Equal(got,original){t.Fatal("previous catalogue altered")}
}

func TestOnlineSyncAcceptsValidZIP(t *testing.T){
 b:=tinySourceZIP(t)
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.Write(b)}))
 defer srv.Close()
 c,err:=downloadPublicCatalogue(context.Background(),srv.Client(),srv.URL,t.TempDir())
 if err!=nil || len(c.Entries)!=2{t.Fatalf("failed import: %v",err)}
}

func TestDisallowUnsafeRedirect(t *testing.T){
 client:=newPublicHTTPClient()
 prev,_:=http.NewRequest("GET","https://codeload.github.com/a",nil)
 next,_:=http.NewRequest("GET","https://example.com/b",nil)
 if client.CheckRedirect(next,[]*http.Request{prev})==nil{t.Fatal("allowed external host")}
}
