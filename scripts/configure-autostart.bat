@echo off
echo ========================================
echo    DriveLocal - Configurar Autostart
echo ========================================
echo.

echo [1/4] Configurando Docker Desktop para iniciar automaticamente...
echo Verificando se Docker Desktop esta instalado...

if not exist "%ProgramFiles%\Docker\Docker\Docker Desktop.exe" (
    if not exist "%ProgramFiles(x86)%\Docker\Docker\Docker Desktop.exe" (
        echo ERRO: Docker Desktop nao encontrado!
        echo Por favor, instale o Docker Desktop primeiro.
        pause
        exit /b 1
    )
)

echo ✅ Docker Desktop encontrado!

echo.
echo [2/4] Configurando Docker Desktop...
echo Abrindo configuracoes do Docker Desktop...
start "" "%ProgramFiles%\Docker\Docker\Docker Desktop.exe"

echo.
echo INSTRUCOES MANUAIS:
echo 1. No Docker Desktop, va em Settings (Configuracoes)
echo 2. Vá em General
echo 3. Marque "Start Docker Desktop when you log in"
echo 4. Clique em "Apply & Restart"
echo.
echo Pressione qualquer tecla quando terminar...
pause

echo.
echo [3/4] Criando script de inicializacao automatica...
echo Criando script para iniciar DriveLocal automaticamente...

echo @echo off > "%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\DriveLocal-Start.bat"
echo cd /d "%~dp0" >> "%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\DriveLocal-Start.bat"
echo timeout /t 30 /nobreak ^>nul >> "%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\DriveLocal-Start.bat"
echo docker-compose -f "%CD%\docker-compose.yml" up -d >> "%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\DriveLocal-Start.bat"

echo ✅ Script de inicializacao criado!

echo.
echo [4/4] Testando configuracao...
echo Iniciando DriveLocal para teste...
docker-compose up -d

if %errorlevel% neq 0 (
    echo ERRO: Falha ao iniciar DriveLocal!
    pause
    exit /b 1
)

echo.
echo ========================================
echo    Configuracao Concluida!
echo ========================================
echo.
echo ✅ Docker Desktop configurado para autostart
echo ✅ DriveLocal configurado para iniciar automaticamente
echo ✅ Script de inicializacao criado
echo.
echo O DriveLocal iniciara automaticamente:
echo - Quando o Docker Desktop iniciar
echo - Apos 30 segundos do login do Windows
echo.
echo Acesse a aplicacao em:
echo   - Local: http://localhost:8080
echo   - Rede: http://[SEU_IP]:8080
echo.
echo Para remover o autostart:
echo   - Delete o arquivo: %APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\DriveLocal-Start.bat
echo.
pause
