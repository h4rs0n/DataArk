// 推荐中心共用的日期、列表与标签格式化。

export function formatLocalDate(date: Date): string {
  const year = date.getFullYear()
  const month = `${date.getMonth() + 1}`.padStart(2, '0')
  const day = `${date.getDate()}`.padStart(2, '0')
  return `${year}-${month}-${day}`
}

// addCalendarDays 用 UTC 日历加减，避免本地夏令时把日期跳成两天或零天。
export function addCalendarDays(date: string, delta: number): string {
  const [year, month, day] = date.split('-').map((part) => Number(part))
  const utc = new Date(Date.UTC(year, month - 1, day + delta))
  const nextYear = utc.getUTCFullYear()
  const nextMonth = `${utc.getUTCMonth() + 1}`.padStart(2, '0')
  const nextDay = `${utc.getUTCDate()}`.padStart(2, '0')
  return `${nextYear}-${nextMonth}-${nextDay}`
}

export function formatDateTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

export function parseList(raw?: string): string[] {
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed.filter((item) => typeof item === 'string' && item.trim()).map((item) => item.trim()) : []
  } catch {
    return []
  }
}

export function splitPreference(raw: string): string[] {
  return [...new Set(raw.split(/[,，\n]/).map((value) => value.trim()).filter(Boolean))]
}

export function sourceHost(rawUrl?: string): string {
  if (!rawUrl) return ''
  try {
    return new URL(rawUrl).hostname
  } catch {
    return rawUrl
  }
}

export function blockRuleTypeLabel(type: string): string {
  if (type === 'topic') return '主题'
  if (type === 'source') return '来源'
  if (type === 'style') return '类型'
  return type
}
