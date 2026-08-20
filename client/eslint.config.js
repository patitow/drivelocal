import js from '@eslint/js';
import globals from 'globals';
import tseslint from '@typescript-eslint/eslint-plugin';
import tsParser from '@typescript-eslint/parser';
import react from 'eslint-plugin-react';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import jsxA11y from 'eslint-plugin-jsx-a11y';
import importPlugin from 'eslint-plugin-import';
import tailwindcss from 'eslint-plugin-tailwindcss';

export default [
  { ignores: ['dist/**', 'node_modules/**', 'storybook-static/**', '.storybook/**'] },
  js.configs.recommended,
  ...tseslint.configs['flat/recommended'],
  react.configs.flat.recommended,
  react.configs.flat['jsx-runtime'],
  jsxA11y.flatConfigs.recommended,
  importPlugin.flatConfigs.recommended,
  ...tailwindcss.configs['flat/recommended'],
  {
    settings: {
      react: { version: 'detect' },
    },
    rules: {
      // No eslint-import-resolver-typescript installed, so the `@/*` path
      // alias from tsconfig.json can't be resolved by eslint-plugin-import.
      'import/no-unresolved': 'off',
      'import/namespace': 'off',
      'import/named': 'off',
    },
  },
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      parser: tsParser,
      ecmaVersion: 2020,
      sourceType: 'module',
      globals: { ...globals.browser, ...globals.node },
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
      'react/prop-types': 'off',
    },
  },
  {
    // Root-level tooling config files run under Node, not the browser, and
    // Tailwind/PostCSS load theirs through a loader that tolerates require()
    // inside an ESM file.
    files: ['*.config.{js,ts}'],
    languageOptions: {
      globals: globals.node,
    },
    rules: {
      '@typescript-eslint/no-require-imports': 'off',
      '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_' }],
    },
  },
];
