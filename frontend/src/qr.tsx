import qrcode from 'qrcode-generator'

/** Jetonu gerçek QR koduna (SVG) çevirir. Hata düzeltme M; uzun jetonda sürüm otomatik büyür. */
export function QR({ value, size = 220 }: { value: string; size?: number }) {
  let svg = ''
  try {
    const q = qrcode(0, 'M')
    q.addData(value)
    q.make()
    svg = q.createSvgTag({ cellSize: 4, margin: 4, scalable: true })
  } catch {
    return <div className="alert err">QR üretilemedi (jeton çok uzun).</div>
  }
  return (
    <div
      role="img"
      aria-label="Sınav kartı QR kodu"
      data-testid="qr-svg"
      style={{ width: size, maxWidth: '100%', background: '#fff', padding: 8, borderRadius: 10, margin: '0 auto' }}
      // qrcode-generator yalnızca kendi ürettiği, değer içermeyen SVG yollarını döndürür
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  )
}
