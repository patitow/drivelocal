@echo off
echo ========================================
echo    Adicionar Usuario ao DriveLocal
echo ========================================
echo.

if "%1"=="" (
    echo Uso: adicionar-usuario.bat ^<username^> ^<password^> [email] [role] [permissions]
    echo.
    echo Exemplos:
    echo   adicionar-usuario.bat joao senha123
    echo   adicionar-usuario.bat maria senha456 maria@email.com user read,write
    echo   adicionar-usuario.bat admin2 admin123 admin2@email.com admin read,write,delete,admin
    echo.
    pause
    exit /b 1
)

set USERNAME=%1
set PASSWORD=%2
set EMAIL=%3
set ROLE=%4
set PERMISSIONS=%5

if "%EMAIL%"=="" set EMAIL=%USERNAME%@drivelocal.local
if "%ROLE%"=="" set ROLE=user
if "%PERMISSIONS%"=="" set PERMISSIONS=read

echo Usuario: %USERNAME%
echo Email: %EMAIL%
echo Role: %ROLE%
echo Permissoes: %PERMISSIONS%
echo.

echo IMPORTANTE: Para adicionar o usuario, voce precisa:
echo 1. Gerar um hash bcrypt da senha
echo 2. Editar o arquivo users.json
echo.
echo Opcao 1 - Gerar hash online:
echo   Acesse: https://bcrypt-generator.com/
echo   Senha: %PASSWORD%
echo   Rounds: 10
echo.
echo Opcao 2 - Usar o servidor Go:
echo   docker-compose exec drivelocal-media-server sh -c "echo '%PASSWORD%' | htpasswd -nBCi 10 usuario"
echo.
echo Depois de obter o hash:
echo 1. Pare o Docker: docker-compose down
echo 2. Edite: server-go\server\data\users.json
echo 3. Adicione o novo usuario ao array
echo 4. Reinicie: docker-compose up -d
echo.
pause

