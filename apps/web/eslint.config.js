import js from '@eslint/js'
import pluginVue from 'eslint-plugin-vue'
import globals from 'globals'
import {
  withVueTs,
  vueTsConfigs,
} from '@vue/eslint-config-typescript'

export default withVueTs(
  { rootDir: import.meta.dirname },
  { ignores: ['dist/**', 'node_modules/**'] },
  js.configs.recommended,
  pluginVue.configs['flat/essential'],
  vueTsConfigs.recommendedTypeChecked,
  {
    name: 'panda-pages/linter-options',
    linterOptions: {
      reportUnusedDisableDirectives: 'error',
    },
  },
  {
    name: 'panda-pages/browser',
    files: ['src/**/*.{ts,vue}'],
    languageOptions: {
      globals: globals.browser,
    },
    rules: {
      'no-eval': 'error',
      'no-implied-eval': 'error',
      'vue/no-v-html': 'error',
      'no-restricted-syntax': [
        'error',
        {
          selector:
            "AssignmentExpression[left.type='MemberExpression'][left.property.name=/^(innerHTML|outerHTML)$/]",
          message: 'Use textContent, DOMParser, or a reviewed trusted-renderer boundary.',
        },
        {
          selector: "CallExpression[callee.type='MemberExpression'][callee.property.name='insertAdjacentHTML']",
          message: 'Use a reviewed trusted-renderer boundary.',
        },
      ],
    },
  },
  {
    files: ['vite.config.ts'],
    languageOptions: {
      globals: globals.nodeBuiltin,
    },
  },
  {
    files: ['tests/**/*.{mjs,ts}', 'playwright.config.ts', 'eslint.config.js'],
    languageOptions: {
      globals: globals.nodeBuiltin,
    },
  },
  {
    name: 'panda-pages/library-type-safety',
    files: ['src/lib/**/*.ts'],
    rules: {
      '@typescript-eslint/no-unsafe-argument': 'error',
      '@typescript-eslint/no-unsafe-assignment': 'error',
      '@typescript-eslint/no-unsafe-call': 'error',
      '@typescript-eslint/no-unsafe-member-access': 'error',
      '@typescript-eslint/no-unsafe-return': 'error',
    },
  },
  {
    files: [
      'src/components/reader/ReaderScrollView.vue',
      'src/components/reader/ReaderPagedView.vue',
      'src/components/admin/story-studio/StoryPreviewPane.vue',
    ],
    rules: {
      // Story HTML is rendered by Goldmark's safe mode in the API. Regression
      // tests reject raw HTML and dangerous URL protocols before persistence.
      'vue/no-v-html': 'off',
    },
  },
  {
    rules: {
      'vue/multi-word-component-names': 'off',
    },
  },
)
