import js from '@eslint/js';
import { defineConfig, globalIgnores } from 'eslint/config';
import tseslint from 'typescript-eslint';

// APP-22: the player is framework-agnostic and must stay portable between the
// Capacitor and Tauri shells, so it may not depend on React or on either shell.
const PLAYER_IMPORT_MESSAGE =
  '@ipcam/player must not import React, Capacitor or Tauri (APP-22). Inject platform behaviour through its options instead.';

export default defineConfig(
  globalIgnores(['**/dist/**', '**/coverage/**']),
  js.configs.recommended,
  tseslint.configs.recommended,
  {
    rules: {
      '@typescript-eslint/no-explicit-any': 'error',
    },
  },
  {
    files: ['apps/packages/player/**/*.{ts,tsx,js,jsx}'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          paths: [
            { name: 'react', message: PLAYER_IMPORT_MESSAGE },
            { name: 'react-dom', message: PLAYER_IMPORT_MESSAGE },
          ],
          patterns: [
            {
              group: ['react/*', 'react-dom/*', '@capacitor/*', '@tauri-apps/*'],
              message: PLAYER_IMPORT_MESSAGE,
            },
          ],
        },
      ],
      // no-restricted-imports does not see dynamic import(), so cover it here.
      'no-restricted-syntax': [
        'error',
        {
          selector:
            'ImportExpression[source.value=/^(react|react-dom)(\\/|$)|^(@capacitor|@tauri-apps)\\//]',
          message: PLAYER_IMPORT_MESSAGE,
        },
      ],
    },
  },
);
