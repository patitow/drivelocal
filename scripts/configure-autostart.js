#!/usr/bin/env node

const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

console.log('🔧 DriveLocal - Configurando Autostart...\n');

// Criar script de inicialização
const startupScript = `@echo off
cd /d "${process.cwd()}"
timeout /t 30 /nobreak >nul
docker-compose up -d`;

const startupPath = path.join(os.homedir(), 'AppData', 'Roaming', 'Microsoft', 'Windows', 'Start Menu', 'Programs', 'Startup', 'DriveLocal-Start.bat');

try {
  fs.writeFileSync(startupPath, startupScript);
  console.log('✅ Script de inicialização criado');
} catch (error) {
  console.log('❌ Falha ao criar script de inicialização');
  console.log('Erro:', error.message);
  process.exit(1);
}

console.log('\n📋 Instruções para Docker Desktop:');
console.log('1. Abra o Docker Desktop');
console.log('2. Vá em Settings (Configurações)');
console.log('3. Vá em General');
console.log('4. Marque "Start Docker Desktop when you log in"');
console.log('5. Clique em "Apply & Restart"');

console.log('\n✅ Configuração concluída!');
console.log('\nO DriveLocal iniciará automaticamente:');
console.log('- Quando o Docker Desktop iniciar');
console.log('- Após 30 segundos do login do Windows');
console.log('\nPara remover autostart, delete o arquivo:');
console.log(startupPath);
