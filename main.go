package main

import (
	"log"
	"os"
	"path/filepath"
)

type App struct {
	dataDir   string
	store     *StoreCatalog
	selection *Collection
	manifests *Catalog
	updater   *Updater
}

func newApp(dir string) (*App, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	store, err := loadStoreCatalog(dir)
	if err != nil {
		return nil, err
	}
	selection, err := loadCollection(dir)
	if err != nil {
		return nil, err
	}
	manifests, err := newCatalog(dir)
	if err != nil {
		return nil, err
	}
	_ = loadChannel(dir)
	return &App{dataDir: dir, store: store, selection: selection, manifests: manifests, updater: newUpdater()}, nil
}
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--apply-update" {
		if err := applyUpdateHelper(os.Args[2:]); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	home, err := os.UserConfigDir()
	if err != nil {
		home = "."
	}
	app, err := newApp(filepath.Join(home, "Astronyx", "SteamManager"))
	if err != nil {
		showError("Falha ao iniciar Astronyx: " + err.Error())
		return
	}
	runDesktop(app)
}
