@echo off
echo ========================================
echo    DriveLocal - Status do Servico
echo ========================================
echo.

echo [1/3] Verificando status dos containers...
docker-compose ps

echo.
echo [2/3] Verificando logs recentes...
docker-compose logs --tail=10

echo.
echo [3/3] Testando conectividade...
curl -s http://localhost:8080/api/health >nul 2>&1
if %errorlevel% equ 0 (
    echo ✅ Servidor respondendo corretamente!
    echo.
    echo Acesse a aplicacao em:
    echo   - Local: http://localhost:8080
    echo   - Rede: http://[SEU_IP]:8080
) else (
    echo ❌ Servidor nao esta respondendo!
    echo Verifique se o container esta rodando.
)

echo.
pause
