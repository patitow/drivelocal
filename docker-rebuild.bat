@echo off
echo ========================================
echo    DriveLocal - Rebuild Docker (Sem Cache)
echo ========================================
echo.
echo Este script vai reconstruir a imagem Docker
echo do zero, sem usar cache, garantindo que use
echo o codigo mais recente do projeto.
echo.
echo IMPORTANTE: Isso pode levar alguns minutos!
echo.

REM Verificar Docker
docker info >nul 2>&1
if %errorlevel% neq 0 (
    echo ERRO: Docker Desktop nao esta rodando!
    echo Por favor, inicie o Docker Desktop primeiro.
    pause
    exit /b 1
)

REM Parar container se estiver rodando
echo [1/5] Parando container atual...
docker-compose down

echo.
echo [2/5] Removendo imagens antigas...
docker-compose down --rmi all 2>nul
docker rmi drivelocal-drivelocal 2>nul
docker rmi drivelocal-drivelocal:latest 2>nul
docker system prune -f --volumes 2>nul

echo.
echo [3/5] Limpando cache do Docker...
docker builder prune -af 2>nul

echo.
echo [4/5] Construindo nova imagem (sem cache)...
echo Isso pode levar varios minutos...
docker-compose build --no-cache --pull

if %errorlevel% neq 0 (
    echo.
    echo ERRO: Falha ao construir imagem!
    echo Verifique os erros acima.
    pause
    exit /b 1
)

echo.
echo [5/5] Iniciando container...
docker-compose up -d

if %errorlevel% neq 0 (
    echo ERRO: Falha ao iniciar container!
    pause
    exit /b 1
)

echo.
echo ========================================
echo    Rebuild Concluido!
echo ========================================
echo.
echo Container reconstruido e iniciado com sucesso!
echo.
echo Aguardando container iniciar...
timeout /t 5 /nobreak >nul

echo.
echo Status do container:
docker-compose ps

echo.
echo Para ver os logs:
echo   docker-compose logs -f
echo.
echo Acesse em: http://localhost:80
echo Ou na rede local: http://[SEU_IP]:80
echo.
pause

