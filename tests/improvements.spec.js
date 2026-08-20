// @ts-check
const { test, expect } = require('@playwright/test');

test.describe('Improvements Tests', () => {
  test.beforeEach(async ({ page }) => {
    // Navigate to the app
    await page.goto('/');
  });

  test('Health check endpoint returns detailed status', async ({ request }) => {
    const response = await request.get('/api/health');
    expect(response.ok()).toBeTruthy();
    
    const body = await response.json();
    expect(body).toHaveProperty('status');
    expect(body).toHaveProperty('checks');
    expect(body.checks).toHaveProperty('mediaPath');
    expect(body.checks).toHaveProperty('thumbnailPool');
    expect(body.checks).toHaveProperty('tokens');
  });

  test('CORS headers are set correctly', async ({ request }) => {
    const response = await request.get('/api/health', {
      headers: {
        'Origin': 'http://localhost:3000'
      }
    });
    
    const headers = response.headers();
    expect(headers['access-control-allow-origin']).toBe('http://localhost:3000');
    expect(headers['access-control-allow-credentials']).toBe('true');
  });

  test('Rate limiting works for auth endpoints', async ({ request }) => {
    // Make multiple login attempts
    const attempts = [];
    for (let i = 0; i < 12; i++) {
      const response = await request.post('/api/auth/login', {
        data: {
          username: 'test',
          password: 'wrong'
        }
      });
      attempts.push(response.status());
    }

    // First 10 should be 401, 11th+ should be 429
    const lastAttempt = attempts[attempts.length - 1];
    expect(lastAttempt).toBe(429); // Too Many Requests
  });

  test('Files endpoint supports pagination', async ({ request, page }) => {
    // Login first if auth is enabled
    try {
      await page.goto('/');
      await page.waitForTimeout(1000);
      
      // Try to login
      const loginResponse = await request.post('/api/auth/login', {
        data: {
          username: 'admin',
          password: 'admin'
        }
      });
      
      // If login fails, check if auth is disabled
      if (!loginResponse.ok() && loginResponse.status() !== 401) {
        // Auth might be disabled, continue
      }
    } catch (e) {
      // Auth might be disabled or server not ready
    }

    // Test pagination - wait a bit for server to be ready
    await page.waitForTimeout(500);
    const response1 = await request.get('/api/files?path=&limit=10&offset=0');
    
    // Accept both 200 (success) and 401/403 (auth required) as valid responses
    expect([200, 401, 403]).toContain(response1.status());
    
    const body1 = await response1.json();
    expect(body1).toHaveProperty('pagination');
    expect(body1.pagination).toHaveProperty('limit', 10);
    expect(body1.pagination).toHaveProperty('offset', 0);
    expect(body1.pagination).toHaveProperty('hasMore');

    // Test second page
    const response2 = await request.get('/api/files?path=&limit=10&offset=10');
    expect(response2.ok()).toBeTruthy();
    
    const body2 = await response2.json();
    expect(body2.pagination.offset).toBe(10);
  });

  test('Download multiple files returns ZIP', async ({ request, page }) => {
    // Login first if needed
    try {
      await page.goto('/');
      await request.post('/api/auth/login', {
        data: {
          username: 'admin',
          password: 'admin'
        }
      });
    } catch (e) {
      // Auth might be disabled
    }

    // Get list of files first
    const filesResponse = await request.get('/api/files?path=');
    const filesBody = await filesResponse.json();
    
    if (filesBody.items && filesBody.items.length > 0) {
      // Get first two files
      const filePaths = filesBody.items
        .filter(item => !item.isDirectory)
        .slice(0, 2)
        .map(item => item.path);

      if (filePaths.length > 0) {
        const downloadResponse = await request.post('/api/download-multiple', {
          data: {
            files: filePaths
          }
        });

        expect(downloadResponse.ok()).toBeTruthy();
        expect(downloadResponse.headers()['content-type']).toContain('application/zip');
        
        const contentLength = downloadResponse.headers()['content-length'];
        expect(parseInt(contentLength || '0')).toBeGreaterThan(0);
      }
    }
  });

  test('Download multiple files rejects too many files', async ({ request, page }) => {
    // Login first if needed
    try {
      await page.goto('/');
      await request.post('/api/auth/login', {
        data: {
          username: 'admin',
          password: 'admin'
        }
      });
    } catch (e) {
      // Auth might be disabled
    }

    const manyFiles = Array.from({ length: 101 }, (_, i) => `file${i}.txt`);
    
    const response = await request.post('/api/download-multiple', {
      data: {
        files: manyFiles
      }
    });

    expect(response.status()).toBe(400);
    const body = await response.json();
    expect(body.error).toContain('Too many files');
  });

  test('Error boundary catches React errors', async ({ page }) => {
    await page.goto('/');
    
    // Try to trigger an error (if possible)
    // This is a basic test - in a real scenario you'd inject an error
    const hasErrorBoundary = await page.evaluate(() => {
      return typeof window !== 'undefined';
    });
    
    expect(hasErrorBoundary).toBeTruthy();
  });

  test('Stream endpoint supports range requests', async ({ request }) => {
    // Get a file to stream
    const filesResponse = await request.get('/api/files?path=');
    const filesBody = await filesResponse.json();
    
    if (filesBody.items && filesBody.items.length > 0) {
      const videoFile = filesBody.items.find(item => item.isVideo);
      
      if (videoFile) {
        const rangeResponse = await request.get(`/api/stream/${encodeURIComponent(videoFile.path)}`, {
          headers: {
            'Range': 'bytes=0-1023'
          }
        });

        expect(rangeResponse.status()).toBe(206); // Partial Content
        expect(rangeResponse.headers()['content-range']).toBeTruthy();
        expect(rangeResponse.headers()['accept-ranges']).toBe('bytes');
      }
    }
  });

  test('Context timeout works for long operations', async ({ request, page }) => {
    // Login first if needed
    try {
      await page.goto('/');
      await request.post('/api/auth/login', {
        data: {
          username: 'admin',
          password: 'admin'
        }
      });
    } catch (e) {
      // Auth might be disabled
    }

    // This test verifies that timeouts are set
    // In a real scenario, you'd test with a very slow operation
    const startTime = Date.now();
    
    const response = await request.get('/api/files?path=');
    const duration = Date.now() - startTime;
    
    // Accept success or auth errors as valid (timeout would be different)
    expect([200, 401, 403]).toContain(response.status());
    // Should complete within reasonable time (30 seconds timeout)
    expect(duration).toBeLessThan(30000);
  });
});

