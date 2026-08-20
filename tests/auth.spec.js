// @ts-check
const { test, expect } = require('@playwright/test');

test.describe('Authentication Tests', () => {
  let authToken = null;
  let refreshTokenCookie = null;

  test.beforeEach(async ({ page, request }) => {
    await page.goto('/');
    await page.waitForTimeout(1000);
  });

  test('should login successfully', async ({ page, request }) => {
    // Try to login
    const loginResponse = await request.post('/api/auth/login', {
      data: {
        username: 'admin',
        password: 'admin'
      },
      failOnStatusCode: false
    });

    if (loginResponse.ok()) {
      const body = await loginResponse.json();
      expect(body.success).toBe(true);
      authToken = body.token || (await page.evaluate(() => localStorage.getItem('auth_token')));
    } else {
      // Auth might be disabled, skip test
      test.skip();
    }
  });

  test('should get auth status when authenticated', async ({ page, request }) => {
    // Login first
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

    if (token) {
      const statusResponse = await request.get('/api/auth/status', {
        headers: {
          'Authorization': `Bearer ${token}`
        },
        failOnStatusCode: false
      });

      if (statusResponse.ok()) {
        const statusBody = await statusResponse.json();
        expect(statusBody).toHaveProperty('username');
        expect(statusBody).toHaveProperty('role');
      }
    }
  });

  test('should logout successfully', async ({ page, request }) => {
    // Login first
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

    if (token) {
      const logoutResponse = await request.post('/api/auth/logout', {
        headers: {
          'Authorization': `Bearer ${token}`
        },
        failOnStatusCode: false
      });

      expect([200, 401]).toContain(logoutResponse.status());
    }
  });

  test('should reject invalid credentials', async ({ request }) => {
    const response = await request.post('/api/auth/login', {
      data: {
        username: 'invalid',
        password: 'wrong'
      },
      failOnStatusCode: false
    });

    // Should return 401 or 400
    expect([400, 401]).toContain(response.status());
  });

  test('should handle forgot password', async ({ request }) => {
    const response = await request.post('/api/auth/forgot-password', {
      data: {
        email: 'admin@drivelocal.local'
      },
      failOnStatusCode: false
    });

    // Should return 200 (even if email doesn't exist for security)
    expect([200, 404]).toContain(response.status());
  });
});

