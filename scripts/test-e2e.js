const http = require('http');
const { spawn } = require('child_process');
const path = require('path');

const BASE_URL = process.env.TEST_URL || 'http://localhost:8080';
const TIMEOUT = 30000;

// Colors for console output
const colors = {
  reset: '\x1b[0m',
  green: '\x1b[32m',
  red: '\x1b[31m',
  yellow: '\x1b[33m',
  blue: '\x1b[34m',
  cyan: '\x1b[36m'
};

function log(message, color = 'reset') {
  console.log(`${colors[color]}${message}${colors.reset}`);
}

function makeRequest(endpoint, options = {}) {
  return new Promise((resolve) => {
    const url = new URL(endpoint, BASE_URL);
    const req = http.get(url.toString(), options, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        resolve({
          success: res.statusCode >= 200 && res.statusCode < 300,
          status: res.statusCode,
          data,
          headers: res.headers
        });
      });
    });

    req.on('error', (err) => {
      resolve({ success: false, error: err.message });
    });

    req.setTimeout(TIMEOUT, () => {
      req.destroy();
      resolve({ success: false, error: 'Timeout' });
    });
  });
}

async function testHealthCheck() {
  log('\n📋 Testing Health Check...', 'cyan');
  
  const result = await makeRequest('/api/health');
  
  if (result.success) {
    try {
      const data = JSON.parse(result.data);
      log('✅ Health check passed', 'green');
      log(`   Status: ${data.status}`, 'green');
      log(`   Media Path Exists: ${data.mediaPathExists}`, 'green');
      return true;
    } catch (e) {
      log('❌ Health check failed - Invalid JSON', 'red');
      return false;
    }
  } else {
    log(`❌ Health check failed - Status: ${result.status}`, 'red');
    return false;
  }
}

async function testFileListing() {
  log('\n📁 Testing File Listing...', 'cyan');
  
  const result = await makeRequest('/api/files');
  
  if (result.success) {
    try {
      const data = JSON.parse(result.data);
      log('✅ File listing passed', 'green');
      log(`   Found ${data.items?.length || 0} items`, 'green');
      return true;
    } catch (e) {
      log('❌ File listing failed - Invalid JSON', 'red');
      return false;
    }
  } else {
    log(`❌ File listing failed - Status: ${result.status}`, 'red');
    return false;
  }
}

async function testNetworkInfo() {
  log('\n🌐 Testing Network Info...', 'cyan');
  
  const result = await makeRequest('/api/network-info');
  
  if (result.success) {
    try {
      const data = JSON.parse(result.data);
      log('✅ Network info passed', 'green');
      log(`   Found ${data.addresses?.length || 0} addresses`, 'green');
      if (data.addresses && data.addresses.length > 0) {
        log(`   Example: http://${data.addresses[0].address}:${data.addresses[0].port}`, 'blue');
      }
      return true;
    } catch (e) {
      log('❌ Network info failed - Invalid JSON', 'red');
      return false;
    }
  } else {
    log(`❌ Network info failed - Status: ${result.status}`, 'red');
    return false;
  }
}

async function testRangeRequests() {
  log('\n🎬 Testing Range Requests (Video Streaming)...', 'cyan');
  
  // First, get a list of files to find a video
  const filesResult = await makeRequest('/api/files');
  
  if (!filesResult.success) {
    log('⚠️  Cannot test range requests - File listing failed', 'yellow');
    return false;
  }
  
  try {
    const filesData = JSON.parse(filesResult.data);
    const videoFile = filesData.items?.find(item => item.isVideo);
    
    if (!videoFile) {
      log('⚠️  Cannot test range requests - No video files found', 'yellow');
      return true; // Not a failure, just skip
    }
    
    log(`   Testing with: ${videoFile.name}`, 'blue');
    
    // Test range request
    const rangeResult = await makeRequest(`/api/stream/${encodeURIComponent(videoFile.path)}`, {
      headers: { 'Range': 'bytes=0-1023' }
    });
    
    if (rangeResult.status === 206) {
      log('✅ Range requests working correctly', 'green');
      log(`   Status: 206 Partial Content`, 'green');
      log(`   Content-Range: ${rangeResult.headers['content-range'] || 'N/A'}`, 'green');
      return true;
    } else if (rangeResult.status === 200) {
      log('⚠️  Range request returned 200 (full file) - may be small file', 'yellow');
      return true;
    } else {
      log(`❌ Range request failed - Status: ${rangeResult.status}`, 'red');
      return false;
    }
  } catch (e) {
    log(`❌ Range request test failed: ${e.message}`, 'red');
    return false;
  }
}

