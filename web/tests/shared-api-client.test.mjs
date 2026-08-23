import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import path from 'node:path'
import test from 'node:test'

const srcRoot = new URL('../src/', import.meta.url)

async function collectSourceFiles(relativeDir, extensions) {
  const dir = new URL(relativeDir, srcRoot)
  const entries = await readdir(dir, { withFileTypes: true })
  const files = []
  for (const entry of entries) {
    if (entry.isDirectory()) {
      files.push(...await collectSourceFiles(`${relativeDir}${entry.name}/`, extensions))
      continue
    }
    if (extensions.some((ext) => entry.name.endsWith(ext))) {
      files.push(new URL(entry.name, dir))
    }
  }
  return files
}

async function readAll(urls) {
  const chunks = []
  for (const url of urls) {
    chunks.push(await readFile(url, 'utf8'))
  }
  return chunks.join('\n')
}

test('views and recommendation panels do not reimplement auth fetch helpers', async () => {
  const views = await collectSourceFiles('views/', ['.vue'])
  const panels = await collectSourceFiles('components/recommendations/', ['.vue'])
  const searchInput = [new URL('components/SearchInput/index.vue', srcRoot)]
  const files = [...views, ...panels, ...searchInput]
  const names = files.map((file) => path.basename(file.pathname))

  for (const file of files) {
    const source = await readFile(file, 'utf8')
    const name = path.basename(file.pathname)
    assert.doesNotMatch(source, /\bfunction getAuthToken\b|\bconst getAuthToken\b/, `${name} still defines getAuthToken`)
    assert.doesNotMatch(source, /class ApiResponseError/, `${name} still defines ApiResponseError`)
    assert.doesNotMatch(source, /\b(?:async )?function requestJSON\b|\bconst requestJSON =/, `${name} still defines requestJSON`)
    assert.doesNotMatch(source, /from ['"]axios['"]/, `${name} still imports axios`)
    assert.doesNotMatch(source, /components\/recommendations\/http/, `${name} still imports recommendations/http`)
  }

  assert.ok(names.includes('LoginView.vue'))
  const login = await readFile(new URL('views/LoginView.vue', srcRoot), 'utf8')
  assert.match(login, /useAuthStore/)
  assert.match(login, /setToken/)
  assert.doesNotMatch(login, /axios/)
})

test('shared API client and auth store are the token source of truth', async () => {
  const client = await readFile(new URL('api/client.ts', srcRoot), 'utf8')
  const authStore = await readFile(new URL('stores/auth.ts', srcRoot), 'utf8')
  const main = await readFile(new URL('main.ts', srcRoot), 'utf8')
  const router = await readFile(new URL('router/index.ts', srcRoot), 'utf8')

  assert.match(client, /export async function requestJSON/)
  assert.match(client, /export async function requestBlob/)
  assert.match(client, /export async function requestEnvelope/)
  assert.match(authStore, /defineStore\('auth'/)
  assert.match(authStore, /setToken/)
  assert.match(authStore, /clearAuth/)
  assert.match(main, /createPinia\(\)/)
  assert.match(main, /useAuthStore\(\)\.clearAuth/)
  assert.match(router, /useAuthStore/)
  assert.match(router, /checkAuth/)
})
