import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import * as api from './api'

type Ctx = { user: api.User | null; ready: boolean; login: (e: string, p: string) => Promise<void>; logout: () => Promise<void> }
const C = createContext<Ctx>(null as never)
export const useAuth = () => useContext(C)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<api.User | null>(null)
  const [ready, setReady] = useState(false)
  useEffect(() => {
    api.setLogoutHandler(() => setUser(null))
    if (!api.hasSession()) return setReady(true)
    api.me().then(setUser, () => setUser(null)).finally(() => setReady(true))
  }, [])
  const value: Ctx = {
    user,
    ready,
    login: async (e, p) => setUser(await api.login(e, p)),
    logout: async () => {
      await api.logout()
      setUser(null)
    },
  }
  return <C.Provider value={value}>{children}</C.Provider>
}
