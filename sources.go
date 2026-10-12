package main

// ManifestProvider describes a community source for informational purposes.
// Providers listed here are independent of Astronyx. No API credentials,
// depot keys, manifest downloads or Steam modifications are performed.
type ManifestProvider struct {
    Name string
    Access string
    Kind string
    Website string
    Notes string
}

func knownManifestProviders() []ManifestProvider {
    return []ManifestProvider{
        {Name: "Sadie (Hubcap)", Kind: "Serviço comunitário", Access: "Chave de API", Website: "https://hubcapmanifest.com/", Notes: "O LuaTools pode exigir uma chave pessoal do provedor. A fonte não está conectada ao Astronyx."},
        {Name: "Luie", Kind: "Fonte exibida no LuaTools", Access: "Não confirmado", Website: "", Notes: "O nome aparece na seleção de fontes. Não foi confirmado um repositório público oficial ou um contrato de API."},
        {Name: "Ryuu", Kind: "Serviço comunitário", Access: "Pode exigir autenticação", Website: "https://generator.ryuu.lol/faq", Notes: "Disponibilidade e requisitos podem variar. Astronyx não solicita cookies e não acessa contas do serviço."},
        {Name: "Espelhos GitHub", Kind: "Repositórios públicos", Access: "Acesso variável", Website: "https://github.com/madoiscool/LuaTools", Notes: "Espelhos e catálogos não são uma fonte garantida dos arquivos completos dos jogos."},
        {Name: "Morrenus", Kind: "Serviço comunitário", Access: "Chave de API", Website: "https://manifest.morrenus.xyz/", Notes: "Citado na documentação comunitária do LuaTools. Não está conectado ao Astronyx."},
    }
}
