// Package database menyiapkan koneksi ke Oracle.
package database

import (
	"database/sql"
	"fmt"
	"time"

	goora "github.com/sijms/go-ora/v2" // mendaftarkan driver "oracle" ke database/sql

	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
)

// OpenOracle menyiapkan connection pool ke Oracle.
//
// Fungsi ini sengaja TIDAK menunggu Oracle siap. Di Kubernetes, Oracle bisa
// butuh beberapa menit untuk start; kalau API langsung berhenti saat Oracle
// belum siap, pod API akan restart terus (CrashLoopBackOff). Status koneksi
// diperiksa lewat /readyz dengan db.PingContext.
//
// Panggil db.Close() saat aplikasi berhenti.
func OpenOracle(cfg config.DBConfig) (*sql.DB, error) {
	dsn := goora.BuildUrl(cfg.Host, cfg.Port, cfg.Service, cfg.User, cfg.Password, nil)

	// sql.Open belum menyambung ke Oracle; ia hanya menyiapkan pool.
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		// Jangan sertakan dsn di pesan error: isinya mengandung password.
		return nil, fmt.Errorf("menyiapkan koneksi oracle %s:%d/%s: %w", cfg.Host, cfg.Port, cfg.Service, err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxOpenConns)
	// Ganti koneksi lama secara berkala agar tidak memakai koneksi yang
	// sudah diputus oleh Oracle atau jaringan.
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	return db, nil
}
