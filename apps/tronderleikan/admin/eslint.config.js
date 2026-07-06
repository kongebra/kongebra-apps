import js from '@eslint/js'
import tseslint from 'typescript-eslint'
import reactHooks from 'eslint-plugin-react-hooks'
import globals from 'globals'

// Flat config. Genererte filer + build-output holdes utenfor.
export default tseslint.config(
  {
    ignores: ['.output', '.nitro', '.tanstack', 'dist', 'src/routeTree.gen.ts'],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['**/*.{ts,tsx,js,mjs,cjs}'],
    languageOptions: {
      globals: { ...globals.browser, ...globals.node },
    },
    rules: {
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
    },
  },
  {
    // react-hooks-regler kun på React-komponenter (.tsx). Server-utils som
    // h3 useSession (@tanstack/react-start/server) er ikke React-hooks.
    files: ['**/*.tsx'],
    plugins: {
      'react-hooks': reactHooks,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
    },
  },
)
