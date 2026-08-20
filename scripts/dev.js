const { spawn } = require('child_process');
const { killPorts } = require('./kill-port');
const path = require('path');
const fs = require('fs');

const PORT = process.env.PORT || '3000';
const API_PORT = process.env.API_PORT || '5000';

async function startDev() {
  console.log('🚀 Starting DriveLocal in DEVELOPMENT mode...\n');
  console.log('📦 Frontend: Vite with HMR (Hot Module Replacement)');
  console.log('⚙️  Backend: Go server with file watching\n');
  console.log('✨ Using non-privileged ports (no admin required!)');
  console.log(`   Frontend: ${PORT}, Backend: ${API_PORT}\n`);
  
  try {
    // Check ports (but don't kill - just warn to avoid UAC prompts)
    console.log('🔧 Checking ports...\n');
    try {
      // Skip killing in dev mode to avoid Windows security prompts
      await killPorts([parseInt(PORT), parseInt(API_PORT)], { skipKill: true });
    } catch (error) {
      // Ignore errors - ports might be free or we can't check them
      console.log('⚠️  Could not check ports (continuing anyway)\n');
    }
    
    console.log('📦 Checking dependencies...');
    
    // Check if Go is installed
    try {
      await runCommand('go', ['version'], { 
        cwd: path.join(process.cwd(), 'server-go') 
      });
    } catch (error) {
      console.error('❌ Go is not installed or not in PATH');
      console.error('💡 Install Go from: https://golang.org/dl/');
      process.exit(1);
    }
    
    // Install Go dependencies
    try {
      await runCommand('go', ['mod', 'download'], { 
        cwd: path.join(process.cwd(), 'server-go') 
      });
    } catch (error) {
      console.error('❌ Failed to download Go dependencies');
      console.error('💡 Try running: cd server-go && go mod download');
      process.exit(1);
    }
    
    // Check if client node_modules exists
    const clientNodeModules = path.join(process.cwd(), 'client', 'node_modules');
    if (!fs.existsSync(clientNodeModules)) {
      console.log('📦 Installing frontend dependencies...');
      try {
        await runCommand('npm', ['install'], {
          cwd: path.join(process.cwd(), 'client')
        });
      } catch (error) {
        console.error('❌ Failed to install frontend dependencies');
        console.error('💡 Try running: cd client && npm install');
        process.exit(1);
      }
    }
    
    console.log('\n🔧 Starting development servers...\n');
    console.log(`🌐 Frontend (Vite): http://localhost:${PORT}/`);
    console.log(`🌐 Backend API: http://localhost:${API_PORT}/api`);
    console.log(`🌐 Network: http://<LAN-IP>:${PORT}/  (set HOST_LAN_IP)\n`);
    console.log('✨ Hot reload enabled for both frontend and backend!\n');
    
    let serverProcess = null;
    let serverRestarting = false;
    let restartTimeout = null;
    
    // Function to start/restart Go server
    const startGoServer = async () => {
      if (serverRestarting) return;
      serverRestarting = true;
      
      if (serverProcess) {
        console.log('🔄 Restarting Go server due to file changes...\n');
        try {
          serverProcess.kill('SIGTERM');
        } catch (e) {
          // Ignore if process already dead
        }
        
        // Wait for process to die
        await new Promise(resolve => setTimeout(resolve, 1500));
        
        // Force kill if still alive
        if (serverProcess) {
          try {
            serverProcess.kill('SIGKILL');
          } catch (e) {
            // Ignore
          }
          await new Promise(resolve => setTimeout(resolve, 500));
        }
        serverProcess = null;
      }
      
      console.log(`🚀 Starting Go server on port ${API_PORT}...`);
      const newServerProcess = spawn('go', ['run', '.'], {
        cwd: path.join(process.cwd(), 'server-go'),
        stdio: 'inherit',
        shell: false, // Don't use shell to avoid Windows File Picker UI Host
        env: { 
          ...process.env, 
          PORT: API_PORT,
          NODE_ENV: 'development'
        }
      });
      
      serverProcess = newServerProcess;
      serverRestarting = false;
      
      newServerProcess.on('error', (error) => {
        console.error('❌ Go server spawn error:', error);
        console.error('\n💡 Make sure Go is installed: https://golang.org/dl/');
        serverProcess = null;
        serverRestarting = false;
      });
      
      newServerProcess.on('exit', (code, signal) => {
        // Only log error if it wasn't intentional (SIGTERM/SIGINT)
        if (code !== 0 && code !== null && signal !== 'SIGTERM' && signal !== 'SIGINT') {
          console.error(`❌ Go server exited unexpectedly with code ${code}`);
          // Retry after a delay if it crashed
          setTimeout(() => {
            if (!serverProcess && !serverRestarting) {
              console.log('🔄 Retrying Go server startup...');
              startGoServer();
            }
          }, 2000);
        }
        serverProcess = null;
        serverRestarting = false;
      });
    };
    
    // Watch Go files for changes using fs.watch (native Node.js)
    const watchGoFiles = () => {
      const serverGoPath = path.join(process.cwd(), 'server-go');
      
      const watchDirectory = (dir) => {
        try {
          const watcher = fs.watch(dir, { recursive: true }, (eventType, filename) => {
            if (filename && filename.endsWith('.go')) {
              const filePath = path.join(dir, filename);
              console.log(`\n📝 File changed: ${path.relative(process.cwd(), filePath)}`);
              serverRestarting = true;
              // Debounce restarts
              if (restartTimeout) clearTimeout(restartTimeout);
              restartTimeout = setTimeout(() => {
                startGoServer();
              }, 800); // Delay to avoid multiple rapid restarts
            }
          });
          
          watcher.on('error', (err) => {
            // Ignore errors, continue watching
            if (err.code !== 'ENOENT') {
              console.error('Watcher error:', err.message);
            }
          });
        } catch (err) {
          // If recursive watching fails (older Node versions), watch individual files
          if (err.code === 'ERR_FEATURE_UNAVAILABLE_ON_PLATFORM') {
            console.log('⚠️  Recursive file watching not available, using simple watch');
            // Fallback: just watch the main directory
            fs.watch(serverGoPath, (eventType, filename) => {
              if (filename && filename.endsWith('.go')) {
                console.log(`\n📝 File changed: ${filename}`);
                serverRestarting = true;
                if (restartTimeout) clearTimeout(restartTimeout);
                restartTimeout = setTimeout(() => {
                  startGoServer();
                }, 800);
              }
            });
          }
        }
      };
      
      watchDirectory(serverGoPath);
    };
    
    watchGoFiles();
    
    // Start Go server initially
    await startGoServer();
    
    // Wait a moment for Go server to start before starting Vite
    console.log('⏳ Waiting for Go server to initialize...\n');
    await new Promise(resolve => setTimeout(resolve, 2000));
    
    // Start Vite dev server for frontend
    console.log(`🚀 Starting Vite dev server on port ${PORT}...`);
    const viteProcess = spawn('npm', ['run', 'dev'], {
      cwd: path.join(process.cwd(), 'client'),
      stdio: 'inherit',
      shell: false, // Don't use shell to avoid Windows File Picker UI Host
      windowsHide: true, // Hide window to prevent UAC prompts
      env: {
        ...process.env,
        PORT: PORT,
        VITE_API_URL: `http://localhost:${API_PORT}`
      }
    });
    
    // Handle process termination
    const shutdown = () => {
      console.log('\n🛑 Shutting down servers...');
      if (restartTimeout) clearTimeout(restartTimeout);
      
      // Kill Go server
      if (serverProcess) {
        try {
          serverProcess.kill('SIGTERM');
        } catch (e) {
          // Ignore
        }
      }
      
      // Kill Vite server
      if (viteProcess) {
        try {
          viteProcess.kill('SIGTERM');
        } catch (e) {
          // Ignore
        }
      }
      
      // Force kill after delay
      setTimeout(() => {
        if (serverProcess) {
          try {
            serverProcess.kill('SIGKILL');
          } catch (e) {
            // Ignore
          }
        }
        if (viteProcess) {
          try {
            viteProcess.kill('SIGKILL');
          } catch (e) {
            // Ignore
          }
        }
        
        // Also kill any remaining processes on our ports
        killPorts([parseInt(PORT), parseInt(API_PORT)]).finally(() => {
          process.exit(0);
        });
      }, 2000);
    };
    
    process.on('SIGINT', shutdown);
    process.on('SIGTERM', shutdown);
    
    // Note: Error handlers are now in startGoServer() and here for viteProcess
    viteProcess.on('error', (error) => {
      console.error('❌ Vite server spawn error:', error);
      console.error('\n💡 Make sure Node.js dependencies are installed: cd client && npm install');
    });
    
    viteProcess.on('exit', (code, signal) => {
      if (code !== 0 && code !== null) {
        console.error(`❌ Vite server exited with code ${code}`);
      }
    });
    
    // Keep the process alive
    await new Promise(() => {});
    
  } catch (error) {
    console.error('❌ Development startup failed:', error);
    process.exit(1);
  }
}

function runCommand(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const process = spawn(command, args, {
      stdio: 'inherit',
      shell: false, // Don't use shell to avoid Windows File Picker UI Host
      windowsHide: true, // Hide window to prevent UAC prompts
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

startDev().catch(console.error);
