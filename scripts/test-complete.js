#!/usr/bin/env node

const { execSync } = require('child_process');
const http = require('http');

console.log('🧪 DriveLocal - Teste Completo...\n');

// Verificar Docker
console.log('1️⃣ Verificando Docker...');
try {
  execSync('docker --version', { stdio: 'pipe' });
  console.log('✅ Docker funcionando');
} catch (error) {
  console.log('❌ Docker não encontrado');
  process.exit(1);
}

// Verificar Docker Compose
console.log('\n2️⃣ Verificando Docker Compose...');
try {
  execSync('docker-compose --version', { stdio: 'pipe' });
  console.log('✅ Docker Compose funcionando');
} catch (error) {
  console.log('❌ Docker Compose não encontrado');
  process.exit(1);
}

// Construir imagem
console.log('\n3️⃣ Construindo imagem...');
try {
  execSync('docker-compose build', { stdio: 'inherit' });
  console.log('✅ Imagem construída');
} catch (error) {
  console.log('❌ Falha ao construir imagem');
  process.exit(1);
}

// Iniciar container
console.log('\n4️⃣ Iniciando container...');
try {
  execSync('docker-compose up -d', { stdio: 'inherit' });
  console.log('✅ Container iniciado');
} catch (error) {
  console.log('❌ Falha ao iniciar container');
  process.exit(1);
}

// Aguardar inicialização
console.log('\n5️⃣ Aguardando inicialização (30s)...');
await new Promise(resolve => setTimeout(resolve, 30000));

// Testar API
console.log('\n6️⃣ Testando API...');
const options = {
  hostname: 'localhost',
  port: 8080,
  path: '/api/health',
  method: 'GET',
  timeout: 5000
};

const req = http.request(options, (res) => {
  if (res.statusCode === 200) {
    console.log('✅ API respondendo');
    console.log('\n🎉 TESTE COMPLETO: SUCESSO!');
    console.log('\nAcesse a aplicação em:');
    console.log('  - Local: http://localhost:8080');
    console.log('  - Rede: http://[SEU_IP]:8080');
  } else {
    console.log('❌ API não está respondendo');
  }
});

req.on('error', (error) => {
  console.log('❌ API não está respondendo');
  console.log('Aguarde mais alguns segundos e execute: npm run check:status');
});

req.on('timeout', () => {
  console.log('❌ Timeout ao conectar com a API');
});

req.end();
