# Astronyx Steam Manager V2.2 — aplicativo Windows nativo

**Janela própria de verdade:** o aplicativo usa diretamente a API Win32 para desenhar a interface e receber cliques, com tema escuro/preto e roxo. **Não inicia servidor HTTP e não abre guia do navegador.** Um executável Windows x64, sem dependência de navegador, Go ou Python no computador do usuário.

**LIMITES IMPORTANTES:** os **253.515 registros do catálogo NÃO são jogos instalados ou licenciados.** "Adicionar todos" **apenas salva 250.696 entradas de jogos/DLCs na coleção local do Astronyx**, não altera a Steam, não instala, não executa Lua nem desbloqueia conteúdo pago. Arquivos `.manifest` não concedem licença de jogo/DLC. Instalações autorizadas continuam no cliente oficial Steam.

## Números da base local (GameList-main.zip enviado)

| Tipo | Quantidade |
|:--|--:|
| Jogos (games.json) | 188.092 |
| DLCs (dlc.json) | 62.604 |
| Software (software.json) | 2.744 |
| Hardware (hardware.json) | 21 |
| Vídeos (videos.json) | 54 |
| **Total** | **253.515** |
| **Um clique: jogos + DLCs** | **250.696** |

O `.exe` contém o ZIP (6,7 MB) embutido — **não precisa deixar o ZIP original ao lado dele**. A primeira inicialização indexa dados na memória para busca por nome ou App ID. Fonte do catálogo: dados enviados pelo usuário, não uma verificação atual do catálogo oficial Steam; algumas entradas podem estar desatualizadas.

## Como usar

1. Dê dois cliques em `Astronyx-Steam-Manager-V2.2.exe` (Windows 10/11, x64). A interface nativa abre diretamente.
2. Pesquise por nome ou App ID na caixa de busca. Filtre por **Todos / Jogos / DLCs / Programas / Minha coleção** na barra lateral. Use a roda do mouse para navegar.
3. Clique **+ ADICIONAR TODOS** para adicionar à *coleção do Astronyx* todos os jogos e DLCs do catálogo. A operação pede confirmação, grava preferência compacta em `%APPDATA%\Astronyx\SteamManager\collection.json`, e a coleção persiste depois de fechar o programa.
4. Clique em `ADICIONAR +` ou `ADICIONADO` ao lado do registro para incluir/remover individualmente. Clique em outra parte da linha para consultar dados públicos do jogo na Steam Store — quando a API está disponível, exibe DLCs vinculados, sem afirmar que são licenciados.
5. **IMPORTAR ZIP** permite carregar um `GameList` atualizado (com pelo menos `games.json` e `dlc.json`). O programa valida o ZIP antes de substituir a cópia persistente.
6. **MANIFESTS ZIP** importa arquivos `.acf`, `.lua` e `.manifest` de um ZIP ou um arquivo compatível; os arquivos são mantidos apenas no diretório de dados local, com deduplicação SHA-256 e sem execução de scripts ou gravação na Steam.
7. **STEAM LOCAL** copia para o índice local os `appmanifest_*.acf` dos jogos detectados na Steam instalada. Nenhum arquivo original é modificado.
8. **ABRIR PASTA** abre a pasta privada local de dados. A importação de arquivos roda em segundo plano para manter a interface responsiva.

## Atualizações enquanto o programa estiver aberto

O app mantém o updater existente e verifica na abertura e **a cada 60 segundos**, usando o canal público HTTPS previamente configurado. Quando houver uma nova versão, mostra **NOVA VERSÃO DISPONÍVEL** na própria janela, sem F5 ou navegador. Clicar no aviso abre a confirmação para **baixar, verificar a assinatura Ed25519 e o hash SHA-256, substituir o EXE após fechar a instância e reiniciar**. Não instala uma atualização sem consentimento.

**Ainda não há feed ativo na cópia pré-compilada**: falta publicar o repositório privado, configurar as chaves e o Cloudflare R2. Um repositório *privado* do GitHub não deve ser consultado por um programa público com um token embutido. A solução é manter código privado e publicar **somente executáveis assinados e o feed HTTPS** no R2.

## Repositório público — Night0231-creator/lua-tools

