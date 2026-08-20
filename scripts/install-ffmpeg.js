const { exec } = require('child_process');
const { promisify } = require('util');

const execAsync = promisify(exec);

async function installFFmpeg() {
  console.log('🔧 Installing FFmpeg for thumbnail generation...\n');
  
  try {
    // Check if ffmpeg is already installed
    try {
      await execAsync('ffmpeg -version');
      console.log('✅ FFmpeg is already installed!');
      return;
    } catch (error) {
      console.log('❌ FFmpeg not found, installing...');
    }

    // Try different installation methods
    const installMethods = [
      {
        name: 'Chocolatey',
        command: 'choco install ffmpeg -y',
        check: 'choco --version'
      },
      {
        name: 'Winget',
        command: 'winget install ffmpeg',
        check: 'winget --version'
      },
      {
        name: 'Scoop',
        command: 'scoop install ffmpeg',
        check: 'scoop --version'
      }
    ];

    for (const method of installMethods) {
      try {
        console.log(`🔍 Trying ${method.name}...`);
        await execAsync(method.check);
        console.log(`📦 Installing FFmpeg via ${method.name}...`);
        await execAsync(method.command);
        
        // Verify installation
        await execAsync('ffmpeg -version');
        console.log(`✅ FFmpeg installed successfully via ${method.name}!`);
        return;
      } catch (error) {
        console.log(`❌ ${method.name} failed: ${error.message}`);
        continue;
      }
    }

    console.log('\n⚠️  Automatic installation failed. Please install FFmpeg manually:');
    console.log('   1. Download from: https://ffmpeg.org/download.html');
    console.log('   2. Or install via Chocolatey: choco install ffmpeg');
    console.log('   3. Or install via Winget: winget install ffmpeg');
    console.log('\n📝 Note: Thumbnails will use fallback (original files) until FFmpeg is installed.');

  } catch (error) {
    console.error('❌ Error installing FFmpeg:', error.message);
    console.log('\n📝 Note: Thumbnails will use fallback (original files) until FFmpeg is installed.');
  }
}

if (require.main === module) {
  installFFmpeg();
}

module.exports = { installFFmpeg };

