const http = require('http');
const fs = require('fs-extra');
const path = require('path');

const PORT = 8080;
const BASE_URL = `http://localhost:${PORT}`;

async function testUI() {
  console.log('🧪 Testing DriveLocal UI Functionality...\n');
  
  try {
    // Test if server is running
    console.log('📡 Testing server connectivity...');
    const healthResponse = await makeRequest('/api/health');
    
    if (!healthResponse.success) {
      console.log('❌ Server is not running. Please start with: npm run dev');
      return;
    }
    
    console.log('✅ Server is running');
    
    // Test files endpoint
    console.log('\n📁 Testing files endpoint...');
    const filesResponse = await makeRequest('/api/files');
    
    if (!filesResponse.success) {
      console.log('❌ Files endpoint failed');
      return;
    }
    
    const filesData = JSON.parse(filesResponse.data);
    const files = filesData.items || [];
    
    console.log(`✅ Found ${files.length} files`);
    
    if (files.length === 0) {
      console.log('⚠️  No files found. Please add some test files to the media directory.');
      return;
    }
    
    // Test with first few files
    const testFiles = files.slice(0, 3);
    
    for (const file of testFiles) {
      console.log(`\n🔍 Testing file: ${file.name}`);
      console.log(`   Type: ${file.isDirectory ? 'Directory' : file.isVideo ? 'Video' : file.isImage ? 'Image' : 'File'}`);
      console.log(`   Path: ${file.path}`);
      
      if (!file.isDirectory) {
        // Test download
        console.log('   Testing download...');
        const downloadResult = await testDownload(file.path);
        console.log(`   Download: ${downloadResult.success ? '✅' : '❌'} (${downloadResult.size} bytes)`);
        
        // Test streaming for media files
        if (file.isVideo || file.isImage) {
          console.log('   Testing streaming...');
          const streamResult = await testStreaming(file.path);
          console.log(`   Stream: ${streamResult.success ? '✅' : '❌'} (${streamResult.size} bytes)`);
          
          console.log('   Testing thumbnail...');
          const thumbnailResult = await testThumbnail(file.path);
          console.log(`   Thumbnail: ${thumbnailResult.success ? '✅' : '❌'} (${thumbnailResult.size} bytes)`);
        }
      }
    }
    
    console.log('\n✅ UI functionality tests completed!');
    console.log('\n📋 Manual testing checklist:');
    console.log('1. Open http://localhost:8080 in browser');
    console.log('2. Check if checkboxes are visible on files');
    console.log('3. Hover over files to see action buttons');
    console.log('4. Click play button on videos');
    console.log('5. Click download button on files');
    console.log('6. Check browser console for errors');
    
  } catch (error) {
    console.error('❌ Test failed:', error);
  }
}

function makeRequest(endpoint) {
  return new Promise((resolve) => {
    const req = http.get(`${BASE_URL}${endpoint}`, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        resolve({
          success: res.statusCode === 200,
          status: res.statusCode,
          data: data
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

function testDownload(filePath) {
  return new Promise((resolve) => {
    const req = http.get(`${BASE_URL}/api/download/${encodeURIComponent(filePath)}`, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        resolve({
          success: res.statusCode === 200 && data.length > 100,
          status: res.statusCode,
          size: data.length
        });
      });
    });
    
    req.on('error', (err) => {
      resolve({ success: false, error: err.message, size: 0 });
    });
    
    req.setTimeout(15000, () => {
      req.destroy();
      resolve({ success: false, error: 'Timeout', size: 0 });
    });
  });
}

function testStreaming(filePath) {
  return new Promise((resolve) => {
    const req = http.get(`${BASE_URL}/api/stream/${encodeURIComponent(filePath)}`, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        resolve({
          success: res.statusCode === 200 && data.length > 100,
          status: res.statusCode,
          size: data.length
        });
      });
    });
    
    req.on('error', (err) => {
      resolve({ success: false, error: err.message, size: 0 });
    });
    
    req.setTimeout(15000, () => {
      req.destroy();
      resolve({ success: false, error: 'Timeout', size: 0 });
    });
  });
}

function testThumbnail(filePath) {
  return new Promise((resolve) => {
    const req = http.get(`${BASE_URL}/api/thumbnail/${encodeURIComponent(filePath)}`, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        resolve({
          success: res.statusCode === 200 && data.length > 100,
          status: res.statusCode,
          size: data.length
        });
      });
    });
    
    req.on('error', (err) => {
      resolve({ success: false, error: err.message, size: 0 });
    });
    
    req.setTimeout(10000, () => {
      req.destroy();
      resolve({ success: false, error: 'Timeout', size: 0 });
    });
  });
}

testUI().catch(console.error);
