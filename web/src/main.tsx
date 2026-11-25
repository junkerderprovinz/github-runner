import '@fontsource-variable/hanken-grotesk'
import '@fontsource-variable/jetbrains-mono'
import './base.css'
import './line-map.css'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { start } from './data/api'

start()
createRoot(document.getElementById('app')!).render(<App />)
