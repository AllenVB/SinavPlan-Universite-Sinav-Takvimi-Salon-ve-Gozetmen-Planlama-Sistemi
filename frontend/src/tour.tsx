import { useEffect, useRef, useState } from 'react'
import type { Role } from './api'
import { useAuth } from './auth'
import { Icon } from './icons'

/**
 * Rehberli tur: değerlendiriciyi sistemin öne çıkan işlerinden sırayla geçirir. Her adım gerçek ekranı açar;
 * adım başka bir rolün işiyse tek tıkla o demo hesabına geçilir. Durum tarayıcıda tutulur, böylece hesap
 * değişince tur kaldığı yerden sürer. Kendiliğinden açılmaz: üst çubuktaki "Tur" düğmesiyle başlar.
 */
type Step = { title: string; role: Role; email: string; who: string; path: string; body: string; try: string }

export const TOUR: Step[] = [
  { title: 'Ana panel', role: 'secretary', email: 'sekreter@sinavplan.test', who: 'Bölüm sekreteri', path: 'panel',
    body: 'Başlangıç rehberi adımları gerçek veriden işaretlenir (çizelge var mı, yayında mı, gözetmenler atandı mı, oturum sürüyor mu). Son olaylar denetim kaydından canlı akar.',
    try: 'Fakülteler kartında bir fakültenin öğretim üyelerini açın.' },
  { title: 'Takvim üretimi', role: 'secretary', email: 'sekreter@sinavplan.test', who: 'Bölüm sekreteri', path: 'takvim?tab=uret',
    body: '301 sınav, ortak öğrencisi olanlar aynı oturuma düşmeyecek ve öğrenci başına günde en çok 2 sınav olacak şekilde dağıtılır (DSatur + benzetimli tavlama). Sonuç bağımsız doğrulayıcıdan geçer: sert ihlal 0.',
    try: '"Takvimi oluştur" deyin; bitince yeni taslak sürüm listede belirir. Prova sekmesi kaydetmeden dener, Karşılaştır iki sürümün farkını gösterir.' },
  { title: 'Salon ve oturma planı', role: 'secretary', email: 'sekreter@sinavplan.test', who: 'Bölüm sekreteri', path: 'salonlar',
    body: 'Aynı sınavın öğrencileri yan yana oturmaz; engelli öğrenciler erişilebilir koltuğa yerleşir. Krokide kapı ve tahta görünür.',
    try: '"Sınav oturma planı"ndan bir sınav seçin; "Salon envanteri"nden bir salonun kapısını düzenleyin.' },
  { title: 'Adil gözetmen ataması', role: 'secretary', email: 'sekreter@sinavplan.test', who: 'Bölüm sekreteri', path: 'gozetmenler',
    body: 'Görev yükü en çok/en az farkı 1; kendi dersinde gözetmenlik, ardışık oturum yok; 50+ öğrencili salonda 2 gözetmen.',
    try: 'Bir gözetmen için "Hasta / çıkar" deyin: yalnız onun görevleri değişir, diğer atamalar korunur.' },
  { title: 'Sınav günü izleme', role: 'secretary', email: 'sekreter@sinavplan.test', who: 'Bölüm sekreteri', path: 'canli',
    body: '"Prompt Mühendisliği" sınavı şu an sürüyor. Salon kartları, geldi/gelmedi çubukları ve olay akışı 5 sn\'de bir kendiliğinden yenilenir.',
    try: 'Bir sonraki adımda gözetmen olarak yoklama açıp burada sayacın arttığını görebilirsiniz.' },
  { title: 'Gözetmen: QR ile yoklama', role: 'invigilator', email: 'gozetmen@sinavplan.test', who: 'Gözetmen (Ömer ASLAN)', path: 'yoklama',
    body: 'Gözetmen şu anki görevini seçip "Yeni QR oluştur" der: ekranda 30 sn\'de bir değişen QR ve yedek 6 haneli kod çıkar. Ağ kesilse de elle işaretlenir, sonra senkronlanır.',
    try: 'Süren görevin yanındaki düğmeye basın, "Yeni QR oluştur" deyin ve 6 haneli kodu not edin.' },
  { title: 'Öğrenci: koda gir, kartını gör', role: 'student', email: 'ogrenci@sinavplan.test', who: 'Öğrenci', path: 'programim',
    body: 'Öğrenci gözetmenin kodunu girer ya da QR\'ı okutur; yoklamada otomatik "geldi" olur. Sınav kartında salon, koltuk ve kapı krokisi var; program .ics olarak takvime eklenir.',
    try: 'Not ettiğiniz kodu girin; ardından gözetmen ekranındaki sayaç artar.' },
  { title: 'Mazeret ve çakışmalar', role: 'secretary', email: 'sekreter@sinavplan.test', who: 'Bölüm sekreteri', path: 'mazeret',
    body: 'Öğrenci ve gözetmen mazeretleri tek masada onaylanır/reddedilir; onaylı mazeretli sınava kart verilmez. Çakışmalar sekmesi yoklama çakışmalarının nasıl çözüldüğünü gösterir.',
    try: 'Bekleyen mazereti onaylayın; çanın üzerindeki sayı azalır.' },
  { title: 'Kanıtlar: kurallar canlı doğrulanıyor', role: 'secretary', email: 'sekreter@sinavplan.test', who: 'Bölüm sekreteri', path: 'kanitlar',
    body: 'Yayındaki planın tüm kuralları (çakışma, günde ≤2 sınav, kapasite, lab, yan yana oturmama, engelli koltuğu, gözetmen kuralları) çözücüden bağımsız SQL ile yeniden sayılır. Her kural bir de bilerek bozulmuş sanal planla denenir: doğrulayıcı bozulmayı yakalamalı.',
    try: '"Yeniden çalıştır" deyin; 12/12 denetimin her birinde gerçek plan 0 ihlal, bozulmuş plan ≥1 yakalandı görünür.' },
]

