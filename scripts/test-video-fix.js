const http = require('http');
const fs = require('fs-extra');
const path = require('path');

const PORT = 8080;
const BASE_URL = `http://localhost:${PORT}`;

async function testVideoDownload(filePath) {
  return new Promise((resolve) => {
    console.log(`🎬 Testing video download: ${filePath}`);
    
    const req = http.get(`${BASE_URL}/api/download/${encodeURIComponent(filePath)}`, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        const success = res.statusCode === 200 && data.length > 1000;
        console.log(`   Download: ${success ? '✅' : '❌'} (${res.statusCode}, ${data.length} bytes)`);
        
        if (!success) {
          console.log(`   Error: ${data.substring(0, 200)}`);
        }
        
        resolve({ success, status: res.statusCode, size: data.length });
      });
    });
    
    req.on('error', (err) => {
      console.log(`   Download: ❌ Connection failed - ${err.message}`);
      resolve({ success: false, error: err.message });
    });
    
    req.setTimeout(15000, () => {
      console.log(`   Download: ❌ Timeout`);
      req.destroy();
      resolve({ success: false, error: 'Timeout' });
    });
  });
}

async function testVideoStreaming(filePath) {
  return new Promise((resolve) => {
    console.log(`🎥 Testing video streaming: ${filePath}`);
    
    const req = http.get(`${BASE_URL}/api/stream/${encodeURIComponent(filePath)}`, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        const success = res.statusCode === 200 && data.length > 1000;
        console.log(`   Stream: ${success ? '✅' : '❌'} (${res.statusCode}, ${data.length} bytes)`);
        
        if (!success) {
          console.log(`   Error: ${data.substring(0, 200)}`);
        }
        
        resolve({ success, status: res.statusCode, size: data.length });
      });
    });
    
    req.on('error', (err) => {
      console.log(`   Stream: ❌ Connection failed - ${err.message}`);
      resolve({ success: false, error: err.message });
    });
    
    req.setTimeout(15000, () => {
      console.log(`   Stream: ❌ Timeout`);
      req.destroy();
      resolve({ success: false, error: 'Timeout' });
    });
  });
}

async function testVideoThumbnail(filePath) {
  return new Promise((resolve) => {
    console.log(`🖼️  Testing video thumbnail: ${filePath}`);
    
    const req = http.get(`${BASE_URL}/api/thumbnail/${encodeURIComponent(filePath)}`, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        const success = res.statusCode === 200 && data.length > 100;
        console.log(`   Thumbnail: ${success ? '✅' : '❌'} (${res.statusCode}, ${data.length} bytes)`);
        
        if (!success) {
          console.log(`   Error: ${data.substring(0, 200)}`);
        }
        
        resolve({ success, status: res.statusCode, size: data.length });
      });
    });
    
    req.on('error', (err) => {
      console.log(`   Thumbnail: ❌ Connection failed - ${err.message}`);
      resolve({ success: false, error: err.message });
    });
    
    req.setTimeout(10000, () => {
      console.log(`   Thumbnail: ❌ Timeout`);
      req.destroy();
      resolve({ success: false, error: 'Timeout' });
    });
  });
}

async function testVideoFunctionality() {
  console.log('🎬 Testing Video Functionality Fixes...\n');
  
  try {
    // Test if server is running
    const healthResponse = await makeRequest('/api/health');
    
    if (!healthResponse.success) {
      console.log('❌ Server is not running. Please start with: npm run dev');
      return;
    }
    
    console.log('✅ Server is running');
    
    // Test files endpoint
    const filesResponse = await makeRequest('/api/files');
    
    if (!filesResponse.success) {
      console.log('❌ Files endpoint failed');
      return;
    }
    
    const filesData = JSON.parse(filesResponse.data);
    const files = filesData.items || [];
    
    console.log(`📁 Found ${files.length} files`);
    
    // Find video files
    const videoFiles = files.filter(file => !file.isDirectory && file.isVideo);
    
    console.log(`🎬 Found ${videoFiles.length} video files`);
    
    if (videoFiles.length === 0) {
      console.log('⚠️  No video files found for testing');
      return;
    }
    
    // Test with first few video files
    const testVideos = videoFiles.slice(0, 3);
    
    for (const video of testVideos) {
      console.log(`\n📹 Testing video: ${video.name}`);
      console.log(`   Path: ${video.path}`);
      console.log(`   Size: ${video.size} bytes`);
      
      // Test download
      const downloadResult = await testVideoDownload(video.path);
      
      // Test streaming
      const streamResult = await testVideoStreaming(video.path);
      
      // Test thumbnail
      const thumbnailResult = await testVideoThumbnail(video.path);
      
      console.log(`\n   📊 Results:`);
      console.log(`   Download: ${downloadResult.success ? '✅' : '❌'}`);
      console.log(`   Stream: ${streamResult.success ? '✅' : '❌'}`);
      console.log(`   Thumbnail: ${thumbnailResult.success ? '✅' : '❌'}`);
    }
    
    console.log('\n✅ Video functionality tests completed!');
    console.log('\n📋 Manual testing checklist:');
    console.log('1. Open http://localhost:8080 in browser');
    console.log('2. Click on video files to test streaming');
    console.log('3. Click download button to test download');
    console.log('4. Check browser console for any errors');
    console.log('5. Verify video player loads and plays correctly');
    
  } catch (error) {
    console.error('❌ Video functionality test failed:', error);
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

testVideoFunctionality().catch(console.error);
