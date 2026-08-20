const { spawn } = require('child_process');
const { promisify } = require('util');

const execAsync = promisify(require('child_process').exec);

async function runTest(testName, command) {
  console.log(`\n🧪 Running ${testName}...`);
  console.log(`Command: ${command}`);
  
  try {
    const { stdout, stderr } = await execAsync(command, { 
      timeout: 30000,
      cwd: process.cwd() 
    });
    
    if (stderr && !stderr.includes('warning')) {
      console.log(`⚠️  ${testName} warnings:`, stderr);
    }
    
    console.log(`✅ ${testName} completed`);
    return { success: true, stdout, stderr };
  } catch (error) {
    console.log(`❌ ${testName} failed:`, error.message);
    return { success: false, error: error.message };
  }
}

async function testAll() {
  console.log('🚀 DriveLocal - Complete Test Suite');
  console.log('=====================================\n');
  
  const tests = [
    {
      name: 'Setup Test',
      command: 'npm run test:setup'
    },
    {
      name: 'Functionality Test', 
      command: 'npm run test:functionality'
    },
    {
      name: 'UI Test',
      command: 'npm run test:ui'
    }
  ];
  
  const results = [];
  
  for (const test of tests) {
    const result = await runTest(test.name, test.command);
    results.push({ ...test, result });
  }
  
  console.log('\n📊 Test Results Summary');
  console.log('========================');
  
  let passed = 0;
  let total = results.length;
  
  for (const test of results) {
    const status = test.result.success ? '✅ PASS' : '❌ FAIL';
    console.log(`${status} ${test.name}`);
    if (test.result.success) passed++;
  }
  
  console.log(`\n🎯 Overall: ${passed}/${total} tests passed`);
  
  if (passed === total) {
    console.log('\n🎉 All tests passed! The application is ready to use.');
    console.log('\n📋 Next steps:');
    console.log('1. Start development: npm run dev');
    console.log('2. Open browser: http://localhost:8080');
    console.log('3. Test the UI manually');
  } else {
    console.log('\n⚠️  Some tests failed. Please check the issues above.');
    console.log('\n🔧 Troubleshooting:');
    console.log('1. Make sure the server is running: npm run dev');
    console.log('2. Check if ports 8080 and 3000 are free');
    console.log('3. Verify media directory exists and has files');
    console.log('4. Check browser console for client-side errors');
  }
}

testAll().catch(console.error);
