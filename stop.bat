@echo off
echo 🛑 DriveLocal Media Server - Stop All
echo.

echo Parando todos os processos...

echo 💀 Matando processos nas portas 8080 e 3000...
npm run kill-ports

echo 🐳 Parando containers Docker...
docker-compose down

echo ✅ Todos os processos foram parados!
pause