Código-fonte publicado em **https://github.com/Night0231-creator/lua-tools**. A biblioteca incluída vem do arquivo `GameList-main.zip` fornecido pelo usuário, baseado no projeto público [SteamTools-Team/GameList](https://github.com/SteamTools-Team/GameList), snapshot `c029b34a0206a873190e2b8108df5242fb629732` (23/09/2026). Os dados catalogam nomes e App IDs; **não são licenças, jogos instaláveis ou pacotes de DLCs desbloqueados**. Marcas e dados de terceiros pertencem aos respectivos titulares. O Astronyx não é afiliado à Steam/Valve/SteamTools.

Novos commits em `main` acionam o workflow Windows para **testar, compilar e disponibilizar o executável nos artefatos do GitHub Actions**. A entrega automática de atualizações aos aplicativos já instalados depende de um feed HTTPS assinado e do armazenamento R2, que ainda precisam ser configurados; não funciona apenas por publicar commits no GitHub.

**Segurança:** não inclua `.env`, tokens, credenciais, chaves de assinatura privadas ou informações de usuários neste repositório público. Caso se torne necessário proteger partes sensíveis no futuro, use um backend privado e variáveis seguras em Actions.

A action `.github/workflows/windows.yml` recompõe e valida o ZIP original do catálogo na primeira publicação, compila e testa no runner Windows e gera artefatos do EXE. Após os testes, ela consolida o catálogo em `assets/GameList-main.zip` e apaga os fragmentos temporários. Não é preciso baixar ou juntar ZIPs manualmente. Para publicar uma versão para atualizações, você precisa definir:

**GitHub → Settings → Secrets and variables → Actions**

- Variables: `UPDATE_PUBLIC_KEY` (Ed25519 pública, base64), `UPDATE_FEED_URL` (URL HTTPS do feed), `UPDATE_PUBLIC_BASE_URL` (prefixo de URLs HTTPS), `R2_BUCKET`.
- Secrets: `UPDATE_PRIVATE_KEY` (Ed25519 privada, base64), `R2_ENDPOINT`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`.
- Gere as chaves uma vez, em ambiente seguro: `go run ./tools/generate-keys`. Guarde a chave *privada* **somente** em GitHub Secrets. **Nunca** a inclua no app/repositório.
- Publique somente após os testes, marcando uma tag como `v2.2.1`. No repositório público, os usuários podem baixar compilações testadas pela aba **Actions**; para releases com atualização automática é preciso configurar o R2. A action envia o EXE ao R2 e **por último** o JSON do feed de atualização.

Para uma compilação já publicada sem canal, é possível preparar `%APPDATA%\Astronyx\SteamManager\update-channel.json` com o formato:

```json
{"feedUrl":"https://updates.exemplo.com/releases/latest.json","publicKey":"CHAVE_PUBLICA_ED25519_EM_BASE64"}
```

Depois reinicie uma vez. **Esta configuração não funciona até que o servidor e as chaves estejam realmente configurados.** Compilações futuras do GitHub com as variables corretas já receberão o feed integrado.

## Compilação manual no Windows (Go 1.22+)

```cmd
go test ./...
go build -trimpath -ldflags="-H=windowsgui -s -w" -o Astronyx-Steam-Manager.exe .
```

O projeto depende apenas da biblioteca padrão Go. Para desenvolvimento no Linux (somente compilação cruzada; a interface não abre no Linux):

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-H=windowsgui -s -w' -o Astronyx-Steam-Manager.exe .
```

**Verificações feitas nesta entrega:** 253.515 entradas contadas e carregadas corretamente, adição em massa 250.696, busca, persistência, importações inválidas, testes de biblioteca Steam e validação criptográfica; `go test ./...`, `go vet` do alvo Windows e compilação `PE32+` bem-sucedidos. **A interface Win32 e a instalação do updater ainda precisam ser testadas no Windows real**; compilação cruzada não equivale a teste da janela/execução.

O Astronyx é um aplicativo independente, não oficial e não afiliado à Valve, Steam, SteamTools ou LuaTools. Nada é enviado a serviços externos exceto consultas à API pública da Steam iniciadas pelo usuário e consultas ao feed de atualização configurado.
