import { createApp } from 'vue'
import vuetify from './plugins/vuetify'
import i18n from './plugins/i18n'
import App from './App.vue'
import './assets/style.css' // Tailwind + DaisyUI

const app = createApp(App)

app.use(vuetify)
app.use(i18n)

app.mount('#app')
