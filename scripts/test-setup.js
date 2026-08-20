const { exec } = require('child_process');
const { promisify } = require('util');
const fs = require('fs-extra');
const path = require('path');

const execAsync = promisify(exec);

async function testSetup() {
  console.log('🧪 Testing DriveLocal Setup...\n');
  
  const tests = [
    {
      name: 'Check Node.js version',
      test: async () => {
        const { stdout } = await execAsync('node --version');
        const version = stdout.trim();
        console.log(`✅ Node.js: ${version}`);
        return version.startsWith('v18') || version.startsWith('v20') || version.startsWith('v22');
      }
    },
    {
      name: 'Check npm version',
      test: async () => {
        const { stdout } = await execAsync('npm --version');
        const version = stdout.trim();
        console.log(`✅ npm: ${version}`);
        return true;
      }
    },
    {
      name: 'Check Docker installation',
      test: async () => {
        try {
          const { stdout } = await execAsync('docker --version');
          console.log(`✅ Docker: ${stdout.trim()}`);
          return true;
        } catch (error) {
          console.log('⚠️  Docker not found (needed for production mode)');
          return false;
        }
      }
    },
    {
      name: 'Check Docker Compose',
      test: async () => {
        try {
          const { stdout } = await execAsync('docker-compose --version');
          console.log(`✅ Docker Compose: ${stdout.trim()}`);
          return true;
        } catch (error) {
          console.log('⚠️  Docker Compose not found (needed for production mode)');
          return false;
        }
      }
    },
    {
      name: 'Check ffmpeg installation',
      test: async () => {
        try {
          const { stdout } = await execAsync('ffmpeg -version');
          console.log(`✅ ffmpeg: Available`);
          return true;
        } catch (error) {
          console.log('⚠️  ffmpeg not found (needed for thumbnails)');
          return false;
        }
      }
    },
    {
      name: 'Check project structure',
      test: async () => {
        const requiredFiles = [
          'package.json',
          'server/index.js',
          'client/package.json',
          'docker-compose.yml',
          'Dockerfile'
        ];
        
        for (const file of requiredFiles) {
          if (!fs.existsSync(file)) {
            console.log(`❌ Missing file: ${file}`);
            return false;
          }
        }
        
        console.log('✅ Project structure: OK');
        return true;
      }
    },
    {
      name: 'Check port availability',
      test: async () => {
        try {
          const { stdout } = await execAsync('netstat -ano | findstr :8080');
          if (stdout.trim()) {
            console.log('⚠️  Port 8080 is in use');
            return false;
          }
          console.log('✅ Port 8080: Available');
          return true;
        } catch (error) {
          console.log('✅ Port 8080: Available');
          return true;
        }
      }
    }
  ];
  
  let passed = 0;
  let total = tests.length;
  
  for (const test of tests) {
    try {
      const result = await test.test();
      if (result) passed++;
    } catch (error) {
      console.log(`❌ ${test.name}: ${error.message}`);
    }
  }
  
  console.log(`\n📊 Test Results: ${passed}/${total} passed`);
  
  if (passed === total) {
    console.log('\n🎉 All tests passed! Ready to run:');
    console.log('   npm run dev    (Development)');
    console.log('   npm run prod   (Production)');
  } else {
    console.log('\n⚠️  Some tests failed. Please check the issues above.');
  }
}

testSetup().catch(console.error);
