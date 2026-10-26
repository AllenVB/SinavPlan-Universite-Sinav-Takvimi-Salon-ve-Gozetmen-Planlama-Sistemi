import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import * as api from './api'
import { cached, clearCache, setCacheOwner } from './offline'

type Ctx = { user: api.User | null; ready: boolean; login: (e: string, p: string) => Promise<void>; logout: () => Promise<void> }
const C = createContext<Ctx>(null as never)
export const useAuth = () => useContext(C)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<api.User | null>(null)
  const [ready, setReady] = useState(false)
  useEffect(() => {
    api.setLogoutHandler(() => setUser(null))
    if (!api.hasSession()) return setReady(true)
    // ağ yoksa son bilinen kullanıcıyla aç (oturum jetonu sekmede duruyor); sunucu reddederse çıkış
    cached('me', api.me).then((u) => { setCacheOwner(u.id); setUser(u) }, () => setUser(null)).finally(() => setReady(true))
  }, [])
  const value: Ctx = {
    user,
    ready,
    login: async (e, p) => {
      const u = await api.login(e, p)
      clearCache() // önceki kullanıcının saklanan listeleri kalmasın
      const me = await cached('me', api.me).catch(() => u) // ad bilgisi /me'den gelir; okunamazsa giriş yanıtı yeter
      setCacheOwner(me.id)
      setUser(me)
    },
    logout: async () => {
      await api.logout()
      clearCache()
      setCacheOwner(null)
      setUser(null)
    },
  }
  return <C.Provider value={value}>{children}</C.Provider>
}
