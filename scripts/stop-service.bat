@echo off
echo ========================================
echo    DriveLocal - Parar Servico
echo ========================================
echo.

echo Parando DriveLocal Media Server...
docker-compose down

if %errorlevel% neq 0 (
    echo ERRO: Falha ao parar servico!
    pause
    exit /b 1
)

echo.
echo ✅ Servico parado com sucesso!
echo.
pause
