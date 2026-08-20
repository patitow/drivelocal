@echo off
echo ========================================
echo    DriveLocal - Iniciar Docker
echo ========================================
echo.

REM Verificar Docker
docker info >nul 2>&1
if %errorlevel% neq 0 (
    echo ERRO: Docker Desktop nao esta rodando!
    echo Por favor, inicie o Docker Desktop primeiro.
    pause
    exit /b 1
)

echo Construindo imagem (se necessario)...
docker-compose build

echo.
echo Iniciando container...
docker-compose up -d

if %errorlevel% neq 0 (
    echo ERRO: Falha ao iniciar container!
    pause
    exit /b 1
)

echo.
echo ========================================
echo    DriveLocal Iniciado!
echo ========================================
echo.
echo Container rodando com restart automatico.
echo.
echo Para ver os logs:
echo   docker-compose logs -f
echo.
echo Para parar:
echo   docker-compose down
echo.
echo Acesse em: http://localhost:80
echo Ou na rede local: http://[SEU_IP]:80
echo.
pause

