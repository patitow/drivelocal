const fs = require('fs');
const path = require('path');
const crypto = require('crypto');

// Caminho do arquivo de usuários
const usersFile = path.join(__dirname, '..', 'server-go', 'server', 'data', 'users.json');

// Ler argumentos da linha de comando
const args = process.argv.slice(2);
const username = args[0];
const password = args[1];
const email = args[2] || `${username}@drivelocal.local`;
const role = args[3] || 'user';
const permissions = args[4] ? args[4].split(',') : ['read'];

if (!username || !password) {
  console.log('Uso: node scripts/adicionar-usuario.js <username> <password> [email] [role] [permissions]');
  console.log('');
  console.log('Exemplos:');
  console.log('  node scripts/adicionar-usuario.js joao senha123');
  console.log('  node scripts/adicionar-usuario.js maria senha456 maria@email.com user read,write');
  console.log('  node scripts/adicionar-usuario.js admin2 admin123 admin2@email.com admin read,write,delete,admin');
  process.exit(1);
}

// Gerar hash bcrypt (simulado - na verdade vamos usar bcryptjs se disponível)
// Mas como não temos bcryptjs instalado, vamos usar uma abordagem diferente
// Vamos criar um script Go simples ou usar o servidor Go para gerar o hash

console.log('⚠️  Para adicionar usuário, você precisa gerar o hash bcrypt da senha.');
console.log('');
console.log('Opção 1: Use o servidor Go para gerar o hash');
console.log('  1. Pare o Docker: docker-compose down');
console.log('  2. Execute: cd server-go && go run -c "package main; import (\"fmt\"; \"golang.org/x/crypto/bcrypt\"); func main() { hash, _ := bcrypt.GenerateFromPassword([]byte(\"' + password + '\"), 10); fmt.Print(string(hash)) }"');
console.log('');
console.log('Opção 2: Use uma ferramenta online');
console.log('  Acesse: https://bcrypt-generator.com/');
console.log('  Digite a senha: ' + password);
console.log('  Rounds: 10');
console.log('  Copie o hash gerado');
console.log('');
console.log('Opção 3: Edite manualmente o arquivo users.json');
console.log('  Arquivo: ' + usersFile);
console.log('');
console.log('Dados do novo usuário:');
console.log('  Username: ' + username);
console.log('  Email: ' + email);
console.log('  Role: ' + role);
console.log('  Permissões: ' + permissions.join(', '));
console.log('');
console.log('Depois de obter o hash, adicione este objeto ao array em users.json:');
console.log(JSON.stringify({
  id: crypto.randomBytes(16).toString('hex'),
  username: username,
  password: '[COLE_O_HASH_AQUI]',
  email: email,
  role: role,
  permissions: permissions,
  createdAt: new Date().toISOString(),
  active: true,
  resetToken: null,
  resetTokenExpiry: null
}, null, 2));

