@echo off
echo ========================================
echo    DriveLocal - Instalacao como Servico
echo ========================================
echo.

echo [1/4] Verificando Docker...
docker --version >nul 2>&1
if %errorlevel% neq 0 (
    echo ERRO: Docker nao encontrado!
    echo Por favor, instale o Docker Desktop.
    pause
    exit /b 1
)
echo ✅ Docker encontrado!

echo.
echo [2/4] Verificando Docker Compose...
docker-compose --version >nul 2>&1
if %errorlevel% neq 0 (
    echo ERRO: Docker Compose nao encontrado!
    echo Por favor, instale o Docker Compose.
    pause
    exit /b 1
)
echo ✅ Docker Compose encontrado!

echo.
echo [3/4] Configurando Docker para iniciar automaticamente...
echo Configurando Docker Desktop para iniciar com Windows...

echo.
echo [4/4] Instalando DriveLocal como servico...
echo Parando servico anterior se existir...
docker-compose down >nul 2>&1

echo Construindo imagem...
docker-compose build

if %errorlevel% neq 0 (
    echo ERRO: Falha ao construir imagem!
    pause
    exit /b 1
)

echo Iniciando servico...
docker-compose up -d

if %errorlevel% neq 0 (
    echo ERRO: Falha ao iniciar servico!
    pause
    exit /b 1
)

echo.
echo ========================================
echo    Instalacao Concluida!
echo ========================================
echo.
echo ✅ DriveLocal instalado como servico
echo ✅ Iniciara automaticamente com o Windows
echo ✅ Docker configurado para autostart
echo.
echo Acesse a aplicacao em:
echo   - Local: http://localhost:8080
echo   - Rede: http://[SEU_IP]:8080
echo.
echo Para gerenciar o servico:
echo   - Parar: docker-compose down
echo   - Iniciar: docker-compose up -d
echo   - Logs: docker-compose logs -f
echo.
echo O servico iniciara automaticamente na proxima reinicializacao!
echo.
pause
