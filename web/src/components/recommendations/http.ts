// 推荐中心共用的鉴权头与 JSON API 请求。

export function authHeaders(json = false): Record<string, string> {
  const token = localStorage.getItem('token') || sessionStorage.getItem('token')
  return {
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...(json ? { 'Content-Type': 'application/json' } : {}),
  }
}

export async function requestJSON<T>(url: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(url, options)
  const payload = await response.json().catch(() => ({}))
  if (!response.ok || payload.Status === '0') {
    throw new Error(payload.Message || `HTTP ${response.status}`)
  }
  return payload.Data as T
}
