// Veri üretici / simülatör. İki modda çalışır:
//
//	seed  --seed=N   deterministik demo verisini yükler
//	live  --duration kendi sınav günü yoklama olaylarını API üzerinden üretir
//	reset --seed=N   veritabanını ilk seed haline döndürür
//
// FAZ 1 DURUMU: üretim mantığı henüz yazılmadı (Faz 2). Komut argümanlarını
// doğrular, ne yapacağını bildirir ve çıkar. Hiçbir veri üretmiyor.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/AllenVB/sinavplan/backend/internal/logging"
)

func main() {
	log := logging.New(os.Stdout, os.Getenv("LOG_LEVEL"))

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "kullanım: simulator <seed|live|reset> [bayraklar]")
		os.Exit(2)
	}

	mode := os.Args[1]
	fs := flag.NewFlagSet(mode, flag.ExitOnError)
	seed := fs.Int64("seed", 42, "deterministik üretim tohumu")
	duration := fs.String("duration", "2m", "live modunun süresi")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}

	switch mode {
	case "seed", "live", "reset":
	default:
		log.Error("bilinmeyen mod", slog.String("mode", mode))
		os.Exit(2)
	}

	log.Warn("simülatör HENÜZ UYGULANMADI — veri üretimi Faz 2'de yazılacak",
		slog.String("mode", mode),
		slog.Int64("seed", *seed),
		slog.String("duration", *duration),
		slog.String("durum", "yer tutucu"),
	)
}
