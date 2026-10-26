import React from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import './index.css'

// Faz 22 C3: kurulabilir uygulama + çevrimdışı açılış. Geliştirme sunucusunda kayıt yapılmaz (HMR ile çakışır).
if ('serviceWorker' in navigator && import.meta.env.PROD) {
  addEventListener('load', () => { navigator.serviceWorker.register('/sw.js').catch(() => undefined) })
}

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