const KEY = 'sinavplan-tour'
type State = { open: boolean; step: number }
const load = (): State => { try { return { open: false, step: 0, ...JSON.parse(localStorage.getItem(KEY) ?? '{}') } } catch { return { open: false, step: 0 } } }
const save = (s: State) => { try { localStorage.setItem(KEY, JSON.stringify(s)) } catch { /* depolama kapalı: tur yalnız bu sayfada sürer */ } }

/** Herhangi bir yerden turu başlatır (üst çubuk, ana panel). */
export const openTour = () => dispatchEvent(new Event('sinavplan-tour-open'))

export function TourButton() {
  return <button className="btn sm" onClick={openTour} title="Sistemi adım adım tanıtan tur"><Icon name="zap" size={15} /> Tur</button>
}

export function Tour() {
  const { user, login, logout } = useAuth()
  const [st, setSt] = useState<State>(load)
  const [busy, setBusy] = useState(false)
  const head = useRef<HTMLHeadingElement>(null)
  const set = (s: State) => { setSt(s); save(s) }
  useEffect(() => {
    const f = () => set({ open: true, step: load().step })
    addEventListener('sinavplan-tour-open', f)
    return () => removeEventListener('sinavplan-tour-open', f)
  }, [])
  useEffect(() => { if (st.open) head.current?.focus() }, [st.open, st.step])
  useEffect(() => {
    if (!st.open) return
    const esc = (e: KeyboardEvent) => { if (e.key === 'Escape') set({ ...load(), open: false }) }
    addEventListener('keydown', esc)
    return () => removeEventListener('keydown', esc)
  }, [st.open])
  if (!st.open || !user) return null
  const i = Math.min(st.step, TOUR.length - 1)
  const s = TOUR[i]
  const mine = user.roles.includes(s.role)
  const go = (n: number) => {
    set({ open: true, step: n })
    if (user.roles.includes(TOUR[n].role)) location.hash = `/${TOUR[n].path}`
  }
  const switchTo = async () => {
    setBusy(true)
    try {
      await logout()
      location.hash = `/${s.path}`
      await login(s.email, 'Demo!1234')
    } finally { setBusy(false) }
  }
  return (
    <section className="tourpanel noprint" role="dialog" aria-modal="false" aria-labelledby="tour-h" data-testid="tour">
      <div className="row between">
        <span className="caps">Rehberli tur · {i + 1} / {TOUR.length}</span>
        <button className="btn sm" onClick={() => set({ ...st, open: false })} aria-label="Turu kapat">✕</button>
      </div>
      <div className="tourdots" aria-hidden>{TOUR.map((_, k) => <i key={k} className={k === i ? 'on' : k < i ? 'done' : ''} />)}</div>
      <h2 id="tour-h" tabIndex={-1}>{s.title}</h2>
      <p className="small">{s.body}</p>
      <p className="small"><b>Deneyin:</b> {s.try}</p>
      {mine ? (
        <a className="btn sm" href={`#/${s.path}`}>Ekranı aç</a>
      ) : (
        <div className="alert warn" style={{ margin: 0 }}><div className="small">Bu adım <b>{s.who}</b> hesabıyla: <span className="mono">{s.email}</span><div style={{ marginTop: 8 }}><button className="btn primary sm" disabled={busy} onClick={() => void switchTo()} data-testid="tour-switch">{busy ? 'Geçiliyor…' : 'Bu hesapla devam et'}</button></div></div></div>
      )}
      <div className="row between" style={{ marginTop: 12 }}>
        <button className="btn sm" disabled={i === 0} onClick={() => go(i - 1)}>← Geri</button>
        {i < TOUR.length - 1 ? <button className="btn primary sm" onClick={() => go(i + 1)}>İleri →</button> : <button className="btn primary sm" onClick={() => set({ open: false, step: 0 })}>Turu bitir</button>}
      </div>
    </section>
  )
}