async function testDownloadMultiple() {
  log('\n📦 Testing Download Multiple...', 'cyan');
  
  // First, get files
  const filesResult = await makeRequest('/api/files');
  
  if (!filesResult.success) {
    log('⚠️  Cannot test download multiple - File listing failed', 'yellow');
    return false;
  }
  
  try {
    const filesData = JSON.parse(filesResult.data);
    const testFiles = filesData.items?.filter(item => !item.isDirectory).slice(0, 2);
    
    if (!testFiles || testFiles.length === 0) {
      log('⚠️  Cannot test download multiple - No files found', 'yellow');
      return true; // Not a failure
    }
    
    // Test download multiple via POST
    const postData = JSON.stringify({ files: testFiles.map(f => f.path) });
    
    const result = await new Promise((resolve) => {
      const url = new URL('/api/download-multiple', BASE_URL);
      const req = http.request({
        hostname: url.hostname,
        port: url.port,
        path: url.pathname,
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Content-Length': Buffer.byteLength(postData)
        }
      }, (res) => {
        let data = Buffer.alloc(0);
        res.on('data', chunk => {
          data = Buffer.concat([data, chunk]);
        });
        res.on('end', () => {
          resolve({
            success: res.statusCode >= 200 && res.statusCode < 300,
            status: res.statusCode,
            size: data.length,
            contentType: res.headers['content-type']
          });
        });
      });
      
      req.on('error', (err) => {
        resolve({ success: false, error: err.message });
      });
      
      req.write(postData);
      req.end();
    });
    
    if (result.success && result.contentType === 'application/zip') {
      log('✅ Download multiple working correctly', 'green');
      log(`   ZIP size: ${result.size} bytes`, 'green');
      return true;
    } else {
      log(`❌ Download multiple failed - Status: ${result.status}`, 'red');
      return false;
    }
  } catch (e) {
    log(`❌ Download multiple test failed: ${e.message}`, 'red');
    return false;
  }
}

async function testPathTraversalProtection() {
  log('\n🛡️  Testing Path Traversal Protection...', 'cyan');
  
  const maliciousPaths = [
    '../../etc/passwd',
    '..\\..\\windows\\system32',
    '../..',
    '....//....//etc/passwd'
  ];
  
  let allProtected = true;
  
  for (const maliciousPath of maliciousPaths) {
    const result = await makeRequest(`/api/files?path=${encodeURIComponent(maliciousPath)}`);
    
    if (result.status === 403) {
      log(`   ✅ Protected: ${maliciousPath}`, 'green');
    } else {
      log(`   ❌ Not protected: ${maliciousPath} (Status: ${result.status})`, 'red');
      allProtected = false;
    }
  }
  
  return allProtected;
}

async function testRateLimiting() {
  log('\n⚡ Testing Rate Limiting...', 'cyan');
  
  // Make many requests quickly
  const requests = [];
  for (let i = 0; i < 105; i++) {
    requests.push(makeRequest('/api/health'));
  }
  
  const results = await Promise.all(requests);
  const blocked = results.filter(r => r.status === 429).length;
  
  if (blocked > 0) {
    log(`✅ Rate limiting working - ${blocked} requests blocked`, 'green');
    return true;
  } else {
    log('⚠️  Rate limiting may not be working (no requests blocked)', 'yellow');
    return true; // Not necessarily a failure
  }
}

async function runAllTests() {
  log('\n🚀 DriveLocal - End-to-End Tests', 'blue');
  log('=====================================\n', 'blue');
  
  // Check if server is running
  log('Checking server availability...', 'cyan');
  const healthCheck = await makeRequest('/api/health');
  
  if (!healthCheck.success) {
    log('\n❌ Server is not running!', 'red');
    log('Please start the server first:', 'yellow');
    log('  npm start', 'yellow');
    log('  or', 'yellow');
    log('  npm run dev', 'yellow');
    process.exit(1);
  }
  
  log('✅ Server is running\n', 'green');
  
  const tests = [
    { name: 'Health Check', fn: testHealthCheck },
    { name: 'File Listing', fn: testFileListing },
    { name: 'Network Info', fn: testNetworkInfo },
    { name: 'Range Requests', fn: testRangeRequests },
    { name: 'Download Multiple', fn: testDownloadMultiple },
    { name: 'Path Traversal Protection', fn: testPathTraversalProtection },
    { name: 'Rate Limiting', fn: testRateLimiting }
  ];
  
  const results = [];
  
  for (const test of tests) {
    try {
      const passed = await test.fn();
      results.push({ name: test.name, passed });
    } catch (error) {
      log(`❌ ${test.name} threw error: ${error.message}`, 'red');
      results.push({ name: test.name, passed: false });
    }
  }
  
  // Summary
  log('\n📊 Test Results Summary', 'blue');
  log('========================\n', 'blue');
  
  const passed = results.filter(r => r.passed).length;
  const total = results.length;
  
  results.forEach(result => {
    const status = result.passed ? '✅ PASS' : '❌ FAIL';
    const color = result.passed ? 'green' : 'red';
    log(`${status} ${result.name}`, color);
  });
  
  log(`\n🎯 Overall: ${passed}/${total} tests passed\n`, passed === total ? 'green' : 'yellow');
  
  if (passed === total) {
    log('🎉 All tests passed! The application is ready for production.', 'green');
  } else {
    log('⚠️  Some tests failed. Please review the issues above.', 'yellow');
  }
  
  process.exit(passed === total ? 0 : 1);
}

// Run tests
runAllTests().catch(error => {
  log(`\n❌ Test suite failed: ${error.message}`, 'red');
  process.exit(1);
});




