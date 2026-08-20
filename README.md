# DriveLocal Media Server

Servidor de mídia local de alta performance para gerenciamento de vídeos e arquivos, construído com **Go** e **React**.

## 🚀 Características

- ⚡ **Alta Performance**: Servidor Go com 5-10x mais rápido que Node.js
- 🔐 **Autenticação Completa**: Cookies httpOnly, refresh tokens, recuperação de senha
- 👥 **Multi-usuário**: Sistema de usuários com roles e permissões
- 📹 **Streaming de Vídeo**: Suporte a Range Requests para streaming eficiente
- 📦 **Download Múltiplo**: Download de múltiplos arquivos em ZIP
- 🖼️ **Thumbnails**: Geração automática de thumbnails para vídeos
- 🐳 **Docker Ready**: Pronto para containerização
- 🌐 **Acesso Local**: Disponível na rede local na porta HTTP padrão (80)

## 📋 Pré-requisitos

- **Go 1.21+** - [Download](https://golang.org/dl/)
- **Node.js 18+** - Para build do frontend
- **FFmpeg** - Para geração de thumbnails (opcional)

## 🔧 Instalação

### 1. Clone o repositório

```bash
git clone https://github.com/patitow/drivelocal.git
cd drivelocal
```

### 2. Configure as variáveis de ambiente

Copie `.env.example` para `.env` e ajuste:

```bash
cp .env.example .env
```

Edite `.env`:
```env
# Portas (padrão em dev: 3000/5000, produção: 80)
PORT=80
API_PORT=5000
MEDIA_PATH=./media
AUTH_ENABLED=true
NODE_ENV=development
```

**Nota:** Em modo desenvolvimento (`npm run dev`), as portas padrão são:
- Frontend: `3000` (sem privilégios admin)
- Backend: `5000` (sem privilégios admin)

Em produção, usa porta `80` (requer privilégios admin).

### 3. Instale as dependências

```bash
# Dependências do frontend
cd client
npm install
cd ..

# Dependências do Go (automático no primeiro run)
cd server-go
go mod download
cd ..
```

## 🚀 Uso

### Desenvolvimento

```bash
npm run dev
```

Inicia o servidor em modo desenvolvimento:
- **Frontend (Vite):** http://localhost:3000
- **Backend API:** http://localhost:5000/api

**✨ Sem necessidade de privilégios de administrador!** As portas padrão (3000/5000) não requerem elevação no Windows.

### Produção

```bash
npm run prod
```

Compila o frontend e inicia o servidor Go em modo produção.

### Build Manual

```bash
# Build do frontend
npm run build

# Build do servidor Go
npm run build:server

# Build completo
npm run build:all
```

## 🐳 Docker

### Configuração Rápida com Autostart

Para configurar o DriveLocal para iniciar automaticamente com o Windows e reiniciar em caso de crash:

```batch
scripts\setup-docker-autostart.bat
```

Este script configura:
- ✅ Docker Desktop para autostart
- ✅ DriveLocal para iniciar automaticamente
- ✅ Reinício automático em caso de crash (`restart: always`)
- ✅ Firewall para rede local apenas (não exposto para internet)

### Executar Manualmente

```batch
# Iniciar
docker-start.bat
# ou
docker compose up -d

# Parar
docker-stop.bat
# ou
docker compose down

# Ver logs
docker compose logs -f

# Status
docker compose ps
```

### Acesso na Rede Local

O servidor está configurado para:
- **Porta**: 80 (padrão)
- **Rede**: Acessível apenas na rede local (não exposto para internet)
- **Bind**: `0.0.0.0` (aceita conexões de qualquer interface de rede local)

Para descobrir seu IP local:
```batch
ipconfig | findstr "IPv4"
```

Acesse em: `http://[SEU_IP_LOCAL]:80`

### Docker Compose Manual

```bash
# Build
docker compose build

# Iniciar
docker compose up -d

# Logs
docker compose logs -f

# Parar
docker compose down

# Reiniciar
docker compose restart
```

Pastas de mídia da sua máquina **não** vão no `docker-compose.yml` público. Copie o example e aponte para os discos locais:

```bash
cp docker-compose.override.example.yml docker-compose.override.yml
```

O Compose junta os dois arquivos. O `docker-compose.override.yml` está no `.gitignore`.

### Remover Autostart

```batch
scripts\remove-docker-autostart.bat
```

📖 **Documentação completa**: Veja [DOCKER-SETUP.md](./DOCKER-SETUP.md) para mais detalhes.

## ⚙️ Configuração

### Variáveis de Ambiente

| Variável | Descrição | Padrão |
|----------|-----------|--------|
| `PORT` | Porta do servidor | `80` |
| `MEDIA_PATH` | Caminho para pasta de mídia | `./media` |
| `AUTH_ENABLED` | Habilitar autenticação | `true` |
| `NODE_ENV` | Ambiente (development/production) | `development` |
| `TZ` | Timezone | `America/Sao_Paulo` |

### Acesso

- **Local**: http://localhost/
- **Rede**: `http://<LAN-IP>/` (defina `HOST_LAN_IP` no `.env`)
- **API Health**: http://localhost/api/health

## 🔐 Autenticação

### Usuário Padrão

Copie `server-go/server/data/users.example.json` para `users.json` no mesmo diretório (arquivo local, fora do git).

- **Username**: `admin`
- **Password**: `admin`

⚠️ **Importante**: Altere a senha padrão em produção!

### Recursos de Autenticação

- ✅ Cookies httpOnly (seguro)
- ✅ Refresh tokens automáticos
- ✅ "Remember me" (30 dias)
- ✅ Recuperação de senha
- ✅ Múltiplos usuários
- ✅ Sistema de permissões (read, write, delete, admin)

## 📁 Estrutura do Projeto

```
drivelocal/
├── client/              # Frontend React
│   ├── src/
│   └── dist/           # Build do frontend
├── server-go/          # Servidor Go
│   ├── main.go
│   ├── auth.go
│   ├── handlers.go
│   └── middleware.go
├── scripts/            # Scripts de desenvolvimento
├── dist/               # Build final (frontend)
├── docker-compose.yml
├── Dockerfile
└── package.json
```

## 🧪 Testes

```bash
# Testes E2E com Playwright
npm run test:playwright

# Testes E2E com UI
npm run test:playwright:ui
```

## 📊 Performance

Comparado ao Node.js:
- **Throughput**: ~5-10x mais requisições/segundo
- **Latência**: ~50% menor
- **Memória**: ~70% menos uso
- **CPU**: ~40% menos uso

## 🛠️ Desenvolvimento

### Adicionar Nova Rota

1. Edite `server-go/handlers.go`
2. Adicione o handler
3. Registre a rota em `server-go/main.go`

### Modificar Autenticação

Edite `server-go/auth.go` para ajustar:
- Expiração de tokens
- Validação de usuários
- Permissões

## 📝 Licença

MIT

## 🤝 Contribuindo

Contribuições são bem-vindas! Por favor, abra uma issue ou pull request.
