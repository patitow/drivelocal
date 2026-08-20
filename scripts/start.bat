@echo off
echo ========================================
echo    DriveLocal Media Server
echo ========================================
echo.

echo Verificando Docker...
docker --version >nul 2>&1
if %errorlevel% neq 0 (
    echo ERRO: Docker nao encontrado!
    pause
    exit /b 1
)

echo Docker encontrado!
echo.

echo Verificando Docker Compose...
docker-compose --version >nul 2>&1
if %errorlevel% neq 0 (
    echo ERRO: Docker Compose nao encontrado!
    pause
    exit /b 1
)

echo Docker Compose encontrado!
echo.

echo IMPORTANTE: Monte sua pasta de midia em docker-compose.override.yml
echo (veja docker-compose.override.example.yml)
echo.
echo Se sua pasta de midia estiver em outro local, copie o example e edite os binds.
echo.
pause

echo Iniciando DriveLocal Media Server...
echo.

echo Construindo imagem Docker...
docker-compose build

if %errorlevel% neq 0 (
    echo ERRO: Falha ao construir imagem Docker!
    pause
    exit /b 1
)

echo.
echo Iniciando servico...
docker-compose up -d

if %errorlevel% neq 0 (
    echo ERRO: Falha ao iniciar servico!
    pause
    exit /b 1
)

echo.
echo ========================================
echo    Servidor iniciado com sucesso!
echo ========================================
echo.
echo Acesse a aplicacao em:
echo   - Local: http://localhost:8080
echo   - Rede: http://[SEU_IP]:8080
echo.
echo Para encontrar seu IP na rede:
echo   ipconfig
echo.
echo Para ver os logs:
echo   docker-compose logs -f
echo.
echo Para parar o servidor:
echo   docker-compose down
echo.
pause