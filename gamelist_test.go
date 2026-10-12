package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestBundledCatalogAndCollection(t *testing.T) {
	c, e := loadStoreCatalog(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if n := len(c.Entries); n != 253515 {
		t.Fatalf("esperado 253515 entradas, obtido %d", n)
	}
	if c.CountByKind["game"] != 188092 || c.CountByKind["dlc"] != 62604 || c.TotalGameDLC != 250696 {
		t.Fatalf("totais incorretos: %+v", c.CountByKind)
	}
	sel, e := loadCollection(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if err := sel.AddAllGamesDLC(); err != nil {
		t.Fatal(err)
	}
	if n := sel.Count(c); n != 250696 {
		t.Fatalf("coleção: %d", n)
	}
	if sel.selected(c.Entries[c.ByID[1840]]) {
		t.Fatal("software não deve ser importado ao adicionar jogos/DLC")
	}
	if err := sel.Toggle(c.Entries[c.ByID[10]]); err != nil {
		t.Fatal(err)
	}
	if n := sel.Count(c); n != 250695 {
		t.Fatalf("toggle inválido %d", n)
	}
	again, e := loadCollection(sel.dir)
	if e != nil {
		t.Fatal(e)
	}
	if again.selected(c.Entries[c.ByID[10]]) {
		t.Fatal("toggle não persistiu")
	}
	hits, total := c.Search("Cyberpunk 2077", "all", 0, 20, again.selected)
	if total == 0 || len(hits) == 0 {
		t.Fatal("busca sem resultados")
	}
}
func TestRejectInvalidCatalog(t *testing.T) {
	_, err := catalogFromZIP([]byte("bad"))
	if err == nil {
		t.Fatal("ZIP inválido aceito")
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	f, _ := z.Create("games.json")
	_, _ = f.Write([]byte(`[{"appid":1,"name":"x"}]`))
	_ = z.Close()
	_, err = catalogFromZIP(buf.Bytes())
	if err == nil {
		t.Fatal("DLC ausente foi aceito")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "broken.zip")
	_ = os.WriteFile(src, buf.Bytes(), 0600)
	if _, err := importStoreCatalog(dir, src); err == nil {
		t.Fatal("importação inválida aceita")
	}
}
