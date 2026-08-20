// Jest setup file
process.env.NODE_ENV = 'test';
process.env.PORT = '8081';
process.env.MEDIA_PATH = './test-media';

// Mock console methods to reduce noise in tests
global.console = {
  ...console,
  log: jest.fn(),
  debug: jest.fn(),
  info: jest.fn(),
  warn: jest.fn(),
  error: jest.fn(),
};
