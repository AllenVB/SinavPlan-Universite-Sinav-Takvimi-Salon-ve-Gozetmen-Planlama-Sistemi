import { useEffect, useRef, useState } from 'react'
import { get, post } from './api'
import { Icon } from './icons'
import { useLive } from './live'

/**
 * Bildirim kutusu (her rol): görev/sınav hatırlatmaları, "yoklamanız alınmadı", mazeret kararı, gözetmen devri,
 * salon değişikliği. Sunucu zamana bağlı olanları okuma sırasında üretir; ekran 30 sn'de bir tazeler.
 * Bildirime tıklamak onu okundu yapar ve ilgili ekrana götürür.
 */
type Item = { id: number; kind: string; title: string; body: string; link: string; created_at: string; read_at: string | null }
type Box = { items: Item[]; unread: number }

const ago = (iso: string) => {
  const m = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 60000))
  return m < 1 ? 'şimdi' : m < 60 ? `${m} dk önce` : m < 1440 ? `${Math.round(m / 60)} sa önce` : `${Math.round(m / 1440)} gün önce`
}

export function Inbox() {
  const box = useLive(() => get<Box>('/me/notifications'), [], 30_000)
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const out = (e: MouseEvent) => { if (!root.current?.contains(e.target as Node)) setOpen(false) }
    const esc = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    addEventListener('mousedown', out); addEventListener('keydown', esc)
    return () => { removeEventListener('mousedown', out); removeEventListener('keydown', esc) }
  }, [open])
  const d = box.data
  const go = async (n: Item) => {
    if (!n.read_at) await post(`/me/notifications/${n.id}/read`).catch(() => undefined)
    setOpen(false)
    if (n.link) location.hash = n.link.replace(/^#/, '')
    void box.reload()
  }
  const all = async () => { await post('/me/notifications/read-all').catch(() => undefined); void box.reload() }
  return (
    <div className="inbox" ref={root}>
      <button className="bellbtn" aria-label={`Bildirimler: ${d?.unread ?? 0} okunmamış`} aria-expanded={open} aria-haspopup="dialog" title="Bildirimler" onClick={() => setOpen(!open)} data-testid="inbox-btn">
        <Icon name="inbox" size={18} />
        {!!d?.unread && <span className="count" data-testid="inbox-count">{d.unread > 99 ? '99+' : d.unread}</span>}
      </button>
      {open && (
        <div className="inboxpanel" role="dialog" aria-label="Bildirimler" data-testid="inbox-panel">
          <div className="row between"><b>Bildirimler</b><button className="btn sm" disabled={!d?.unread} onClick={() => void all()}>Tümünü okundu yap</button></div>
          {!d || d.items.length === 0 ? <p className="small muted" style={{ margin: '12px 0 4px' }}>Henüz bildirim yok. Sınav/görev hatırlatmaları, mazeret kararları ve salon değişiklikleri burada görünür.</p> : (
            <ul className="inboxlist">
              {d.items.map((n) => (
                <li key={n.id}>
                  <button className={`inboxitem ${n.read_at ? '' : 'unread'}`} onClick={() => void go(n)}>
                    <span className="small muted">{ago(n.created_at)}</span>
                    <b>{n.title}</b>
                    <span className="small">{n.body}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}
