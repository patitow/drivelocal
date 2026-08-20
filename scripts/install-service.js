#!/usr/bin/env node

const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');

console.log('🚀 DriveLocal - Instalando Serviço...\n');

// Verificar Docker
try {
  execSync('docker --version', { stdio: 'pipe' });
  console.log('✅ Docker encontrado');
} catch (error) {
  console.log('❌ Docker não encontrado!');
  console.log('Por favor, instale o Docker Desktop primeiro.');
  process.exit(1);
}

// Verificar Docker Compose
try {
  execSync('docker-compose --version', { stdio: 'pipe' });
  console.log('✅ Docker Compose encontrado');
} catch (error) {
  console.log('❌ Docker Compose não encontrado!');
  console.log('Por favor, instale o Docker Compose primeiro.');
  process.exit(1);
}

// Construir imagem
console.log('\n🔨 Construindo imagem Docker...');
try {
  execSync('docker-compose build', { stdio: 'inherit' });
  console.log('✅ Imagem construída com sucesso');
} catch (error) {
  console.log('❌ Falha ao construir imagem Docker');
  process.exit(1);
}

// Iniciar serviço
console.log('\n🚀 Iniciando serviço...');
try {
  execSync('docker-compose up -d', { stdio: 'inherit' });
  console.log('✅ Serviço iniciado com sucesso');
} catch (error) {
  console.log('❌ Falha ao iniciar serviço');
  process.exit(1);
}

console.log('\n🎉 Instalação concluída!');
console.log('\nAcesse a aplicação em:');
console.log('  - Local: http://localhost:8080');
console.log('  - Rede: http://[SEU_IP]:8080');
console.log('\nPara configurar autostart: npm run configure:autostart');
console.log('Para ver status: npm run check:status');
