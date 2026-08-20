module.exports = {
  env: {
    browser: true,
    es2021: true,
    node: true,
    jest: true
  },
  extends: [
    'eslint:recommended'
  ],
  parserOptions: {
    ecmaVersion: 'latest',
    sourceType: 'module'
  },
  rules: {
    'no-console': 'off', // Allow console.log in scripts
    'no-unused-vars': 'warn',
    'prefer-const': 'error',
    'no-var': 'error'
  },
  overrides: [
    {
      files: ['scripts/**/*.js'],
      rules: {
        'no-console': 'off' // Explicitly allow console in scripts
      }
    },
    {
      files: ['server/**/*.js'],
      rules: {
        'no-console': 'off' // Allow console in server code
      }
    }
  ]
};