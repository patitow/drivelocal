@echo off
echo ========================================
echo    DriveLocal - Testes e Linting
echo ========================================
echo.

echo [1/4] Verificando dependencias...
npm list >nul 2>&1
if %errorlevel% neq 0 (
    echo Instalando dependencias...
    npm install
)

echo.
echo [2/4] Executando linting...
npm run lint
if %errorlevel% neq 0 (
    echo AVISO: Problemas de linting encontrados!
    echo Executando correcao automatica...
    npm run lint:fix
)

echo.
echo [3/4] Executando testes do backend...
npm test
if %errorlevel% neq 0 (
    echo ERRO: Testes do backend falharam!
    pause
    exit /b 1
)

echo.
echo [4/4] Executando testes do frontend...
cd client
npm test
if %errorlevel% neq 0 (
    echo ERRO: Testes do frontend falharam!
    cd ..
    pause
    exit /b 1
)
cd ..

echo.
echo ========================================
echo    Todos os testes passaram!
echo ========================================
echo.
echo Para executar testes em modo watch:
echo   npm run test:watch
echo.
echo Para ver cobertura de testes:
echo   npm run test:coverage
echo.
pause
