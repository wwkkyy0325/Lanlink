import { ref, computed, type Ref } from 'vue'
import zh from './zh'
import en from './en'

type Locale = 'zh' | 'en'
type Translation = typeof zh

const messages: Record<Locale, Translation> = { zh, en }

const currentLocale: Ref<Locale> = ref(
  (localStorage.getItem('lanlink-locale') as Locale) || 'zh'
)

function t(path: string, params?: Record<string, string | number>): string {
  const keys = path.split('.')
  let value: any = messages[currentLocale.value]

  for (const key of keys) {
    if (value == null) return path
    value = value[key]
  }

  if (typeof value !== 'string') return path

  if (params) {
    return value.replace(/\{(\w+)\}/g, (_, k) => String(params[k] ?? `{${k}}`))
  }

  return value
}

function setLocale(locale: Locale) {
  currentLocale.value = locale
  localStorage.setItem('lanlink-locale', locale)
}

function toggleLocale() {
  setLocale(currentLocale.value === 'zh' ? 'en' : 'zh')
}

const locale = computed(() => currentLocale.value)
const localeLabel = computed(() => currentLocale.value === 'zh' ? 'EN' : '中文')

function te(englishError: string): string {
  const errors = messages[currentLocale.value].upnpErrors as Record<string, string>
  // Try exact match first
  if (errors[englishError]) return errors[englishError]
  // Try partial match
  for (const [key, val] of Object.entries(errors)) {
    if (englishError.includes(key)) return val
  }
  return englishError
}

export function useI18n() {
  return { t, te, locale, localeLabel, setLocale, toggleLocale }
}
