@echo off
echo ========================================
echo    DriveLocal - Remover Autostart
echo ========================================
echo.

set "STARTUP_DIR=%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup"
set "AUTOSTART_SCRIPT=%STARTUP_DIR%\DriveLocal-Autostart.bat"

if exist "%AUTOSTART_SCRIPT%" (
    echo Removendo script de autostart...
    del "%AUTOSTART_SCRIPT%"
    if %errorlevel% equ 0 (
        echo ✅ Script de autostart removido!
    ) else (
        echo ERRO: Falha ao remover script de autostart!
        pause
        exit /b 1
    )
) else (
    echo ⚠️  Script de autostart nao encontrado.
)

echo.
echo Removendo regra de firewall...
netsh advfirewall firewall delete rule name="DriveLocal Media Server" >nul 2>&1
if %errorlevel% equ 0 (
    echo ✅ Regra de firewall removida!
) else (
    echo ⚠️  Regra de firewall nao encontrada ou nao foi possivel remover.
)

echo.
echo ========================================
echo    Autostart Removido!
echo ========================================
echo.
echo O DriveLocal nao iniciara mais automaticamente.
echo Para iniciar manualmente, use: docker-compose up -d
echo.
pause

