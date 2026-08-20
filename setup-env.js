const fs = require('fs-extra');
const path = require('path');

console.log('🔧 Configurando arquivo .env...\n');

const envExamplePath = path.join(__dirname, '.env.example');
const envPath = path.join(__dirname, '.env');

// Conteúdo do .env.example
const envExampleContent = `# DriveLocal - Configuração de Ambiente
# Copie este arquivo para .env e configure suas variáveis
# 
# IMPORTANTE: O arquivo .env não deve ser commitado no git!
# Ele contém informações sensíveis como senhas.

# ============================================
# AUTENTICAÇÃO
# ============================================
# Autenticação está habilitada por padrão
# Para desabilitar, defina como false (não recomendado)
AUTH_ENABLED=true

# Tipo de autenticação: 'token' ou 'basic'
# Token é mais flexível e recomendado
AUTH_TYPE=token

# Credenciais de acesso
# ⚠️ ALTERE ESTAS CREDENCIAIS EM PRODUÇÃO!
AUTH_USERNAME=admin
AUTH_PASSWORD=admin

# ============================================
# SERVIDOR
# ============================================
# Porta do servidor (80 é a porta padrão HTTP)
# Use 8080 apenas para desenvolvimento local
PORT=80

# ============================================
# MÍDIA
# ============================================
# Caminho para a pasta de mídia (relativo ou absoluto)
# Windows: D:\\\\Videos
# Linux/Mac: /media/videos
MEDIA_PATH=./media

# ============================================
# AMBIENTE
# ============================================
# Ambiente: development, production, test
NODE_ENV=production
`;

// Criar ou atualizar .env.example se não existir ou estiver vazio
if (!fs.existsSync(envExamplePath) || fs.statSync(envExamplePath).size === 0) {
  console.log('📝 Criando/atualizando arquivo .env.example...');
  fs.writeFileSync(envExamplePath, envExampleContent);
  console.log('✅ Arquivo .env.example criado!\n');
}

// Verificar se .env já existe
if (fs.existsSync(envPath)) {
  console.log('✅ Arquivo .env já existe!');
  console.log('   Se precisar recriar, delete o arquivo .env e execute novamente.\n');
} else {
  createEnvFile();
}

function createEnvFile() {
  try {
    // Copiar .env.example para .env
    fs.copyFileSync(envExamplePath, envPath);
    console.log('✅ Arquivo .env criado com sucesso!');
    console.log('\n📝 Próximos passos:');
    console.log('1. Edite o arquivo .env e configure suas credenciais');
    console.log('2. Altere AUTH_USERNAME e AUTH_PASSWORD');
    console.log('3. Configure MEDIA_PATH se necessário');
    console.log('4. Execute: npm start\n');
  } catch (error) {
    console.error('❌ Erro ao criar arquivo .env:', error.message);
    process.exit(1);
  }
}

