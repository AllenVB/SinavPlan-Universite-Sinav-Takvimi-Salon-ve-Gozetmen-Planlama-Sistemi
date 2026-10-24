package export

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Event, bir takvim olayı. UID kararlıdır: aynı olay yeniden içe aktarılınca kopyalanmaz.
type Event struct {
	UID      string
	Start    time.Time
	End      time.Time
	Summary  string
	Location string
	Desc     string
}

func icsEscape(s string) string {
	s = strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", "").Replace(clean(s))
	return s
}

// fold, RFC 5545 satır katlama (≤75 oktet; çok baytlı karakteri bölmez).
func fold(line string) string {
	var b strings.Builder
	n := 0
	for _, r := range line {
		sz := len(string(r))
		if n+sz > 74 {
			b.WriteString("\r\n ")
			n = 1
		}
		b.WriteRune(r)
		n += sz
	}
	return b.String()
}

// WriteICS, olayları UTC zamanlı bir iCalendar olarak yazar (CRLF satır sonları).
func WriteICS(w io.Writer, calName string, events []Event, now time.Time) error {
	var b strings.Builder
	line := func(s string) { b.WriteString(fold(s)); b.WriteString("\r\n") }
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//SinavPlan//TR")
	line("CALSCALE:GREGORIAN")
	line("METHOD:PUBLISH")
	line("X-WR-CALNAME:" + icsEscape(calName))
	const f = "20060102T150405Z"
	for _, e := range events {
		line("BEGIN:VEVENT")
		line("UID:" + e.UID)
		line("DTSTAMP:" + now.UTC().Format(f))
		line("DTSTART:" + e.Start.UTC().Format(f))
		line("DTEND:" + e.End.UTC().Format(f))
		line("SUMMARY:" + icsEscape(e.Summary))
		if e.Location != "" {
			line("LOCATION:" + icsEscape(e.Location))
		}
		if e.Desc != "" {
			line("DESCRIPTION:" + icsEscape(e.Desc))
		}
		line("END:VEVENT")
	}
	line("END:VCALENDAR")
	_, err := io.WriteString(w, b.String())
	if err != nil {
		return fmt.Errorf("ics yazılamadı: %w", err)
	}
	return nil
}
