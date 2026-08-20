const http = require('http');
const fs = require('fs-extra');
const path = require('path');

const PORT = 8080;
const BASE_URL = `http://localhost:${PORT}`;

async function testEndpoint(endpoint, description, expectedStatus = 200) {
  return new Promise((resolve) => {
    const req = http.get(`${BASE_URL}${endpoint}`, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        const success = res.statusCode === expectedStatus;
        console.log(`${success ? '✅' : '❌'} ${description}: ${res.statusCode}`);
        
        if (!success) {
          console.log(`   Error: ${data.substring(0, 200)}...`);
        }
        
        resolve({ 
          success, 
          status: res.statusCode, 
          data: data.substring(0, 500),
          size: data.length 
        });
      });
    });
    
    req.on('error', (err) => {
      console.log(`❌ ${description}: Connection failed - ${err.message}`);
      resolve({ success: false, error: err.message });
    });
    
    req.setTimeout(10000, () => {
      console.log(`⏰ ${description}: Timeout`);
      req.destroy();
      resolve({ success: false, error: 'Timeout' });
    });
  });
}

async function testFileDownload(filePath) {
  return new Promise((resolve) => {
    const req = http.get(`${BASE_URL}/api/download/${encodeURIComponent(filePath)}`, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        const success = res.statusCode === 200 && data.length > 1000; // More than 1KB
        console.log(`${success ? '✅' : '❌'} Download ${filePath}: ${res.statusCode} (${data.length} bytes)`);
        
        if (!success) {
          console.log(`   Error: ${data.substring(0, 200)}...`);
        }
        
        resolve({ 
          success, 
          status: res.statusCode, 
          size: data.length,
          data: data.substring(0, 100)
        });
      });
    });
    
    req.on('error', (err) => {
      console.log(`❌ Download ${filePath}: Connection failed - ${err.message}`);
      resolve({ success: false, error: err.message });
    });
    
    req.setTimeout(15000, () => {
      console.log(`⏰ Download ${filePath}: Timeout`);
      req.destroy();
      resolve({ success: false, error: 'Timeout' });
    });
  });
}

async function testStreaming(filePath) {
  return new Promise((resolve) => {
    const req = http.get(`${BASE_URL}/api/stream/${encodeURIComponent(filePath)}`, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        const success = res.statusCode === 200 && data.length > 1000; // More than 1KB
        console.log(`${success ? '✅' : '❌'} Stream ${filePath}: ${res.statusCode} (${data.length} bytes)`);
        
        if (!success) {
          console.log(`   Error: ${data.substring(0, 200)}...`);
        }
        
        resolve({ 
          success, 
          status: res.statusCode, 
          size: data.length,
          data: data.substring(0, 100)
        });
      });
    });
    
    req.on('error', (err) => {
      console.log(`❌ Stream ${filePath}: Connection failed - ${err.message}`);
      resolve({ success: false, error: err.message });
    });
    
    req.setTimeout(15000, () => {
      console.log(`⏰ Stream ${filePath}: Timeout`);
      req.destroy();
      resolve({ success: false, error: 'Timeout' });
    });
  });
}

async function testThumbnail(filePath) {
  return new Promise((resolve) => {
    const req = http.get(`${BASE_URL}/api/thumbnail/${encodeURIComponent(filePath)}`, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        const success = res.statusCode === 200 && data.length > 100; // More than 100 bytes
        console.log(`${success ? '✅' : '❌'} Thumbnail ${filePath}: ${res.statusCode} (${data.length} bytes)`);
        
        if (!success) {
          console.log(`   Error: ${data.substring(0, 200)}...`);
        }
        
        resolve({ 
          success, 
          status: res.statusCode, 
          size: data.length,
          data: data.substring(0, 100)
        });
      });
    });
    
    req.on('error', (err) => {
      console.log(`❌ Thumbnail ${filePath}: Connection failed - ${err.message}`);
      resolve({ success: false, error: err.message });
    });
    
    req.setTimeout(10000, () => {
      console.log(`⏰ Thumbnail ${filePath}: Timeout`);
      req.destroy();
      resolve({ success: false, error: 'Timeout' });
    });
  });
}

async function runFunctionalityTests() {
  console.log('🧪 Testing DriveLocal Functionality...\n');
  
  // Test basic endpoints
  console.log('📡 Testing basic endpoints...');
  const healthTest = await testEndpoint('/api/health', 'Health Check');
  const networkTest = await testEndpoint('/api/network-info', 'Network Info');
  const filesTest = await testEndpoint('/api/files', 'Files List');
  
  if (!healthTest.success) {
    console.log('\n❌ Server is not running. Please start the server first:');
    console.log('   npm run dev');
    return;
  }
  
  console.log('\n📁 Testing file operations...');
  
  // Test files endpoint to get available files
  if (filesTest.success) {
    try {
      const filesData = JSON.parse(filesTest.data);
      const files = filesData.items || [];
      
      console.log(`Found ${files.length} files in directory`);
      
      // Test with first few files
      const testFiles = files.slice(0, 3);
      
      for (const file of testFiles) {
        if (!file.isDirectory) {
          console.log(`\nTesting file: ${file.name}`);
          
          // Test download
          await testFileDownload(file.path);
          
          // Test streaming (for videos/images)
          if (file.isVideo || file.isImage) {
            await testStreaming(file.path);
            await testThumbnail(file.path);
          }
        }
      }
    } catch (error) {
      console.log('❌ Error parsing files data:', error.message);
    }
  }
  
  console.log('\n✅ Functionality tests completed!');
  console.log('\n📋 Next steps:');
  console.log('1. Check the browser console for client-side errors');
  console.log('2. Verify file paths are correct');
  console.log('3. Test the UI interactions manually');
}

runFunctionalityTests().catch(console.error);
