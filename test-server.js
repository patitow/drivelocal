const http = require('http');

const PORT = 8080;
const BASE_URL = `http://localhost:${PORT}`;

async function testEndpoint(endpoint, description) {
  return new Promise((resolve) => {
    const req = http.get(`${BASE_URL}${endpoint}`, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        console.log(`✅ ${description}: ${res.statusCode}`);
        if (res.statusCode !== 200) {
          console.log(`   Error: ${data}`);
        }
        resolve({ status: res.statusCode, data });
      });
    });
    
    req.on('error', (err) => {
      console.log(`❌ ${description}: Connection failed - ${err.message}`);
      resolve({ status: 0, error: err.message });
    });
    
    req.setTimeout(5000, () => {
      console.log(`⏰ ${description}: Timeout`);
      req.destroy();
      resolve({ status: 0, error: 'Timeout' });
    });
  });
}

async function runTests() {
  console.log('🧪 Testing Media Server...\n');
  
  const tests = [
    { endpoint: '/api/health', description: 'Health Check' },
    { endpoint: '/api/network-info', description: 'Network Info' },
    { endpoint: '/api/files', description: 'Files List' },
  ];
  
  for (const test of tests) {
    await testEndpoint(test.endpoint, test.description);
  }
  
  console.log('\n✅ Server tests completed!');
  console.log('\n📋 Next steps:');
  console.log('1. Start the server: npm start');
  console.log('2. Build the client: npm run build');
  console.log('3. Test the application: http://localhost:8080');
}

runTests().catch(console.error);

