@echo off
echo ========================================
echo    DriveLocal - Remover Autostart
echo ========================================
echo.

echo [1/3] Parando servico...
docker-compose down

echo.
echo [2/3] Removendo script de inicializacao...
if exist "%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\DriveLocal-Start.bat" (
    del "%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\DriveLocal-Start.bat"
    echo ✅ Script de inicializacao removido!
) else (
    echo ⚠️  Script de inicializacao nao encontrado.
)

echo.
echo [3/3] Verificando remocao...
if not exist "%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\DriveLocal-Start.bat" (
    echo ✅ Autostart removido com sucesso!
) else (
    echo ❌ Falha ao remover autostart!
)

echo.
echo ========================================
echo    Autostart Removido!
echo ========================================
echo.
echo ✅ Servico parado
echo ✅ Script de inicializacao removido
echo.
echo O DriveLocal nao iniciara mais automaticamente.
echo.
echo Para iniciar manualmente:
echo   start-service.bat
echo.
echo Para reinstalar com autostart:
echo   install-service.bat
echo   configure-autostart.bat
echo.
pause
