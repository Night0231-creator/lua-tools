package main

import "testing"

func TestManifestProvidersInformational(t *testing.T) {
    items := knownManifestProviders()
    if len(items) < 3 { t.Fatalf("expected multiple sources, got %d", len(items)) }
    names := map[string]bool{}
    for _, src := range items {
        if src.Name == "" || src.Kind == "" || src.Access == "" || src.Notes == "" {
            t.Fatalf("incomplete source: %#v", src)
        }
        if names[src.Name] { t.Fatalf("duplicate provider: %s", src.Name) }
        names[src.Name] = true
    }
    for _, name := range []string{"Sadie (Hubcap)", "Luie", "Ryuu"} {
        if !names[name] { t.Fatalf("missing provider: %s", name) }
    }
}
