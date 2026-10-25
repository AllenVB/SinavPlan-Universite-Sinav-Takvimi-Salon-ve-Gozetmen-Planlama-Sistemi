import jsQR from 'jsqr'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Banner } from './ui'

/** Kamera ile QR okur (jsQR). Aynı kodu 4 sn içinde tekrar göndermez. */
export function CameraScanner({ onCode, onClose }: { onCode: (code: string) => void; onClose: () => void }) {
  const video = useRef<HTMLVideoElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const last = useRef({ t: '', at: 0 })
  const [err, setErr] = useState<string | null>(null)
  const cb = useRef(onCode)
  cb.current = onCode
  const stop = useCallback(() => {
    const v = video.current
    ;(v?.srcObject as MediaStream | null)?.getTracks().forEach((t) => t.stop())
    if (v) v.srcObject = null
  }, [])
  useEffect(() => {
    let timer = 0, stopped = false
    ;(async () => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' }, audio: false })
        if (stopped) return stream.getTracks().forEach((t) => t.stop())
        const v = video.current!
        v.srcObject = stream
        await v.play()
        const tick = () => {
          if (stopped) return
          const c = canvas.current!, ctx = c.getContext('2d', { willReadFrequently: true })!
          if (v.readyState >= 2 && v.videoWidth) {
            c.width = v.videoWidth; c.height = v.videoHeight
            ctx.drawImage(v, 0, 0)
            const img = ctx.getImageData(0, 0, c.width, c.height)
            const code = jsQR(img.data, img.width, img.height, { inversionAttempts: 'dontInvert' })
            if (code?.data && (code.data !== last.current.t || Date.now() - last.current.at > 4000)) {
              last.current = { t: code.data, at: Date.now() }
              cb.current(code.data)
            }
          }
          timer = window.setTimeout(tick, 120)
        }
        tick()
      } catch (e) {
        setErr((e as Error).name === 'NotAllowedError' ? 'Kamera izni reddedildi. Tarayıcı ayarlarından izin verin veya 6 haneli kodu girin.' : 'Kamera açılamadı. 6 haneli kodu girebilirsiniz.')
      }
    })()
    return () => { stopped = true; clearTimeout(timer); stop() }
  }, [stop])
  if (err) return <Banner tone="err" icon="camera" title="Kamera kullanılamıyor" actions={<button className="btn sm" onClick={onClose}>Kapat</button>}>{err}</Banner>
  return (
    <div style={{ maxWidth: 420, margin: '0 auto 12px', position: 'relative' }}>
      <video ref={video} playsInline muted style={{ width: '100%', borderRadius: 12, background: '#000' }} data-testid="checkin-video" />
      <div style={{ position: 'absolute', inset: '15%', border: '3px solid rgba(255,255,255,.85)', borderRadius: 12, pointerEvents: 'none' }} />
      <canvas ref={canvas} hidden />
      <p className="small muted" style={{ textAlign: 'center' }}>Gözetmenin ekranındaki QR'ı çerçeveye hizalayın; okununca otomatik gönderilir.</p>
    </div>
  )
}
