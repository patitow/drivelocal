const http = require('http');
const { promisify } = require('util');

const execAsync = promisify(require('child_process').exec);

const PORT = 8080;
const BASE_URL = `http://localhost:${PORT}`;

async function makeRequest(endpoint) {
  return new Promise((resolve) => {
    const req = http.get(`${BASE_URL}${endpoint}`, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        resolve({
          success: res.statusCode === 200,
          status: res.statusCode,
          data: data,
          size: data.length
        });
      });
    });
    
    req.on('error', (err) => {
      resolve({ success: false, error: err.message });
    });
    
    req.setTimeout(10000, () => {
      req.destroy();
      resolve({ success: false, error: 'Timeout' });
    });
  });
}

async function testEncodingDecoding() {
  console.log('🔍 Testing Encoding/Decoding fixes...\n');
  
  try {
    // Test files endpoint to get files with special characters
    const filesResponse = await makeRequest('/api/files');
    
    if (!filesResponse.success) {
      console.log('❌ Files endpoint failed');
      return;
    }
    
    const filesData = JSON.parse(filesResponse.data);
    const files = filesData.items || [];
    
    console.log(`Found ${files.length} files`);
    
    // Look for files with special characters
    const specialCharFiles = files.filter(file => 
      !file.isDirectory && (
        file.name.includes(' ') || 
        file.name.includes('%') || 
        file.name.includes('\\') ||
        file.name.includes('+') ||
        file.name.includes('#')
      )
    );
    
    console.log(`Found ${specialCharFiles.length} files with special characters`);
    
    if (specialCharFiles.length > 0) {
      console.log('\nTesting files with special characters:');
      
      for (const file of specialCharFiles.slice(0, 3)) {
        console.log(`\n📁 Testing: ${file.name}`);
        console.log(`   Path: ${file.path}`);
        
        // Test download
        const downloadResult = await makeRequest(`/api/download/${encodeURIComponent(file.path)}`);
        console.log(`   Download: ${downloadResult.success ? '✅' : '❌'} (${downloadResult.size} bytes)`);
        
        if (!downloadResult.success) {
          console.log(`   Error: ${downloadResult.data.substring(0, 200)}`);
        }
        
        // Test streaming for media files
        if (file.isVideo || file.isImage) {
          const streamResult = await makeRequest(`/api/stream/${encodeURIComponent(file.path)}`);
          console.log(`   Stream: ${streamResult.success ? '✅' : '❌'} (${streamResult.size} bytes)`);
          
          if (!streamResult.success) {
            console.log(`   Error: ${streamResult.data.substring(0, 200)}`);
          }
          
          const thumbnailResult = await makeRequest(`/api/thumbnail/${encodeURIComponent(file.path)}`);
          console.log(`   Thumbnail: ${thumbnailResult.success ? '✅' : '❌'} (${thumbnailResult.size} bytes)`);
          
          if (!thumbnailResult.success) {
            console.log(`   Error: ${thumbnailResult.data.substring(0, 200)}`);
          }
        }
      }
    } else {
      console.log('⚠️  No files with special characters found for testing');
    }
    
  } catch (error) {
    console.error('❌ Encoding test failed:', error);
  }
}

async function testBreadcrumbs() {
  console.log('\n🍞 Testing Breadcrumbs...\n');
  
  try {
    // Test different path levels
    const testPaths = [
      '',
      'folder1',
      'folder1/subfolder',
      'folder1/subfolder/deep'
    ];
    
    for (const testPath of testPaths) {
      console.log(`Testing path: "${testPath}"`);
      
      const response = await makeRequest(`/api/files?path=${encodeURIComponent(testPath)}`);
      
      if (response.success) {
        const data = JSON.parse(response.data);
        console.log(`   ✅ Path accessible: ${data.items?.length || 0} items`);
      } else {
        console.log(`   ❌ Path failed: ${response.status}`);
      }
    }
    
  } catch (error) {
    console.error('❌ Breadcrumbs test failed:', error);
  }
}

async function testImageVideoFunctionality() {
  console.log('\n🖼️ Testing Image/Video Functionality...\n');
  
  try {
    const filesResponse = await makeRequest('/api/files');
    
    if (!filesResponse.success) {
      console.log('❌ Files endpoint failed');
      return;
    }
    
    const filesData = JSON.parse(filesResponse.data);
    const files = filesData.items || [];
    
    const mediaFiles = files.filter(file => !file.isDirectory && (file.isVideo || file.isImage));
    
    console.log(`Found ${mediaFiles.length} media files`);
    
    if (mediaFiles.length > 0) {
      for (const file of mediaFiles.slice(0, 2)) {
        console.log(`\n📁 Testing: ${file.name} (${file.isVideo ? 'Video' : 'Image'})`);
        
        // Test streaming
        const streamResult = await makeRequest(`/api/stream/${encodeURIComponent(file.path)}`);
        console.log(`   Stream: ${streamResult.success ? '✅' : '❌'} (${streamResult.size} bytes)`);
        
        if (!streamResult.success) {
          console.log(`   Stream Error: ${streamResult.data.substring(0, 200)}`);
        }
        
        // Test thumbnail
        const thumbnailResult = await makeRequest(`/api/thumbnail/${encodeURIComponent(file.path)}`);
        console.log(`   Thumbnail: ${thumbnailResult.success ? '✅' : '❌'} (${thumbnailResult.size} bytes)`);
        
        if (!thumbnailResult.success) {
          console.log(`   Thumbnail Error: ${thumbnailResult.data.substring(0, 200)}`);
        }
      }
    } else {
      console.log('⚠️  No media files found for testing');
    }
    
  } catch (error) {
    console.error('❌ Media functionality test failed:', error);
  }
}

async function runAllTests() {
  console.log('🧪 DriveLocal - Testing Fixes');
  console.log('==============================\n');
  
  // Test if server is running
  const healthResponse = await makeRequest('/api/health');
  
  if (!healthResponse.success) {
    console.log('❌ Server is not running. Please start with: npm run dev');
    return;
  }
  
  console.log('✅ Server is running\n');
  
  // Run all tests
  await testEncodingDecoding();
  await testBreadcrumbs();
  await testImageVideoFunctionality();
  
  console.log('\n✅ All tests completed!');
  console.log('\n📋 Manual testing checklist:');
  console.log('1. Open http://localhost:8080 in browser');
  console.log('2. Navigate to folders with special characters');
  console.log('3. Test breadcrumbs navigation');
  console.log('4. Click on images to open image viewer');
  console.log('5. Click on videos to open video player');
  console.log('6. Test download of files with special characters');
  console.log('7. Check browser console for any errors');
}

runAllTests().catch(console.error);
