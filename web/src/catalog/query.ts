import type { LocationQuery, LocationQueryRaw } from 'vue-router'

export interface CatalogFilters {
  search: string
  author: string
  tag: string
  tag1: string
  tag2: string
  minScore: string
  maxScore: string
  minWords: string
  maxWords: string
  onlyContent: string
  sortBy: string
}
export interface CatalogScope { kind: 'library' | 'category' | 'author'; name: string }
export const defaults: CatalogFilters = {
  search: '', author: '', tag: '', tag1: '', tag2: '', minScore: '7.5', maxScore: '10',
  minWords: '0', maxWords: '9007199254740991', onlyContent: '1', sortBy: 'score',
}
export const filterKeys = Object.keys(defaults) as (keyof CatalogFilters)[]
export const sorts = [
  { value: 'score', label: '评分最高' }, { value: 'xiancao', label: '仙草票最多' },
  { value: 'popularity', label: '评价最多' }, { value: 'latest', label: '最近入库' },
  { value: 'words', label: '字数最多' }, { value: 'title', label: '书名顺序' },
]

export function readFilters(query: LocationQuery): CatalogFilters {
  const filters = { ...defaults }
  for (const key of filterKeys) {
    const value = query[key]
    if (typeof value === 'string') filters[key] = value
    else if (Array.isArray(value)) filters[key] = value[0] ?? ''
  }
  return filters
}

export function filterQuery(filters: CatalogFilters, page = 1): LocationQueryRaw {
  const query: LocationQueryRaw = {}
  for (const key of filterKeys) if (filters[key] !== defaults[key]) query[key] = filters[key]
  if (page > 1) query.page = String(page)
  return query
}

export function catalogURL(query: LocationQuery, scope: CatalogScope): string {
  const filters = readFilters(query)
  const params = new URLSearchParams({ ...filters, page: typeof query.page === 'string' ? query.page : '1', limit: '24' })
  // Do not silently accept only the first of several conflicting parameters.
  for (const key of [...filterKeys, 'page'] as const) {
    const values = query[key]
    if (Array.isArray(values)) {
      params.delete(key)
      for (const value of values) params.append(key, value ?? '')
    }
  }
  if (scope.kind === 'author') params.set('author', scope.name)
  if (scope.kind === 'category') params.set('tag', scope.name)
  return `/api/v1/books?${params}`
}
