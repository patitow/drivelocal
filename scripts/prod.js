const { spawn } = require('child_process');
const { killPorts } = require('./kill-port');
const fs = require('fs-extra');
const path = require('path');

const PORT = process.env.PORT || '80';

async function startProd() {
  console.log('🚀 Starting DriveLocal in PRODUCTION mode (Go Server)...\n');
  console.log('⚠️  Note: Port 80 requires administrator privileges on Windows');
  console.log('   If you encounter permission errors, run as administrator\n');
  
  try {
    // Kill any existing processes on our port
    await killPorts([parseInt(PORT)]);
    
    console.log('📦 Building client...');
    
    // Install and build client
    console.log('Installing client dependencies...');
    await runCommand('npm', ['install'], { cwd: path.join(process.cwd(), 'client') });
    
    console.log('Building client...');
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
    
    console.log('📦 Building Go server...');
    
    // Build Go server
    await runCommand('go', ['build', '-o', 'drivelocal-server.exe'], {
      cwd: path.join(process.cwd(), 'server-go')
    });
    
    console.log('\n🚀 Starting production server...\n');
    console.log(`🌐 Application will be available at: http://<LAN-IP>/  (set HOST_LAN_IP)`);
    console.log(`🌐 Or locally at: http://localhost/\n`);
    
    // Start Go server
    const serverProcess = spawn('./drivelocal-server.exe', [], {
      cwd: path.join(process.cwd(), 'server-go'),
      stdio: 'inherit',
      shell: true,
      env: { 
        ...process.env, 
        PORT: PORT,
        NODE_ENV: 'production'
      }
    });
    
    // Handle process termination
    process.on('SIGINT', () => {
      console.log('\n🛑 Shutting down production server...');
      serverProcess.kill('SIGINT');
      process.exit(0);
    });
    
    process.on('SIGTERM', () => {
      console.log('\n🛑 Shutting down production server...');
      serverProcess.kill('SIGTERM');
      process.exit(0);
    });
    
    // Keep the process alive
    await new Promise(() => {});
    
  } catch (error) {
    console.error('❌ Production startup failed:', error);
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

startProd().catch(console.error);
