# DriveLocal

Servidor de mídia para LAN: backend em **Go**, frontend em **React + TypeScript + Vite**, empacotado para **Docker**.

Feito para rodar na rede local — streaming com Range Requests, galeria com thumbnails, autenticação e vários usuários.

## O que tem

- Streaming de vídeo e download (incluindo ZIP de vários arquivos)
- Thumbnails gerados com FFmpeg
- Login com cookies httpOnly, refresh token e “lembrar de mim”
- Papéis e permissões (read, write, delete, admin)
- Pronto para Docker, com bind de pastas de mídia no host

## Requisitos

**Produção (Docker):** Docker Desktop / Docker Engine + Compose, e FFmpeg já vai na imagem.

**Desenvolvimento:** [Go 1.24+](https://go.dev/dl/), [Node.js 18+](https://nodejs.org/) e FFmpeg (opcional, só para thumbnails).

## Começando com Docker

```bash
git clone https://github.com/patitow/drivelocal.git
cd drivelocal
```

### 1. Usuários

O `users.json` real **não** entra no git. Copie o exemplo:

```bash
cp server-go/server/data/users.example.json server-go/server/data/users.json
```

Login padrão do exemplo: `admin` / `admin`. Troque isso antes de deixar o servidor na LAN.

No Docker, os usuários ficam no volume `drivelocal-data`. O arquivo do host só vale na primeira subida, se o volume ainda estiver vazio.

### 2. Pastas de mídia

O `docker-compose.yml` commitado **não** monta disco nenhum. Copie o template e aponte para as pastas da sua máquina:

```bash
cp docker-compose.override.example.yml docker-compose.override.yml
```

Exemplo com várias bibliotecas:

```yaml
services:
  drivelocal:
    volumes:
      - "D:/Videos:/data/Videos:ro"
      - "D:/Anime:/data/Anime:ro"
```

Não monte também `./media:/data:ro` junto com binds aninhados (`/data/Anime`, etc.): um `/data` read-only impede o Docker de criar os pontos de montagem.

O `docker-compose.override.yml` está no `.gitignore`.

### 3. Ambiente

```bash
cp .env.example .env
```

No Docker, o importante é `HOST_LAN_IP` (o IPv4 da LAN do host — o container não enxerga isso sozinho) e `TZ` se quiser. Descubra o IP com `ipconfig` (Windows) ou `ip addr` (Linux).

### 4. Subir

```bash
docker compose up -d --build
```

| Onde | URL |
|------|-----|
| Neste PC | http://localhost/ ou http://127.0.0.1:8080/ (se a porta 80 já estiver ocupada) |
| Na LAN | `http://<HOST_LAN_IP>/` |
| Health | http://localhost/api/health |

```bash
docker compose logs -f
docker compose down
```

No Windows, a porta 80 no host pede elevação; por isso o caminho normal de produção é Docker.

## Desenvolvimento (sem Docker)

```bash
cp .env.example .env
cp server-go/server/data/users.example.json server-go/server/data/users.json
cd client && npm install && cd ..
```

```bash
npm run dev
```

- Frontend (Vite): http://localhost:3000
- API: http://localhost:5000

Essas portas não precisam de admin no Windows.

```bash
npm run prod          # build do client + servidor Go em produção
cd server-go && go run .
cd server-go && go build -o drivelocal-server .
```

`MEDIA_PATH` padrão em dev é `./media`.

## Autenticação

Usuários ficam em `server-go/server/data/users.json` (gitignored). Use o `users.example.json` como ponto de partida.

Variáveis úteis:

| Variável | Função | Padrão |
|----------|--------|--------|
| `AUTH_ENABLED` | Liga/desliga login | `true` |
| `MEDIA_PATH` | Raiz da biblioteca | `./media` (no container: `/data`) |
| `PORT` | Porta do servidor | `80` |
| `HOST_LAN_IP` | IPv4 da LAN, para o app anunciar o endereço certo no Docker | (vazio) |
| `TZ` | Timezone | `UTC` no compose |
| `NODE_ENV` | `development` ou `production` | `development` fora do Docker |

## Testes

```bash
cd server-go && go test ./...
cd client && npx vitest
npm run test:playwright    # E2E; sobe `npm run dev` se a porta 3000 estiver livre
```

## Estrutura

```
drivelocal/
├── client/                 # React + Vite
├── server-go/              # API e streaming (Go 1.24)
│   └── server/data/        # users.example.json; users.json é local
├── docker-compose.yml
├── docker-compose.override.example.yml
├── Dockerfile
└── tests/                  # Playwright
```

## Licença

MIT
