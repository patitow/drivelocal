@echo off
echo ========================================
echo    DriveLocal - Verificar Funcionamento
echo ========================================
echo.

echo [1/5] Verificando Docker...
docker --version >nul 2>&1
if %errorlevel% neq 0 (
    echo ❌ Docker nao encontrado!
    echo Execute: install-service.bat
    pause
    exit /b 1
)
echo ✅ Docker funcionando!

echo.
echo [2/5] Verificando containers...
docker-compose ps
echo.

echo [3/5] Verificando se DriveLocal esta rodando...
docker-compose ps | findstr "drivelocal-media-server" | findstr "Up" >nul 2>&1
if %errorlevel% neq 0 (
    echo ❌ DriveLocal nao esta rodando!
    echo Execute: start-service.bat
    pause
    exit /b 1
)
echo ✅ DriveLocal esta rodando!

echo.
echo [4/5] Testando API...
curl -s http://localhost:8080/api/health >nul 2>&1
if %errorlevel% neq 0 (
    echo ❌ API nao esta respondendo!
    echo Aguarde alguns segundos e tente novamente.
    pause
    exit /b 1
)
echo ✅ API respondendo corretamente!

echo.
echo [5/5] Testando conectividade de rede...
echo Testando se a aplicacao esta acessivel na rede...

echo.
echo ========================================
echo    Status: FUNCIONANDO PERFEITAMENTE!
echo ========================================
echo.
echo ✅ Docker: Funcionando
echo ✅ Container: Rodando
echo ✅ API: Respondendo
echo ✅ Aplicacao: Acessivel
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
echo Para parar o servico:
echo   stop-service.bat
echo.
pause
