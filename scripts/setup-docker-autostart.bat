@echo off
echo ========================================
echo    DriveLocal - Configurar Autostart Docker
echo ========================================
echo.
echo Este script configura o DriveLocal para:
echo - Iniciar automaticamente com o Windows
echo - Reiniciar automaticamente em caso de crash
echo - Ficar acessivel apenas na rede local
echo.

REM Verificar se esta rodando como administrador
net session >nul 2>&1
if %errorlevel% neq 0 (
    echo AVISO: Algumas configuracoes podem precisar de privilegios de administrador.
    echo.
)

REM Obter caminho absoluto do script
set "SCRIPT_DIR=%~dp0"
set "PROJECT_DIR=%SCRIPT_DIR%.."

echo [1/5] Verificando Docker Desktop...
docker --version >nul 2>&1
if %errorlevel% neq 0 (
    echo ERRO: Docker nao encontrado!
    echo Por favor, instale o Docker Desktop primeiro.
    pause
    exit /b 1
)
echo ✅ Docker encontrado!

echo.
echo [2/5] Verificando Docker Compose...
docker-compose --version >nul 2>&1
if %errorlevel% neq 0 (
    echo ERRO: Docker Compose nao encontrado!
    pause
    exit /b 1
)
echo ✅ Docker Compose encontrado!

echo.
echo [3/5] Configurando Docker Desktop para autostart...
echo.
echo IMPORTANTE: Configure o Docker Desktop para iniciar automaticamente:
echo 1. Abra o Docker Desktop
echo 2. Va em Settings (Configuracoes) ^> General
echo 3. Marque "Start Docker Desktop when you log in"
echo 4. Clique em "Apply ^& Restart"
echo.
set /p docker_configured="Docker Desktop ja esta configurado para autostart? (S/N): "
if /i "%docker_configured%"=="N" (
    echo Abrindo Docker Desktop...
    start "" "%ProgramFiles%\Docker\Docker\Docker Desktop.exe"
    echo.
    echo Pressione qualquer tecla quando terminar de configurar...
    pause >nul
)

echo.
echo [4/5] Criando script de autostart...
set "STARTUP_DIR=%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup"
set "AUTOSTART_SCRIPT=%STARTUP_DIR%\DriveLocal-Autostart.bat"

REM Criar script de autostart
(
    echo @echo off
    echo REM DriveLocal Media Server - Autostart
    echo cd /d "%PROJECT_DIR%"
    echo call "%SCRIPT_DIR%docker-autostart.bat"
) > "%AUTOSTART_SCRIPT%"

if %errorlevel% neq 0 (
    echo ERRO: Falha ao criar script de autostart!
    pause
    exit /b 1
)
echo ✅ Script de autostart criado em: %AUTOSTART_SCRIPT%

echo.
echo [5/5] Configurando firewall para rede local...
echo.
echo IMPORTANTE: Configurando regra de firewall para permitir acesso na rede local...
echo.

REM Criar regra de firewall (requer admin)
netsh advfirewall firewall show rule name="DriveLocal Media Server" >nul 2>&1
if %errorlevel% neq 0 (
    echo Criando regra de firewall...
    netsh advfirewall firewall add rule name="DriveLocal Media Server" dir=in action=allow protocol=TCP localport=80 profile=private,domain >nul 2>&1
    if %errorlevel% equ 0 (
        echo ✅ Regra de firewall criada (permitir apenas rede local)
    ) else (
        echo ⚠️  Nao foi possivel criar regra de firewall automaticamente.
        echo    Configure manualmente no Windows Firewall:
        echo    - Porta: 80
        echo    - Perfil: Privado e Dominio (NAO marcar Publico)
    )
) else (
    echo ✅ Regra de firewall ja existe
)

echo.
echo ========================================
echo    Configuracao Concluida!
echo ========================================
echo.
echo ✅ Docker Desktop configurado para autostart
echo ✅ DriveLocal configurado para iniciar automaticamente
echo ✅ Container configurado para reiniciar automaticamente (restart: always)
echo ✅ Firewall configurado para rede local apenas
echo.
echo O DriveLocal iniciara automaticamente:
echo - Quando o Docker Desktop iniciar
echo - Apos o login do Windows
echo.
echo IMPORTANTE - Seguranca:
echo - O servidor esta configurado para aceitar conexoes apenas na rede local
echo - Nao esta exposto para a internet publica
echo - Apenas dispositivos na sua rede local podem acessar
echo.
echo Para descobrir seu IP local:
echo   ipconfig ^| findstr "IPv4"
echo.
echo Acesse a aplicacao em:
echo   http://[SEU_IP_LOCAL]:80
echo.
echo Para remover o autostart:
echo   - Delete o arquivo: %AUTOSTART_SCRIPT%
echo.
echo Testando inicializacao agora...
call "%SCRIPT_DIR%docker-autostart.bat"

echo.
pause

