/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Backend taban adresi; derleme sırasında gömülür. */
  readonly VITE_API_BASE_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
