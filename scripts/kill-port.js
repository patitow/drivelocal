const { exec } = require('child_process');
const { promisify } = require('util');

const execAsync = promisify(exec);

async function killPort(port, options = {}) {
  const { skipKill = false } = options; // Skip killing processes (just check)
  
  try {
    console.log(`🔍 Checking for processes on port ${port}...`);
    
    // Windows command to find processes on port
    // Use cmd.exe explicitly to avoid File Picker UI Host (required for pipes)
    const findCommand = `netstat -ano | findstr :${port}`;
    const result = await execAsync(findCommand, { 
      windowsHide: true,
      shell: 'cmd.exe' // Use cmd.exe explicitly to avoid File Picker UI Host
    });
    
    if (result.stdout.trim()) {
      console.log(`⚠️  Found processes on port ${port}:`);
      console.log(result.stdout);
      
      // Extract PIDs and kill them
      const lines = result.stdout.split('\n').filter(line => line.trim());
      const pids = new Set();
      
      for (const line of lines) {
        const parts = line.trim().split(/\s+/);
        if (parts.length >= 5) {
          const pid = parts[parts.length - 1];
          if (pid && pid !== '0') {
            pids.add(pid);
          }
        }
      }
      
      if (pids.size > 0) {
        if (skipKill) {
          // In dev mode: just warn, don't kill (avoids UAC prompts)
          console.log(`⚠️  Port ${port} is in use by processes: ${Array.from(pids).join(', ')}`);
          console.log(`💡 Skipping kill to avoid UAC prompts.`);
          console.log(`   If you get port conflicts, close these processes manually or restart.\n`);
        } else {
          // Try to kill only if explicitly requested (production mode)
          console.log(`💀 Attempting to kill processes: ${Array.from(pids).join(', ')}`);
          
          for (const pid of pids) {
            try {
              // Try graceful termination first (without /F to avoid UAC)
              await execAsync(`taskkill /PID ${pid}`, { 
                timeout: 2000,
                windowsHide: true,
                shell: 'cmd.exe' // Use cmd.exe explicitly to avoid File Picker UI Host
              });
              console.log(`✅ Terminated process ${pid} gracefully`);
            } catch (error) {
              // If graceful fails, skip force kill to avoid UAC prompts
              console.log(`⚠️  Could not kill process ${pid} (may require admin or already closed)`);
            }
          }
          
          await new Promise(resolve => setTimeout(resolve, 1000));
        }
      }
    } else {
      console.log(`✅ Port ${port} is free`);
    }
  } catch (error) {
    if (error.message.includes('findstr')) {
      console.log(`✅ Port ${port} is free (no processes found)`);
    } else {
      console.error(`❌ Error checking port ${port}:`, error.message);
    }
  }
}

async function killPorts(ports, options = {}) {
  console.log('🔧 Checking for port conflicts...\n');
  
  for (const port of ports) {
    await killPort(port, options);
  }
  
  console.log('\n✅ Port conflict check completed');
}

// If called directly
if (require.main === module) {
  const ports = process.argv.slice(2).map(Number).filter(p => !isNaN(p));
  if (ports.length === 0) {
    console.log('Usage: node scripts/kill-port.js <port1> <port2> ...');
    console.log('Example: node scripts/kill-port.js 8080 3000');
    process.exit(1);
  }
  
  killPorts(ports).catch(console.error);
}

module.exports = { killPort, killPorts };
