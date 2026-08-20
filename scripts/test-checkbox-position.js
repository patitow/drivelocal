const { chromium } = require('playwright');

async function testCheckboxPosition() {
  console.log('🔍 Testing checkbox positioning with Playwright...');
  
  let browser;
  try {
    browser = await chromium.launch({ 
      headless: false,
      args: ['--no-sandbox', '--disable-setuid-sandbox']
    });
    
    const context = await browser.newContext({
      viewport: { width: 1280, height: 720 }
    });
    
    const page = await context.newPage();
    
    // Navigate to the application
    // Use port 80 in production, fallback to 3000 in development
    const appUrl = process.env.TEST_URL || 'http://localhost:80';
    console.log(`📱 Navigating to application: ${appUrl}...`);
    await page.goto(appUrl, { waitUntil: 'networkidle' });
    
    // Wait for the file browser to load
    console.log('⏳ Waiting for file browser to load...');
    await page.waitForSelector('[data-testid="file-browser"]', { timeout: 10000 });
    
    // Check if checkboxes are positioned correctly within file items
    console.log('🔍 Checking checkbox positioning...');
    
    // Get all file items
    const fileItems = await page.locator('[data-testid="file-item"]').all();
    console.log(`📁 Found ${fileItems.length} file items`);
    
    let checkboxPositionCorrect = true;
    let checkboxesFound = 0;
    
    for (let i = 0; i < fileItems.length; i++) {
      const fileItem = fileItems[i];
      
      // Check if this file item has a checkbox
      const checkbox = fileItem.locator('input[type="checkbox"]').first();
      const checkboxCount = await checkbox.count();
      
      if (checkboxCount > 0) {
        checkboxesFound++;
        
        // Get the position of the checkbox relative to its parent
        const checkboxBox = await checkbox.boundingBox();
        const parentBox = await fileItem.boundingBox();
        
        if (checkboxBox && parentBox) {
          // Check if checkbox is within the file item bounds
          const isWithinBounds = 
            checkboxBox.x >= parentBox.x &&
            checkboxBox.y >= parentBox.y &&
            checkboxBox.x + checkboxBox.width <= parentBox.x + parentBox.width &&
            checkboxBox.y + checkboxBox.height <= parentBox.y + parentBox.height;
          
          if (!isWithinBounds) {
            console.log(`❌ Checkbox ${i + 1} is not properly positioned within file item`);
            checkboxPositionCorrect = false;
          } else {
            console.log(`✅ Checkbox ${i + 1} is properly positioned within file item`);
          }
        }
      }
    }
    
    // Check if there are any checkboxes at the top of the page (outside file items)
    console.log('🔍 Checking for misplaced checkboxes...');
    const topCheckboxes = await page.locator('body > input[type="checkbox"]').count();
    if (topCheckboxes > 0) {
      console.log(`❌ Found ${topCheckboxes} checkboxes at the top of the page (outside file items)`);
      checkboxPositionCorrect = false;
    } else {
      console.log('✅ No misplaced checkboxes found at the top of the page');
    }
    
    // Test checkbox functionality
    console.log('🔍 Testing checkbox functionality...');
    const firstCheckbox = page.locator('[data-testid="file-item"] input[type="checkbox"]').first();
    const firstCheckboxCount = await firstCheckbox.count();
    
    if (firstCheckboxCount > 0) {
      // Click the checkbox
      await firstCheckbox.click();
      await page.waitForTimeout(500);
      
      // Check if the checkbox is checked
      const isChecked = await firstCheckbox.isChecked();
      if (isChecked) {
        console.log('✅ Checkbox functionality works correctly');
      } else {
        console.log('❌ Checkbox functionality failed');
        checkboxPositionCorrect = false;
      }
    }
    
    // Summary
    console.log('\n📊 Test Results:');
    console.log(`📁 File items found: ${fileItems.length}`);
    console.log(`☑️ Checkboxes found: ${checkboxesFound}`);
    console.log(`📍 Checkbox positioning: ${checkboxPositionCorrect ? '✅ CORRECT' : '❌ INCORRECT'}`);
    console.log(`🔧 Checkbox functionality: ${checkboxPositionCorrect ? '✅ WORKING' : '❌ BROKEN'}`);
    
    if (checkboxPositionCorrect) {
      console.log('\n🎉 SUCCESS: Checkbox positioning is correct!');
      return true;
    } else {
      console.log('\n💥 FAILURE: Checkbox positioning issues detected!');
      return false;
    }
    
  } catch (error) {
    console.error('❌ Test failed:', error);
    return false;
  } finally {
    if (browser) {
      await browser.close();
    }
  }
}

// Run the test
testCheckboxPosition().then(success => {
  if (success) {
    console.log('\n✅ Checkbox position test completed successfully!');
    process.exit(0);
  } else {
    console.log('\n❌ Checkbox position test failed!');
    process.exit(1);
  }
}).catch(error => {
  console.error('❌ Test execution failed:', error);
  process.exit(1);
});