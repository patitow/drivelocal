const { spawn } = require('child_process');
const fs = require('fs-extra');
const path = require('path');

async function setup() {
  console.log('🚀 DriveLocal - Setup Completo\n');
  
  try {
    // 1. Verificar Go
    console.log('1️⃣ Verificando Go...');
    try {
      await runCommand('go', ['version'], { stdio: 'pipe' });
      console.log('   ✅ Go instalado\n');
    } catch (error) {
      console.log('   ❌ Go não encontrado!');
      console.log('   📥 Instale em: https://golang.org/dl/\n');
      process.exit(1);
    }
    
    // 2. Verificar Node.js
    console.log('2️⃣ Verificando Node.js...');
    try {
      await runCommand('node', ['--version'], { stdio: 'pipe' });
      console.log('   ✅ Node.js instalado\n');
    } catch (error) {
      console.log('   ❌ Node.js não encontrado!');
      console.log('   📥 Instale em: https://nodejs.org/\n');
      process.exit(1);
    }
    
    // 3. Instalar dependências do frontend
    console.log('3️⃣ Instalando dependências do frontend...');
    await runCommand('npm', ['install'], { 
      cwd: path.join(process.cwd(), 'client') 
    });
    console.log('   ✅ Dependências do frontend instaladas\n');
    
    // 4. Instalar dependências do Go
    console.log('4️⃣ Instalando dependências do Go...');
    await runCommand('go', ['mod', 'download'], {
      cwd: path.join(process.cwd(), 'server-go')
    });
    console.log('   ✅ Dependências do Go instaladas\n');
    
    // 5. Criar .env se não existir
    console.log('5️⃣ Configurando variáveis de ambiente...');
    const envPath = path.join(process.cwd(), '.env');
    const envExamplePath = path.join(process.cwd(), '.env.example');
    
    if (!fs.existsSync(envPath)) {
      if (fs.existsSync(envExamplePath)) {
        await fs.copy(envExamplePath, envPath);
        console.log('   ✅ Arquivo .env criado a partir de .env.example\n');
      } else {
        console.log('   ⚠️  Arquivo .env.example não encontrado\n');
      }
    } else {
      console.log('   ✅ Arquivo .env já existe\n');
    }
    
    // 6. Criar diretórios necessários
    console.log('6️⃣ Criando diretórios...');
    const dirs = [
      path.join(process.cwd(), 'dist'),
      path.join(process.cwd(), 'server-go', 'server', 'data'),
      path.join(process.cwd(), 'server-go', 'server', 'thumbnails'),
    ];
    
    for (const dir of dirs) {
      await fs.ensureDir(dir);
    }
    console.log('   ✅ Diretórios criados\n');
    
    console.log('✅ Setup completo!\n');
    console.log('📋 Próximos passos:');
    console.log('   npm run dev      # Desenvolvimento');
    console.log('   npm run prod     # Produção');
    console.log('   npm run docker:setup  # Preparar Docker\n');
    
  } catch (error) {
    console.error('❌ Setup falhou:', error);
    process.exit(1);
  }
}

function runCommand(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const process = spawn(command, args, {
      stdio: options.stdio || 'inherit',
      shell: true,
      ...options
    });
    
    process.on('close', (code) => {
      if (code === 0) {
        resolve();
      } else {
        reject(new Error(`Command failed with code ${code}`));
      }
    });
    
    process.on('error', reject);
  });
}

setup().catch(console.error);




