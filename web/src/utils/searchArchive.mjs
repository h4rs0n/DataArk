// Keep submission state independent from the eventual presence of a local file.
export async function submitSearchArchive(actions, item, submit) {
  if (['submitting', 'queued', 'success'].includes(actions[item.id]?.status)) return null
  actions[item.id] = { status: 'submitting' }
  try {
    const response = await submit(item.link)
    if (!response.Data) throw new Error('归档响应缺少任务信息，请重试')
    if (response.Data.status === 'failed') throw new Error(response.Data.error || '归档任务失败，请重试')
    const completed = response.Data.status === 'success'
    actions[item.id] = { status: completed ? 'success' : 'queued' }
    return completed ? '已归档，请重新搜索查看本地入口' : '已加入归档队列'
  } catch (error) {
    actions[item.id] = { status: 'failed', error: error instanceof Error ? error.message : '提交归档失败，请重试' }
    return null
  }
}
