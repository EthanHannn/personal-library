// Retain the six established reader palettes and legacy preference identifiers.
export const themes = [
  { id: 'light', name: '月白', shell: '#E8EEF0', paper: '#F9FBFA', text: '#2A3438', title: '#2D6874', muted: '#52666F', accent: '#397F8E', soft: '#DCECEF', border: '#D4E0E2', quote: '#9B4F61', bracket: '#536F98', punctuation: '#356E66', marker: '#AB4753', number: '#89602B', english: '#75529A' },
  { id: 'sepia', name: '宣纸', shell: '#E7DDC7', paper: '#F7EFDF', text: '#4A3B31', title: '#9A642D', muted: '#746354', accent: '#945C29', soft: '#EEDFBE', border: '#DED0B5', quote: '#9B4141', bracket: '#506687', punctuation: '#3D6A60', marker: '#A44639', number: '#805923', english: '#76567C' },
  { id: 'celadon', name: '青瓷', shell: '#D9E6DE', paper: '#EEF4EE', text: '#293A33', title: '#2E6A58', muted: '#52695C', accent: '#397B65', soft: '#D3E6DA', border: '#C8DCCF', quote: '#985468', bracket: '#4E6C8A', punctuation: '#326C63', marker: '#A64A44', number: '#756126', english: '#69588D' },
  { id: 'peach', name: '桃雾', shell: '#F0E0E1', paper: '#FFF5F2', text: '#49363A', title: '#A14F65', muted: '#796165', accent: '#A14F65', soft: '#F2DDE1', border: '#E9D3D4', quote: '#A14848', bracket: '#5B6792', punctuation: '#416E64', marker: '#B04455', number: '#8E6637', english: '#805484' },
  { id: 'ocean', name: '海夜', shell: '#0E1922', paper: '#172631', text: '#CBD9DE', title: '#79BECA', muted: '#9BB0B9', accent: '#69AEBB', soft: '#233D48', border: '#29414D', quote: '#E2A0A8', bracket: '#92AED9', punctuation: '#72BCAF', marker: '#E6848F', number: '#D5B777', english: '#BE9BD2' },
  { id: 'dark', name: '墨黑', shell: '#101214', paper: '#1B1E20', text: '#C9C4BA', title: '#D7AD70', muted: '#A5A198', accent: '#C2965B', soft: '#332C23', border: '#343536', quote: '#D68D82', bracket: '#8EA7CA', punctuation: '#70A99B', marker: '#DF7B75', number: '#C8AA68', english: '#B493C1' },
] as const

export interface Preferences { theme: string; fontSize: number; colorize: boolean; rate: number; voice: string }
export const defaultPreferences: Preferences = { theme: 'light', fontSize: 20, colorize: true, rate: 1, voice: '' }

export function validPreferences(raw: Partial<Preferences>): Preferences {
  return {
    theme: themes.some(t => t.id === raw.theme) ? raw.theme! : 'light',
    fontSize: Number.isFinite(raw.fontSize) && raw.fontSize! >= 14 && raw.fontSize! <= 30 ? raw.fontSize! : 20,
    colorize: typeof raw.colorize === 'boolean' ? raw.colorize : true,
    rate: Number.isFinite(raw.rate) && raw.rate! >= 0.6 && raw.rate! <= 2 ? raw.rate! : 1,
    voice: typeof raw.voice === 'string' ? raw.voice.slice(0, 512) : '',
  }
}
