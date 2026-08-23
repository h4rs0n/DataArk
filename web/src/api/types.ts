// 后端 JSON 信封。多数接口把业务数据放在 Data；搜索把命中列表放在 Result 字符串里。

export interface ApiEnvelope<T = unknown> {
  Status: string
  Message: string
  Error?: string
  Data?: T
  Result?: string
  TotalHits?: number | string
  TotalPages?: number | string
}
