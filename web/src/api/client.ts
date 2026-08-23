import type { ApiEnvelope } from '@/api/types'
import { AUTH_TOKEN_KEY, useAuthStore } from '@/stores/auth'

export class ApiResponseError extends Error {
  constructor(
    message: string,
    readonly statusCode: number,
  ) {
    super(message)
    this.name = 'ApiResponseError'
  }
}

export function isUnauthorizedError(error: unknown): boolean {
  return error instanceof ApiResponseError && error.statusCode === 401
}

// Pinia 尚未挂上时（例如 fetch 包装初始化）回退读存储。
function currentToken(): string {
  try {
    return useAuthStore().token
  } catch {
    return localStorage.getItem(AUTH_TOKEN_KEY) || sessionStorage.getItem(AUTH_TOKEN_KEY) || ''
  }
}

export function authHeaders(json = false): Record<string, string> {
  const token = currentToken()
  return {
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...(json ? { 'Content-Type': 'application/json' } : {}),
  }
}

type RequestOptions = RequestInit & { auth?: boolean }

function headerRecord(headers?: HeadersInit): Record<string, string> {
  if (!headers) {
    return {}
  }
  if (headers instanceof Headers) {
    return Object.fromEntries(headers.entries())
  }
  if (Array.isArray(headers)) {
    return Object.fromEntries(headers)
  }
  return { ...headers }
}

function hasHeader(headers: Record<string, string>, name: string): boolean {
  const needle = name.toLowerCase()
  return Object.keys(headers).some((key) => key.toLowerCase() === needle)
}

function buildHeaders(options: RequestOptions): Record<string, string> {
  const headers = headerRecord(options.headers)
  if (options.auth !== false) {
    const token = currentToken()
    if (token && !hasHeader(headers, 'Authorization')) {
      headers.Authorization = `Bearer ${token}`
    }
  }
  const body = options.body
  const isFormData = typeof FormData !== 'undefined' && body instanceof FormData
  if (isFormData) {
    // FormData 必须由浏览器自己带 boundary，不能预设 Content-Type。
    for (const key of Object.keys(headers)) {
      if (key.toLowerCase() === 'content-type') {
        delete headers[key]
      }
    }
    return headers
  }
  if (typeof body === 'string' && !hasHeader(headers, 'Content-Type')) {
    headers['Content-Type'] = 'application/json'
  }
  return headers
}

async function send(url: string, options: RequestOptions = {}): Promise<Response> {
  const { auth: _auth, ...init } = options
  return fetch(url, {
    ...init,
    headers: buildHeaders(options),
  })
}

async function readEnvelope(response: Response): Promise<ApiEnvelope> {
  try {
    return (await response.json()) as ApiEnvelope
  } catch {
    return {
      Status: response.ok ? '1' : '0',
      Message: response.ok ? '' : `HTTP ${response.status}`,
    }
  }
}

function assertEnvelopeOk(response: Response, payload: ApiEnvelope): void {
  if (!response.ok || payload.Status === '0') {
    throw new ApiResponseError(
      payload.Error || payload.Message || `HTTP ${response.status}`,
      response.status,
    )
  }
}

// 解析 Status/Message/Data 信封，返回 Data。
export async function requestJSON<T>(url: string, options: RequestOptions = {}): Promise<T> {
  const payload = await requestEnvelope<T>(url, options)
  return payload.Data as T
}

// 返回完整信封，给搜索这种非 Data 形状的接口用。
export async function requestEnvelope<T = unknown>(url: string, options: RequestOptions = {}): Promise<ApiEnvelope<T>> {
  const response = await send(url, options)
  const payload = await readEnvelope(response)
  assertEnvelopeOk(response, payload)
  return payload as ApiEnvelope<T>
}

// 下载二进制；若服务端用 JSON 信封报错则抛出。
export async function requestBlob(url: string, options: RequestOptions = {}): Promise<{ blob: Blob; fileName: string | null }> {
  const response = await send(url, options)
  const contentType = response.headers.get('Content-Type') || ''
  if (!response.ok || contentType.includes('application/json')) {
    const payload = await readEnvelope(response)
    throw new ApiResponseError(
      payload.Error || payload.Message || `HTTP ${response.status}`,
      response.status,
    )
  }
  return {
    blob: await response.blob(),
    fileName: parseContentDispositionFileName(response.headers.get('Content-Disposition')),
  }
}

// 拉取归档 HTML 正文（非 JSON 信封）。
export async function requestText(url: string, options: RequestOptions = {}): Promise<string> {
  const response = await send(url, options)
  if (!response.ok) {
    const payload = contentTypeLooksJSON(response)
      ? await readEnvelope(response)
      : { Status: '0', Message: `HTTP ${response.status}: ${response.statusText}` }
    throw new ApiResponseError(payload.Error || payload.Message || `HTTP ${response.status}`, response.status)
  }
  return response.text()
}

function contentTypeLooksJSON(response: Response): boolean {
  return (response.headers.get('Content-Type') || '').includes('application/json')
}

export function parseContentDispositionFileName(disposition: string | null): string | null {
  if (!disposition) {
    return null
  }
  const match = disposition.match(/filename="?([^"]+)"?/i)
  return match?.[1] || null
}
