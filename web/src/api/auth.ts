import { requestJSON } from '@/api/client'

export interface AuthUser {
  id: number
  username: string
  role?: string
}

export interface LoginResult {
  token: string
  expires_at?: string
  user?: AuthUser
}

// 登录不带旧 token，避免过期 Authorization 干扰。
export function login(username: string, password: string): Promise<LoginResult> {
  return requestJSON<LoginResult>('/api/login', {
    method: 'POST',
    auth: false,
    body: JSON.stringify({ username, password }),
  })
}

export function checkAuth(): Promise<AuthUser> {
  return requestJSON<AuthUser>('/api/authChecker')
}
