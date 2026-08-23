import { requestEnvelope, requestJSON } from '@/api/client'

export interface SearchHit {
  title: string
  filename: string
  link: string
  content: string
  domain: string
}

export interface SearchPage {
  result: SearchHit[]
  totalHits: number
}

export interface KeywordSuggestion {
  keyword: string
  count: number
}

// 搜索接口把命中列表放在 Result 字符串里，不是 Data。
export async function searchByKeyword(keyword: string, page = '1'): Promise<SearchPage> {
  const envelope = await requestEnvelope(
    `/api/search?q=${encodeURIComponent(keyword)}&p=${encodeURIComponent(page || '1')}`,
  )
  let result: SearchHit[] = []
  try {
    result = JSON.parse(envelope.Result || '[]') as SearchHit[]
  } catch {
    result = []
  }
  return {
    result,
    totalHits: Number(envelope.TotalHits ?? 0),
  }
}

export async function suggestKeywords(prefix: string, limit = 6): Promise<KeywordSuggestion[]> {
  return (await requestJSON<KeywordSuggestion[]>(
    `/api/search/keywords?prefix=${encodeURIComponent(prefix)}&limit=${limit}`,
  )) ?? []
}
