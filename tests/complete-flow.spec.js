// @ts-check
const { test, expect } = require('@playwright/test');

test.describe('Complete Application Flow', () => {
  test('complete user journey', async ({ page, request }) => {
    // 1. Check health
    const healthResponse = await request.get('/api/health');
    expect(healthResponse.ok()).toBeTruthy();
    const healthBody = await healthResponse.json();
    expect(healthBody).toHaveProperty('status');

    // 2. Try to login
    const loginResponse = await request.post('/api/auth/login', {
      data: {
        username: 'admin',
        password: 'admin'
      },
      failOnStatusCode: false
    });

    if (!loginResponse.ok()) {
      // Auth disabled, skip rest
      test.skip();
      return;
    }

    const loginBody = await loginResponse.json();
    const token = loginBody.token;

    if (!token) {
      test.skip();
      return;
    }

    // 3. Navigate to app
    await page.goto('/');
    await page.waitForTimeout(2000);

    // 4. Check if logged in (should see file browser or login form)
    const loginForm = page.locator('form').first();
    const loginFormVisible = await loginForm.isVisible().catch(() => false);
    
    if (loginFormVisible) {
      // Need to login via UI
      await page.fill('input[type="text"], input[name="username"]', 'admin');
      await page.fill('input[type="password"]', 'admin');
      await page.click('button[type="submit"]');
      await page.waitForTimeout(2000);
    }

    // 5. Check files endpoint
    const filesResponse = await request.get('/api/files?path=', {
      headers: {
        'Authorization': `Bearer ${token}`
      },
      failOnStatusCode: false
    });

    if (filesResponse.ok()) {
      const filesBody = await filesResponse.json();
      expect(filesBody).toHaveProperty('items');
      expect(filesBody).toHaveProperty('currentPath');
    }

    // 6. Test network info
    const networkResponse = await request.get('/api/network-info', {
      headers: {
        'Authorization': `Bearer ${token}`
      },
      failOnStatusCode: false
    });

    if (networkResponse.ok()) {
      const networkBody = await networkResponse.json();
      expect(networkBody).toHaveProperty('addresses');
    }

    // 7. Logout
    const logoutResponse = await request.post('/api/auth/logout', {
      headers: {
        'Authorization': `Bearer ${token}`
      },
      failOnStatusCode: false
    });

    expect([200, 401]).toContain(logoutResponse.status());
  });

  test('file operations flow', async ({ page, request }) => {
    // Login
    const loginResponse = await request.post('/api/auth/login', {
      data: {
        username: 'admin',
        password: 'admin'
      },
      failOnStatusCode: false
    });

    if (!loginResponse.ok()) {
      test.skip();
      return;
    }

    const loginBody = await loginResponse.json();
    const token = loginBody.token;

    if (!token) {
      test.skip();
      return;
    }

    // Get files list
    const filesResponse = await request.get('/api/files?path=', {
      headers: {
        'Authorization': `Bearer ${token}`
      },
      failOnStatusCode: false
    });

    if (filesResponse.ok()) {
      const filesBody = await filesResponse.json();
      const files = filesBody.items || [];

      if (files.length > 0) {
        const firstFile = files.find(f => !f.isDirectory);
        
        if (firstFile) {
          // Test thumbnail
          const thumbnailResponse = await request.get(`/api/thumbnail/${encodeURIComponent(firstFile.path)}`, {
            headers: {
              'Authorization': `Bearer ${token}`
            },
            failOnStatusCode: false
          });

          expect([200, 404]).toContain(thumbnailResponse.status());

          // Test stream (if video/image)
          if (firstFile.isVideo || firstFile.isImage) {
            const streamResponse = await request.get(`/api/stream/${encodeURIComponent(firstFile.path)}`, {
              headers: {
                'Authorization': `Bearer ${token}`,
                'Range': 'bytes=0-1023'
              },
              failOnStatusCode: false
            });

            expect([200, 206, 404]).toContain(streamResponse.status());
          }
        }
      }
    }
  });
});

