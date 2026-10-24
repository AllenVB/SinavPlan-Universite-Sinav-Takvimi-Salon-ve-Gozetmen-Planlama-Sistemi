// SınavPlan HTTP API sunucusu.
package main

import (
	_ "time/tzdata" // sınav saatlerini yerel saat diliminde yorumlamak için; imaja tzdata paketi kurmaya gerek kalmıyor

	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/auth"
	"github.com/AllenVB/sinavplan/backend/internal/config"
	"github.com/AllenVB/sinavplan/backend/internal/httpapi"
	"github.com/AllenVB/sinavplan/backend/internal/logging"
	"github.com/AllenVB/sinavplan/backend/internal/store"
	"github.com/prometheus/client_golang/prometheus"
)

// version, derleme sırasında -ldflags ile verilir.
var version = "dev"

func main() {
	if err := run(); err != nil {
		// Logger henüz kurulmamış olabileceği için doğrudan stderr'e yazıyoruz.
		slog.Error("api başlatılamadı", slog.String("err", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logging.New(os.Stdout, cfg.LogLevel)
	slog.SetDefault(log)

	log.Info("api başlıyor",
		slog.String("version", version),
		slog.String("env", cfg.Env),
		slog.String("addr", cfg.HTTPAddr),
	)

	// Bağlantı havuzunu kurarken veritabanının hazır olmasını beklemiyoruz:
	// /health'in 503 dönebilmesi ve kaos senaryosunda sürecin ayakta kalması
	// gerekiyor. pgxpool bağlantıyı tembel kurar, bu yüzden Open'ın başarısı
	// veritabanının ayakta olduğu anlamına gelmez — bu bilinçli.
	ctx := context.Background()
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		log.Warn("açılışta veritabanına ulaşılamadı; /health 503 dönecek",
			slog.String("err", err.Error()))
	} else {
		log.Info("veritabanı bağlantısı doğrulandı")
	}

	authSvc, err := auth.NewService(db.Pool, auth.NewIssuer(cfg.JWTSecret, cfg.AccessTokenTTL), cfg.RefreshTokenTTL)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewServer(httpapi.Options{
			DB:             db,
			Logger:         log,
			Version:        version,
			RequestTimeout: cfg.RequestTimeout,
			Registry:       prometheus.NewRegistry(),
			Pool:           db.Pool,
			Auth:           authSvc,
			QR:             store.NewQR(cfg.QRHMACSecret),
			SeatingStrict:  cfg.SeatingStrict,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// WriteTimeout bilinçli olarak istek zaman aşımından uzun: yanıtı
		// kesmek yerine handler'ın ortak biçimde 504 dönmesini istiyoruz.
		WriteTimeout: cfg.RequestTimeout + 10*time.Second,
	}

	// SIGTERM/SIGINT geldiğinde kapanışı tetikleyen kanal. Docker durdurmada
	// SIGTERM gönderiyor; 15 sn içinde açık istekleri bitirip çıkıyoruz.
	shutdownCtx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		log.Info("http dinleniyor", slog.String("addr", cfg.HTTPAddr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			return err
		}
		return nil

	case <-shutdownCtx.Done():
		log.Info("kapanma sinyali alındı, düzgün kapanış başlıyor",
			slog.Duration("timeout", cfg.ShutdownTimeout))

		// signal.NotifyContext'i serbest bırak: ikinci bir SIGTERM artık
		// süreci doğrudan düşürebilsin (operatör beklemek istemezse).
		stop()

		graceCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(graceCtx); err != nil {
			log.Error("düzgün kapanış süresi aşıldı, bağlantılar kesiliyor",
				slog.String("err", err.Error()))
			_ = srv.Close()
			return err
		}
		log.Info("düzgün kapanış tamamlandı")
		return nil
	}
}
