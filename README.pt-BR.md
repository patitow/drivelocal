<div align="center">

# DriveLocal

**Seu próprio serviço de streaming, rodando na sua rede.**
Servidor de mídia self-hosted para animes, séries, filmes e mangás, com um catálogo que uma IA local ajuda a manter organizado.

Português · [English](./README.md)

![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square&logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-TypeScript-3178C6?style=flat-square&logo=react&logoColor=white)
![FFmpeg](https://img.shields.io/badge/FFmpeg-streaming-007808?style=flat-square&logo=ffmpeg&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-pronto-2496ED?style=flat-square&logo=docker&logoColor=white)
![IA local](https://img.shields.io/badge/IA-local_e_revisada-8B5CF6?style=flat-square)
![Licença](https://img.shields.io/badge/licen%C3%A7a-MIT-green?style=flat-square)

![Página de série do DriveLocal](./docs/assets/series.png)

</div>

## Por quê

Pasta cheia de `.mkv` não é biblioteca. O DriveLocal varre suas pastas e transforma tudo em catálogo: séries agrupadas por temporada, sinopse, capa e banner, o que você já viu e onde parou. Toca qualquer arquivo no navegador, em qualquer aparelho da sua rede, sem nada sair de casa.

## Destaques

### Um catálogo com cara de app de streaming

- **Estantes** de animes, filmes e séries, mangás e criadores, com início, "continuar assistindo", favoritos, playlists e tópicos
- **Página de série** com temporadas, progresso por episódio, "próximo" e autoplay
- **Metadados de várias fontes**: AniList, MyAnimeList, Kitsu, TMDB, TVmaze e Wikipédia (sinopse, ano, capa, banner), com seletor de arte e recorte
- **Leitor de mangá** com progresso de leitura

### Um player que toca tudo

![Player do DriveLocal](./docs/assets/player.png)

- **Reprodução direta** quando o navegador consegue e **remux ou transcode na hora com FFmpeg** quando não consegue (MKV, áudio AC3/DTS, HEVC…), escolhido sozinho
- Respostas HTTP Range de verdade nos streams processados: seek, reconexão e pause longo retomam no ponto certo
- **Faixas de áudio e legenda** trocáveis no meio do episódio, incluindo legendas ASS estilizadas com as fontes embutidas
- **Lembra a dublagem e a legenda escolhidas em cada série**
- Teclado: <kbd>Espaço</kbd> play/pause, <kbd>F</kbd> tela cheia, <kbd>C</kbd> legenda, <kbd>M</kbd> mudo, setas para avançar e volume

### Curadoria com IA, sob seu controle

- **Classificação por IA local**: um modelo que você mesmo roda (qualquer endpoint compatível com OpenAI, como llama.cpp ou llama-swap) lê o nome do arquivo e, se precisar, um frame do vídeo, e propõe título, série, temporada, episódio e tags
- A resposta segue um **JSON schema**, o trabalho só roda **dentro de uma janela de horário** (de madrugada, por exemplo) e **tudo vira sugestão**: nada muda no catálogo até você aceitar na fila de revisão
- A qualidade é medida contra um **golden set** (`eval`), para comparar mudanças de prompt ou de modelo
- **Detecção de duplicatas** de séries e criadores escritos de jeitos diferentes (token set ratio, Jaro-Winkler e contenção), com fusão que guarda os apelidos e pode ser desfeita
- **Regras, operações em lote com dry-run, histórico e desfazer** para tudo que altera o catálogo

### Pensado para agentes

O DriveLocal tem **tokens de API** e uma CLI, `drivelocal-cli`. Um agente de IA (Claude Code, ou qualquer assistente com shell ou ferramentas MCP) busca no catálogo, consulta metadados, monta um lote, roda em dry-run e depois aplica ou desfaz. Curadoria em massa vira conversa, não uma tarde inteira clicando.

```bash
export DRIVELOCAL_URL=http://localhost
export DRIVELOCAL_TOKEN=dlt_...          # criado por um admin: POST /api/admin/library/tokens

drivelocal-cli search "korra"
drivelocal-cli review --kind topic       # sugestões pendentes da IA
drivelocal-cli accept 812 813
drivelocal-cli batch ajustes.json --dry  # confere antes
drivelocal-cli undo 42                   # todo lote pode ser desfeito
```

### Self-hosted e multiusuário

- Login com cookies httpOnly, access token curto com renovação silenciosa e "lembrar de mim"
- Papéis e permissões (read, write, delete, admin); progresso, favoritos e faixas por usuário
- Um `docker compose up`; bibliotecas podem ser adicionadas pela interface com um pequeno helper no host

## Começando (Docker)

```bash
git clone https://github.com/patitow/drivelocal.git
cd drivelocal
cp .env.example .env
cp server-go/server/data/users.example.json server-go/server/data/users.json
cp docker-compose.override.example.yml docker-compose.override.yml
```

Aponte o override para as suas pastas de mídia:

```yaml
services:
  drivelocal:
    volumes:
      - "D:/Anime:/data/Anime:ro"
      - "D:/Filmes:/data/Filmes:ro"
```

```bash
docker compose up -d --build
```

Abra http://localhost (ou http://127.0.0.1:8080 se a porta 80 estiver ocupada). O login do exemplo é `admin` / `admin`; troque antes de deixar o servidor na rede.

> Não monte também `./media:/data:ro` junto com binds aninhados como `/data/Anime`: um `/data` somente leitura impede o Docker de criar os pontos de montagem.

## Configuração

| Variável | Função | Padrão |
| --- | --- | --- |
| `HOST_LAN_IP` | IPv4 do host na LAN, para o app mostrar o endereço certo | vazio |
| `AUTH_ENABLED` | Liga ou desliga o login | `true` |
| `TZ` | Fuso horário | `UTC` |
| `TMDB_API_KEY` | Metadados de filmes e séries do TMDB | vazio |
| `MAL_CLIENT_ID` | API do MyAnimeList (sem ela, usa o Jikan) | vazio |
| `AI_BASE_URL` | Endpoint compatível com OpenAI da IA local | vazio (IA desligada) |
| `AI_API_KEY` | Chave desse endpoint | vazio |
| `AI_TEXT_MODEL` / `AI_VISION_MODEL` | Modelos de texto e visão | ver `.env.example` |
| `AI_WINDOW` / `AI_TZ` | Quando a IA pode rodar | `01:00-08:00` |

As chaves ficam só no `.env`, que está no `.gitignore`.

## Desenvolvimento

Requisitos: [Go 1.24+](https://go.dev/dl/), [Node.js 18+](https://nodejs.org/) e FFmpeg.

```bash
cd client && npm install && cd ..
npm run dev                 # Vite em :3000, API em :5000
```

```bash
cd server-go && go test ./...
cd client && npx vitest run
npm run test:playwright     # ponta a ponta
```

## Stack

| Camada | Tecnologia |
| --- | --- |
| Servidor | Go, SQLite, FFmpeg / ffprobe |
| Cliente | React, TypeScript, Vite, TanStack Router e Query, Tailwind |
| IA | Qualquer modelo local compatível com OpenAI (texto e visão) |
| Empacotamento | Docker Compose, mais um helper opcional no host para montar pastas |

```
drivelocal/
├── client/          # app React
├── server-go/       # API, scanner, streaming, curadoria com IA, CLI (cmd/drivelocal-cli)
├── host-helper/     # helper opcional para montar pastas do host pela interface
├── docker-compose.yml
└── tests/           # Playwright
```

## Licença

MIT, veja [LICENSE](./LICENSE).
