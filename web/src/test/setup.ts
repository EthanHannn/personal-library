import 'fake-indexeddb/auto'
import { afterEach, vi } from 'vitest'
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); localStorage.clear() })
