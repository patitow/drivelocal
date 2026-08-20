#!/usr/bin/env node

const { execSync } = require('child_process');
const http = require('http');

console.log('📊 DriveLocal - Verificando Status...\n');

// Verificar containers
console.log('🐳 Status dos containers:');
try {
  execSync('docker-compose ps', { stdio: 'inherit' });
} catch (error) {
  console.log('❌ Erro ao verificar containers');
}

// Verificar API
console.log('\n🌐 Testando API...');
const options = {
  hostname: 'localhost',
  port: 8080,
  path: '/api/health',
  method: 'GET',
  timeout: 5000
};

const req = http.request(options, (res) => {
  if (res.statusCode === 200) {
    console.log('✅ API respondendo corretamente');
    console.log('\n🎉 DriveLocal está funcionando!');
    console.log('\nAcesse a aplicação em:');
    console.log('  - Local: http://localhost:8080');
    console.log('  - Rede: http://[SEU_IP]:8080');
  } else {
    console.log('❌ API não está respondendo corretamente');
  }
});

req.on('error', (error) => {
  console.log('❌ API não está respondendo');
  console.log('Verifique se o container está rodando: npm run docker:status');
});

req.on('timeout', () => {
  console.log('❌ Timeout ao conectar com a API');
});

req.end();
