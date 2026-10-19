@echo off
REM ========================================
REM    DriveLocal - Instala o helper de montagem (host)
REM    Compila host-helper, gera o token no .env e registra
REM    a tarefa agendada "DriveLocal Mount Helper" (logon do
REM    usuario atual, sem privilegios de administrador).
REM ========================================

set "HELPER_DIR=%~dp0..\host-helper"
set "HELPER_EXE=%~dp0..\host-helper\drivelocal-helper.exe"
set "PATH=C:\Program Files\Docker\Docker\resources\bin;%PATH%"

where go >nul 2>&1
if errorlevel 1 (
    echo ERRO: Go nao encontrado no PATH. Instale o Go e tente novamente.
    exit /b 1
)

REM Para a instancia em execucao (senao o exe nao pode ser sobrescrito)
schtasks /End /TN "DriveLocal Mount Helper" >nul 2>&1

echo Compilando helper...
pushd "%HELPER_DIR%"
go build -ldflags "-H=windowsgui" -o drivelocal-helper.exe .
set "BUILD_RC=%errorlevel%"
popd
if not "%BUILD_RC%"=="0" (
    echo ERRO: falha ao compilar o helper.
    exit /b 1
)

echo Gerando token ^(MOUNT_HELPER_TOKEN no .env^)...
"%HELPER_EXE%" init
if errorlevel 1 (
    echo ERRO: falha no init do helper.
    exit /b 1
)

echo Subindo o servidor para receber MOUNT_HELPER_TOKEN...
echo O container so le o token do .env ao ser criado; por isso o compose sobe aqui.
pushd "%~dp0.."
docker compose up -d
set "COMPOSE_RC=%errorlevel%"
popd
if not "%COMPOSE_RC%"=="0" (
    echo ERRO: falha ao rodar docker compose up -d na raiz do projeto.
    exit /b 1
)


echo Registrando tarefa agendada...
set "DL_EXE=%HELPER_EXE%"
set "DL_WD=%HELPER_DIR%"
powershell -NoProfile -ExecutionPolicy Bypass -Command "$user = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name; $action = New-ScheduledTaskAction -Execute $env:DL_EXE -WorkingDirectory $env:DL_WD; $trigger = New-ScheduledTaskTrigger -AtLogOn -User $user; $principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Limited; $settings = New-ScheduledTaskSettingsSet -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries; Register-ScheduledTask -TaskName 'DriveLocal Mount Helper' -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null"
if errorlevel 1 (
    echo ERRO: falha ao registrar a tarefa agendada.
    exit /b 1
)

schtasks /Run /TN "DriveLocal Mount Helper" >nul 2>&1

echo.
echo Helper de montagem instalado. Escutando em 127.0.0.1:47123
echo Tarefa: DriveLocal Mount Helper
exit /b 0
