@echo off
REM ========================================
REM    DriveLocal - Autostart Script
REM    Este script inicia o DriveLocal automaticamente
REM    e garante que ele reinicie em caso de crash
REM ========================================

set "PATH=C:\Program Files\Docker\Docker\resources\bin;%PATH%"

REM Mudar para o diretório do projeto
cd /d "%~dp0\.."

REM Aguardar Docker Desktop iniciar (máximo 2 minutos)
echo Aguardando Docker Desktop iniciar...
set /a count=0
:wait_docker
docker info >nul 2>&1
if %errorlevel% equ 0 (
    echo Docker Desktop esta rodando!
    goto docker_ready
)
set /a count+=1
if %count% geq 120 (
    echo ERRO: Docker Desktop nao iniciou apos 2 minutos!
    echo Por favor, inicie o Docker Desktop manualmente.
    exit /b 1
)
timeout /t 1 /nobreak >nul
goto wait_docker

:docker_ready
echo.
echo Iniciando DriveLocal Media Server...

REM Detect current private IPv4 for network-info (container cannot see the host NIC)
for /f "usebackq delims=" %%I in (`powershell -NoProfile -Command "Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.IPAddress -like '192.168.*' -and $_.PrefixOrigin -ne 'WellKnown' } | Sort-Object -Property IPAddress | Select-Object -First 1 -ExpandProperty IPAddress"`) do set "HOST_LAN_IP=%%I"
if not defined HOST_LAN_IP (
  for /f "usebackq delims=" %%I in (`powershell -NoProfile -Command "Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.IPAddress -like '10.*' } | Select-Object -First 1 -ExpandProperty IPAddress"`) do set "HOST_LAN_IP=%%I"
)
if defined HOST_LAN_IP (
  echo HOST_LAN_IP=%HOST_LAN_IP%
)

REM Garantir que o helper de montagem de pastas (host) esteja rodando
REM (ignora erro se a tarefa nao existir)
schtasks /Run /TN "DriveLocal Mount Helper" >nul 2>&1

REM Construir imagem se necessário
docker compose build --quiet

REM Iniciar container (restart: always garante reinicio automatico)
docker compose up -d

if %errorlevel% neq 0 (
    echo ERRO: Falha ao iniciar DriveLocal!
    exit /b 1
)

REM Verificar se container esta rodando
timeout /t 5 /nobreak >nul
docker ps | findstr "drivelocal-media-server" >nul
if %errorlevel% equ 0 (
    echo.
    echo ========================================
    echo    DriveLocal iniciado com sucesso!
    echo ========================================
    echo.
    echo O servidor esta acessivel na sua rede local em:
    echo   http://%HOST_LAN_IP%:80
    echo   http://127.0.0.1:8080
    echo.
) else (
    echo ERRO: Container nao esta rodando!
    echo Verifique os logs com: docker compose logs
    exit /b 1
)

REM Script silencioso - nao pausa
exit /b 0

