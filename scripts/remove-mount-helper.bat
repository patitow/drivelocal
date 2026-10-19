@echo off
REM ========================================
REM    DriveLocal - Remove o helper de montagem (host)
REM    Para a instancia em execucao e apaga a tarefa agendada.
REM    Nao apaga host-helper e nao mexe no .env.
REM ========================================

schtasks /End /TN "DriveLocal Mount Helper" >nul 2>&1
schtasks /Delete /TN "DriveLocal Mount Helper" /F >nul 2>&1
if errorlevel 1 (
    echo Tarefa "DriveLocal Mount Helper" nao encontrada ou ja removida.
    exit /b 0
)
echo Tarefa "DriveLocal Mount Helper" removida.
exit /b 0
