const { spawn } = require('child_process');
const fs = require('fs-extra');
const path = require('path');

async function dockerSetup() {
  console.log('🐳 Preparing DriveLocal for Docker...\n');
  
  try {
    // Build client
    console.log('📦 Building client...');
    await runCommand('npm', ['install'], { cwd: path.join(process.cwd(), 'client') });
    await runCommand('npm', ['run', 'build'], { cwd: path.join(process.cwd(), 'client') });
    
    // Ensure dist directory exists
    const distPath = path.join(process.cwd(), 'dist');
    if (!fs.existsSync(distPath)) {
      console.log('Creating dist directory...');
      fs.ensureDirSync(distPath);
    }
    
    // Copy client build to dist
    const clientDistPath = path.join(process.cwd(), 'client', 'dist');
    if (fs.existsSync(clientDistPath)) {
      console.log('Copying client build to dist...');
      await fs.copy(clientDistPath, distPath);
    }
    
    // Build Docker image
    console.log('\n🐳 Building Docker image...');
    await runCommand('docker', ['compose', 'build'], {
      cwd: process.cwd()
    });
    
    console.log('\n✅ Docker setup complete!');
    console.log('\n📋 Next steps:');
    console.log('   docker compose up -d    # Start container');
    console.log('   docker compose logs -f   # View logs');
    console.log('   docker compose down      # Stop container');
    
  } catch (error) {
    console.error('❌ Docker setup failed:', error);
    process.exit(1);
  }
}

function runCommand(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const process = spawn(command, args, {
      stdio: 'inherit',
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

dockerSetup().catch(console.error);




