import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import {
  bootstrapReaderTheme,
  clearReaderTheme,
  isReaderRoute,
} from './lib/reader-theme-bootstrap'
import './style.css'

bootstrapReaderTheme()
// Reader owns live theme changes. The router is the sole teardown owner so
// root variables also cover teleported dialogs until navigation succeeds.
router.afterEach((to, _from, failure) => {
  if (!failure && !isReaderRoute(to.path)) clearReaderTheme()
})

createApp(App).use(router).mount('#app')
