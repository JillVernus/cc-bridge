import { createI18n } from 'vue-i18n'
import zhCN from '../locales/zh-CN'
import en from '../locales/en'

// Get saved locale from localStorage or detect from browser
const getDefaultLocale = (): string => {
  const saved = localStorage.getItem('locale')
  if (saved && ['zh-CN', 'en'].includes(saved)) {
    return saved
  }
  // Detect from browser
  const browserLang = navigator.language
  if (browserLang.startsWith('zh')) {
    return 'zh-CN'
  }
  return 'en'
}

const i18n = createI18n({
  legacy: false, // Use Composition API
  locale: getDefaultLocale(),
  fallbackLocale: 'en',
  messages: {
    'zh-CN': zhCN,
    en: en,
  },
})

export default i18n

// Export type for useI18n
export type MessageSchema = typeof zhCN
