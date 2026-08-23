import { defineStore } from 'pinia'

// localStorage / sessionStorage 共用的 JWT 键名。
export const AUTH_TOKEN_KEY = 'token'

export type AuthPersist = 'local' | 'session'

function storageFor(persist: AuthPersist): Storage {
  return persist === 'local' ? localStorage : sessionStorage
}

// 启动时先读长期 token，再读会话 token。
function readStoredToken(): string {
  return localStorage.getItem(AUTH_TOKEN_KEY) || sessionStorage.getItem(AUTH_TOKEN_KEY) || ''
}

function writeStoredToken(token: string, persist: AuthPersist) {
  localStorage.removeItem(AUTH_TOKEN_KEY)
  sessionStorage.removeItem(AUTH_TOKEN_KEY)
  storageFor(persist).setItem(AUTH_TOKEN_KEY, token)
}

function clearStoredToken() {
  localStorage.removeItem(AUTH_TOKEN_KEY)
  sessionStorage.removeItem(AUTH_TOKEN_KEY)
}

// 登录态是前端唯一的全局状态；页面数据不进 Pinia。
export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: readStoredToken(),
  }),
  actions: {
    // persist=local 对应「记住我」；session 关闭标签后失效。
    setToken(token: string, persist: AuthPersist = 'local') {
      this.token = token
      writeStoredToken(token, persist)
    },
    clearAuth() {
      this.token = ''
      clearStoredToken()
    },
  },
})
