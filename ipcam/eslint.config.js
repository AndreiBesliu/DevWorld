import js from '@eslint/js';
import { defineConfig, globalIgnores } from 'eslint/config';
import tseslint from 'typescript-eslint';

// typescript is held at 6.0.x in package.json: typescript-eslint (8.71.2 and
// its canary alike) declares `typescript <6.1.0` and refuses to load under
// TypeScript 7 (tracking issue typescript-eslint/typescript-eslint#10940).
// Revisit when a typescript-eslint release at least 14 days old supports 7.x.

// APP-22: the player is framework-agnostic and must stay portable between the
// Capacitor and Tauri shells, so it may not depend on React or on either shell.
const PLAYER_IMPORT_MESSAGE =
  '@ipcam/player must not import React, Capacitor or Tauri (APP-22). Inject platform behaviour through its options instead.';

// The module specifiers that no-restricted-imports bans for the player, as one
// regular expression for the selectors below. no-restricted-imports only sees
// import/export declarations, so dynamic import('react') and the type-position
// import('react').FC (a TSImportType) need selectors. Both ImportExpression and
// TSImportType (typescript-eslint 8.71) keep the specifier in `source`.
const PLAYER_FORBIDDEN_SPECIFIER = String.raw`^(react|react-dom)(\/|$)|^(@capacitor|@tauri-apps)\/`;

export default defineConfig(
  globalIgnores([
    '**/dist/**',
    '**/coverage/**',
    // `cap sync` copies the spike app's dist/ into the Android project's assets.
    'apps/mobile/android/app/src/main/assets/**',
    // Gradle's output: AGP merges those assets into build/intermediates/assets,
    // so a local build leaves the minified bundle there.
    'apps/mobile/android/**/build/**',
    // Cargo's output: tauri-codegen leaves the embedded app and Tauri's own scripts there.
    'apps/desktop/src-tauri/target/**',
  ]),
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
      // A later config entry for these files that sets no-restricted-syntax
      // replaces this one, so add any further selectors here.
      'no-restricted-syntax': [
        'error',
        ...['ImportExpression', 'TSImportType'].map((node) => ({
          selector: `${node}[source.value=/${PLAYER_FORBIDDEN_SPECIFIER}/]`,
          message: PLAYER_IMPORT_MESSAGE,
        })),
      ],
    },
  },
);
