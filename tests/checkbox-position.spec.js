// @ts-check
const { test, expect } = require('@playwright/test');

test.describe('Checkbox Position Tests', () => {
  test.beforeEach(async ({ page }) => {
    // Navigate to the application
    const appUrl = process.env.TEST_URL || 'http://localhost:80';
    await page.goto(appUrl);
    
    // Wait for the file browser to load
    await page.waitForSelector('[data-testid="file-browser"]', { timeout: 10000 });
  });

  test('checkboxes should be positioned correctly within file items', async ({ page }) => {
    // Get all file items
    const fileItems = page.locator('[data-testid="file-item"]');
    const fileItemsCount = await fileItems.count();
    
    expect(fileItemsCount).toBeGreaterThan(0);
    
    let checkboxPositionCorrect = true;
    let checkboxesFound = 0;
    
    // Check each file item
    for (let i = 0; i < fileItemsCount; i++) {
      const fileItem = fileItems.nth(i);
      const checkbox = fileItem.locator('input[type="checkbox"]').first();
      const checkboxCount = await checkbox.count();
      
      if (checkboxCount > 0) {
        checkboxesFound++;
        
        // Get bounding boxes
        const checkboxBox = await checkbox.boundingBox();
        const parentBox = await fileItem.boundingBox();
        
        if (checkboxBox && parentBox) {
          // Check if checkbox is within the file item bounds
          const isWithinBounds = 
            checkboxBox.x >= parentBox.x &&
            checkboxBox.y >= parentBox.y &&
            checkboxBox.x + checkboxBox.width <= parentBox.x + parentBox.width &&
            checkboxBox.y + checkboxBox.height <= parentBox.y + parentBox.height;
          
          expect(isWithinBounds).toBe(true);
          
          if (!isWithinBounds) {
            checkboxPositionCorrect = false;
          }
        }
      }
    }
    
    expect(checkboxesFound).toBeGreaterThan(0);
    expect(checkboxPositionCorrect).toBe(true);
  });

  test('should not have misplaced checkboxes at the top of the page', async ({ page }) => {
    // Check if there are any checkboxes directly under body (outside file items)
    const topCheckboxes = page.locator('body > input[type="checkbox"]');
    const topCheckboxesCount = await topCheckboxes.count();
    
    expect(topCheckboxesCount).toBe(0);
  });

  test('checkboxes should be functional', async ({ page }) => {
    const firstCheckbox = page.locator('[data-testid="file-item"] input[type="checkbox"]').first();
    const checkboxCount = await firstCheckbox.count();
    
    if (checkboxCount > 0) {
      // Click the checkbox
      await firstCheckbox.click();
      await page.waitForTimeout(500);
      
      // Check if the checkbox is checked
      const isChecked = await firstCheckbox.isChecked();
      expect(isChecked).toBe(true);
      
      // Click again to uncheck
      await firstCheckbox.click();
      await page.waitForTimeout(500);
      
      const isUnchecked = await firstCheckbox.isChecked();
      expect(isUnchecked).toBe(false);
    }
  });
});




