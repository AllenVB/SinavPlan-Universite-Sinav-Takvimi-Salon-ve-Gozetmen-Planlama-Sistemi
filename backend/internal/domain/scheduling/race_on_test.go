//go:build race

package scheduling

// raceEnabled: yarış dedektörü çalışma süresini ~9 kat artırır; tam ölçek testi
// yalnızca normal derlemede koşar.
const raceEnabled = true
