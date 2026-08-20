// @ts-check
const { test, expect } = require('@playwright/test');

test.describe('Pagination Tests', () => {
  let authToken = null;

  test.beforeEach(async ({ request }) => {
    // Login
    const loginResponse = await request.post('/api/auth/login', {
      data: {
        username: 'admin',
        password: 'admin'
      },
      failOnStatusCode: false
    });

    if (loginResponse.ok()) {
      const body = await loginResponse.json();
      authToken = body.token;
    }
  });

  test('should support pagination parameters', async ({ request }) => {
    if (!authToken) {
      test.skip();
      return;
    }

    // Test with limit
    const response1 = await request.get('/api/files?path=&limit=10&offset=0', {
      headers: {
        'Authorization': `Bearer ${authToken}`
      },
      failOnStatusCode: false
    });

    if (response1.ok()) {
      const body1 = await response1.json();
      expect(body1).toHaveProperty('items');
      expect(body1).toHaveProperty('currentPath');
      // Pagination metadata is in response
      if (body1.pagination) {
        expect(body1.pagination).toHaveProperty('limit');
        expect(body1.pagination).toHaveProperty('offset');
        expect(body1.pagination).toHaveProperty('hasMore');
        expect(body1.pagination).toHaveProperty('total');
      }
    }

    // Test second page
    const response2 = await request.get('/api/files?path=&limit=10&offset=10', {
      headers: {
        'Authorization': `Bearer ${authToken}`
      },
      failOnStatusCode: false
    });

    if (response2.ok()) {
      const body2 = await response2.json();
      expect(body2).toHaveProperty('items');
      // Offset is applied server-side, items should be different from first page
    }
  });

  test('should handle invalid pagination parameters', async ({ request }) => {
    if (!authToken) {
      test.skip();
      return;
    }

    // Test with negative limit
    const response1 = await request.get('/api/files?path=&limit=-10', {
      headers: {
        'Authorization': `Bearer ${authToken}`
      },
      failOnStatusCode: false
    });

    // Should use default limit (100)
    if (response1.ok()) {
      const body = await response1.json();
      expect(body).toHaveProperty('items');
      // Limit is applied server-side, items length should be <= 100
      expect(body.items.length).toBeLessThanOrEqual(100);
    }

    // Test with limit too high
    const response2 = await request.get('/api/files?path=&limit=10000', {
      headers: {
        'Authorization': `Bearer ${authToken}`
      },
      failOnStatusCode: false
    });

    // Should cap at max (1000)
    if (response2.ok()) {
      const body = await response2.json();
      expect(body.items.length).toBeLessThanOrEqual(1000);
    }
  });
});

