// QR gidiş-dönüş testi: arayüzdeki üretici (qrcode-generator) ile üretilen kod, tablet tarayıcısının
// kullandığı çözücüyle (jsQR) aynen çözülmeli. Çalıştırma: node deploy/ui-smoke/qr-roundtrip.cjs (frontend/node_modules gerekir)
const path = require('path');
const nm = path.join(__dirname, '../../frontend/node_modules');
const qrcode = require(path.join(nm, 'qrcode-generator'));
const jsQR = require(path.join(nm, 'jsqr'));
let fail = 0;
for (const len of [40, 120, 200, 260]) {
  const value = Array.from({ length: len }, (_, i) => 'abcdefghijklmnopqrstuvwxyz0123456789-_.'[(i * 7 + len) % 39]).join('');
  const q = qrcode(0, 'M'); q.addData(value); q.make();
  const n = q.getModuleCount(), scale = 6, margin = 4, size = (n + margin * 2) * scale;
  const data = new Uint8ClampedArray(size * size * 4).fill(255);
  for (let r = 0; r < n; r++) for (let c = 0; c < n; c++) if (q.isDark(r, c))
    for (let y = 0; y < scale; y++) for (let x = 0; x < scale; x++) { const i = (((r + margin) * scale + y) * size + (c + margin) * scale + x) * 4; data[i] = data[i + 1] = data[i + 2] = 0; }
  const out = jsQR(data, size, size);
  const ok = out && out.data === value;
  console.log(ok ? 'PASS' : 'FAIL', `QR gidiş-dönüş: ${len} karakterlik jeton, ${n}x${n} modül`);
  if (!ok) fail++;
}
process.exit(fail ? 1 : 0);
