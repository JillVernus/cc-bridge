import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'

export type LocaleType = 'zh-CN' | 'en'

export function useLocale() {
  const { locale } = useI18n()

  const currentLocale = computed<LocaleType>(() => locale.value as LocaleType)

  const setLocale = (newLocale: LocaleType) => {
    locale.value = newLocale
    localStorage.setItem('locale', newLocale)
    // Update HTML lang attribute
    document.documentElement.lang = newLocale === 'zh-CN' ? 'zh-CN' : 'en'
  }

  const toggleLocale = () => {
    setLocale(currentLocale.value === 'zh-CN' ? 'en' : 'zh-CN')
  }

  // Initialize HTML lang attribute
  const init = () => {
    document.documentElement.lang = currentLocale.value === 'zh-CN' ? 'zh-CN' : 'en'
  }

  return {
    currentLocale,
    setLocale,
    toggleLocale,
    init,
  }
}
