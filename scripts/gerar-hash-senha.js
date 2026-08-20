// Script para gerar hash bcrypt de senha
const bcrypt = require('bcrypt');

const senha = process.argv[2] || 'changeme';

bcrypt.hash(senha, 10, (err, hash) => {
  if (err) {
    console.error('Erro ao gerar hash:', err);
    process.exit(1);
  }
  console.log(`Senha: ${senha}`);
  console.log(`Hash: ${hash}`);
});

