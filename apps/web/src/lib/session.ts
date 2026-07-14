import { authStatus } from './api'
import { createAuthStateCache } from './auth-state'

export const authState = createAuthStateCache(async () => {
  const status = await authStatus()
  return status.unlocked
})
