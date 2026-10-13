import { computed, shallowRef } from 'vue'
import { defaultPreferences, themes, validPreferences } from '../reader/themes'
import type { Preferences } from '../reader/themes'

const key = 'library.reader.preferences.v1'

export function loadPreferences(): Preferences {
  try {
    const value = localStorage.getItem(key)
    if (value) return validPreferences(JSON.parse(value) ?? {})
    const migrated = validPreferences({
      theme: localStorage.getItem('zx_reader_theme') ?? 'light',
      fontSize: Number(localStorage.getItem('zx_reader_fontsize')),
      colorize: localStorage.getItem('zx_reader_colorize') !== 'false',
      rate: Number(localStorage.getItem('zx_reader_tts_rate')),
      voice: localStorage.getItem('zx_reader_tts_voice') ?? '',
    })
    try { localStorage.setItem(key, JSON.stringify(migrated)) } catch { /* Apply readable legacy values for this session even if storage is full. */ }
    return migrated
  } catch { return { ...defaultPreferences } }
}

export function useReaderPreferences() {
  const preferences = shallowRef(loadPreferences())
  const warning = shallowRef('')
  const theme = computed(() => themes.find(t => t.id === preferences.value.theme) ?? themes[0])
  const style = computed(() => Object.fromEntries(Object.entries(theme.value).filter(([k]) => !['id', 'name'].includes(k)).map(([k, v]) => [`--reader-${k}`, v])))
  function update(value: Preferences) {
    preferences.value = validPreferences(value)
    try { localStorage.setItem(key, JSON.stringify(preferences.value)); warning.value = '' } catch { warning.value = '设置仅在当前页面生效，浏览器未允许保存' }
  }
  return { preferences, theme, style, warning, update }
}
