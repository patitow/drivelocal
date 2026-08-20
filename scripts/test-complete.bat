@echo off
echo ========================================
echo    DriveLocal - Teste Completo
echo ========================================
echo.

echo [1/6] Verificando Docker...
docker --version >nul 2>&1
if %errorlevel% neq 0 (
    echo ❌ Docker nao encontrado!
    echo Execute: install-service.bat
    pause
    exit /b 1
)
echo ✅ Docker funcionando!

echo.
echo [2/6] Verificando Docker Compose...
docker-compose --version >nul 2>&1
if %errorlevel% neq 0 (
    echo ❌ Docker Compose nao encontrado!
    echo Execute: install-service.bat
    pause
    exit /b 1
)
echo ✅ Docker Compose funcionando!

echo.
echo [3/6] Construindo imagem Docker...
docker-compose build

if %errorlevel% neq 0 (
    echo ❌ Falha ao construir imagem!
    echo Verifique se o Docker Desktop esta rodando.
    pause
    exit /b 1
)
echo ✅ Imagem construida com sucesso!

echo.
echo [4/6] Iniciando container...
docker-compose up -d

if %errorlevel% neq 0 (
    echo ❌ Falha ao iniciar container!
    pause
    exit /b 1
)
echo ✅ Container iniciado!

echo.
echo [5/6] Aguardando inicializacao...
echo Aguardando 30 segundos para o servidor inicializar...
timeout /t 30 /nobreak >nul

echo.
echo [6/6] Testando API...
curl -s http://localhost:8080/api/health >nul 2>&1
if %errorlevel% neq 0 (
    echo ❌ API nao esta respondendo!
    echo Aguarde mais alguns segundos e execute: check-running.bat
    pause
    exit /b 1
)

echo.
echo ========================================
echo    TESTE COMPLETO: SUCESSO!
echo ========================================
echo.
echo ✅ Docker: Funcionando
echo ✅ Docker Compose: Funcionando
echo ✅ Imagem: Construida
echo ✅ Container: Rodando
echo ✅ API: Respondendo
echo.
echo Acesse a aplicacao em:
echo   - Local: http://localhost:8080
echo   - Rede: http://[SEU_IP]:8080
echo.
echo Para configurar autostart:
echo   configure-autostart.bat
echo.
echo Para ver os logs:
echo   docker-compose logs -f
echo.
echo Para parar o servico:
echo   stop-service.bat
echo.
pause
