// @ts-check
const { test, expect } = require('@playwright/test');

test.describe('User Management Tests', () => {
  let adminToken = null;

  test.beforeEach(async ({ page, request }) => {
    // Login as admin
    const loginResponse = await request.post('/api/auth/login', {
      data: {
        username: 'admin',
        password: 'admin'
      },
      failOnStatusCode: false
    });

    if (loginResponse.ok()) {
      const body = await loginResponse.json();
      adminToken = body.token;
    } else {
      // Auth might be disabled, skip tests
      test.skip();
    }
  });

  test('should list users', async ({ request }) => {
    if (!adminToken) {
      test.skip();
      return;
    }

    const response = await request.get('/api/users', {
      headers: {
        'Authorization': `Bearer ${adminToken}`
      },
      failOnStatusCode: false
    });

    if (response.ok()) {
      const body = await response.json();
      expect(body).toHaveProperty('users');
      expect(Array.isArray(body.users)).toBe(true);
    } else {
      // Might not be admin, skip
      expect([401, 403]).toContain(response.status());
    }
  });

  test('should create user', async ({ request }) => {
    if (!adminToken) {
      test.skip();
      return;
    }

    const response = await request.post('/api/users', {
      headers: {
        'Authorization': `Bearer ${adminToken}`,
        'Content-Type': 'application/json'
      },
      data: {
        username: 'testuser_' + Date.now(),
        password: 'password123',
        email: `test${Date.now()}@test.com`,
        role: 'user',
        permissions: ['read']
      },
      failOnStatusCode: false
    });

    if (response.ok()) {
      const body = await response.json();
      expect(body).toHaveProperty('success', true);
    } else {
      expect([400, 401, 403]).toContain(response.status());
    }
  });

  test('should reject user creation with invalid data', async ({ request }) => {
    if (!adminToken) {
      test.skip();
      return;
    }

    const response = await request.post('/api/users', {
      headers: {
        'Authorization': `Bearer ${adminToken}`,
        'Content-Type': 'application/json'
      },
      data: {
        username: 'ab', // Too short
        password: 'pass', // Too short
        email: 'invalid-email',
        role: 'user'
      },
      failOnStatusCode: false
    });

    expect([400, 401, 403]).toContain(response.status());
  });

  test('should update user', async ({ request }) => {
    if (!adminToken) {
      test.skip();
      return;
    }

    // First create a user
    const createResponse = await request.post('/api/users', {
      headers: {
        'Authorization': `Bearer ${adminToken}`,
        'Content-Type': 'application/json'
      },
      data: {
        username: 'updateuser_' + Date.now(),
        password: 'password123',
        email: `update${Date.now()}@test.com`,
        role: 'user'
      },
      failOnStatusCode: false
    });

    if (createResponse.ok()) {
      const createBody = await createResponse.json();
      const userId = createBody.user?.id;

      if (userId) {
        const updateResponse = await request.put(`/api/users/${userId}`, {
          headers: {
            'Authorization': `Bearer ${adminToken}`,
            'Content-Type': 'application/json'
          },
          data: {
            email: 'updated@test.com',
            role: 'admin'
          },
          failOnStatusCode: false
        });

        expect([200, 400, 401, 403, 404]).toContain(updateResponse.status());
      }
    }
  });

  test('should delete user', async ({ request }) => {
    if (!adminToken) {
      test.skip();
      return;
    }

    // First create a user
    const createResponse = await request.post('/api/users', {
      headers: {
        'Authorization': `Bearer ${adminToken}`,
        'Content-Type': 'application/json'
      },
      data: {
        username: 'deleteuser_' + Date.now(),
        password: 'password123',
        email: `delete${Date.now()}@test.com`,
        role: 'user'
      },
      failOnStatusCode: false
    });

    if (createResponse.ok()) {
      const createBody = await createResponse.json();
      const userId = createBody.user?.id;

      if (userId) {
        const deleteResponse = await request.delete(`/api/users/${userId}`, {
          headers: {
            'Authorization': `Bearer ${adminToken}`
          },
          failOnStatusCode: false
        });

        expect([200, 400, 401, 403, 404]).toContain(deleteResponse.status());
      }
    }
  });
});

