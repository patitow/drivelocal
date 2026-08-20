@echo off
echo ========================================
echo    DriveLocal - Iniciar Servico
echo ========================================
echo.

echo Iniciando DriveLocal Media Server...
docker-compose up -d

if %errorlevel% neq 0 (
    echo ERRO: Falha ao iniciar servico!
    pause
    exit /b 1
)

echo.
echo ✅ Servico iniciado com sucesso!
echo.
echo Acesse a aplicacao em:
echo   - Local: http://localhost:8080
echo   - Rede: http://[SEU_IP]:8080
echo.
echo Para ver os logs:
echo   docker-compose logs -f
echo.
pause
