@echo off
echo ========================================
echo    Adicionar Usuario - Modo Simples
echo ========================================
echo.

set /p USERNAME="Digite o nome de usuario: "
set /p PASSWORD="Digite a senha: "
set /p EMAIL="Digite o email (ou Enter para %USERNAME%@drivelocal.local): "

if "%EMAIL%"=="" set EMAIL=%USERNAME%@drivelocal.local

echo.
echo ========================================
echo    Gerando Hash da Senha...
echo ========================================
echo.

REM Parar container temporariamente para acessar o arquivo
echo Parando container...
docker-compose down >nul 2>&1

REM Verificar se arquivo existe
if not exist "server-go\server\data\users.json" (
    echo ERRO: Arquivo users.json nao encontrado!
    echo Criando arquivo...
    mkdir "server-go\server\data" 2>nul
    echo [] > "server-go\server\data\users.json"
)

echo.
echo IMPORTANTE: Voce precisa gerar um hash bcrypt da senha.
echo.
echo Opcao 1 - Online (Mais facil):
echo   1. Acesse: https://bcrypt-generator.com/
echo   2. Cole a senha: %PASSWORD%
echo   3. Rounds: 10
echo   4. Copie o hash gerado
echo.
echo Opcao 2 - Via PowerShell (se tiver bcrypt instalado):
echo   [Nao disponivel - use opcao 1]
echo.
set /p HASH="Cole o hash bcrypt gerado: "

if "%HASH%"=="" (
    echo ERRO: Hash nao pode estar vazio!
    pause
    exit /b 1
)

echo.
echo Adicionando usuario ao arquivo...
echo.

REM Ler arquivo JSON atual
powershell -Command "$users = Get-Content 'server-go\server\data\users.json' | ConvertFrom-Json; $newUser = @{id=[System.Guid]::NewGuid().ToString('N'); username='%USERNAME%'; password='%HASH%'; email='%EMAIL%'; role='user'; permissions=@('read'); createdAt=[DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ'); active=$true; resetToken=$null; resetTokenExpiry=$null}; $users += $newUser; $users | ConvertTo-Json -Depth 10 | Set-Content 'server-go\server\data\users.json'"

if %errorlevel% equ 0 (
    echo.
    echo ✅ Usuario adicionado com sucesso!
    echo.
    echo Reiniciando container...
    docker-compose up -d
    echo.
    echo ========================================
    echo    Pronto!
    echo ========================================
    echo.
    echo Usuario criado:
    echo   Username: %USERNAME%
    echo   Email: %EMAIL%
    echo   Senha: %PASSWORD%
    echo.
    echo Faca login com essas credenciais!
    echo.
) else (
    echo.
    echo ❌ ERRO ao adicionar usuario!
    echo Verifique o arquivo users.json manualmente.
    echo.
)

pause

