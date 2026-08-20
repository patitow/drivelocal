@echo off
echo ========================================
echo    DriveLocal - Parar Docker
echo ========================================
echo.

echo Parando container...
docker-compose down

if %errorlevel% equ 0 (
    echo.
    echo ✅ Container parado com sucesso!
) else (
    echo.
    echo ⚠️  Container pode nao estar rodando.
)

echo.
pause

