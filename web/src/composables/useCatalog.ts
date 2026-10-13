import { computed, toValue } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { CatalogPage, Category } from '../api'
import { catalogURL, defaults, filterQuery, readFilters } from '../catalog/query'
import type { CatalogFilters, CatalogScope } from '../catalog/query'
import { useRequest } from './useRequest'

export function useCatalog(scope: MaybeRefOrGetter<CatalogScope>) {
  const route = useRoute()
  const router = useRouter()
  const filters = computed(() => {
    const value = readFilters(route.query)
    if (toValue(scope).kind === 'author') value.author = ''
    if (toValue(scope).kind === 'category') value.tag = ''
    return value
  })
  const result = useRequest<CatalogPage>(() => catalogURL(route.query, toValue(scope)))
  const categories = useRequest<{ categories: Category[] }>('/api/v1/categories')
  const pageLink = (page: number) => ({ path: route.path, query: filterQuery(filters.value, page) })
  const apply = (next: CatalogFilters) => router.push({ path: route.path, query: filterQuery(next) })
  const reset = () => apply({ ...defaults })
  const showAll = () => apply({ ...defaults, minScore: '0', onlyContent: '0' })
  return { ...result, categories, filters, pageLink, apply, reset, showAll }
}
