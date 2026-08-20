@echo off
echo 🚀 DriveLocal Media Server - Quick Start
echo.

echo Escolha o modo de execução:
echo 1. Desenvolvimento (Local)
echo 2. Produção (Docker)
echo 3. Matar processos nas portas
echo 4. Sair
echo.

set /p choice="Digite sua escolha (1-4): "

if "%choice%"=="1" (
    echo.
    echo 🔧 Iniciando em modo DESENVOLVIMENTO...
    npm run dev
) else if "%choice%"=="2" (
    echo.
    echo 🐳 Iniciando em modo PRODUÇÃO (Docker)...
    npm run prod
) else if "%choice%"=="3" (
    echo.
    echo 💀 Matando processos nas portas 8080 e 3000...
    npm run kill-ports
    pause
) else if "%choice%"=="4" (
    echo.
    echo 👋 Saindo...
    exit /b 0
) else (
    echo.
    echo ❌ Opção inválida!
    pause
    goto :eof
)

pause