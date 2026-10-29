// Package gormdb membuka koneksi GORM ke Oracle memakai driver resmi
// Oracle (gorm-oracle + godror).
//
// godror memakai CGO dan butuh Oracle Instant Client saat program berjalan.
// Karena itu package ini sengaja dipisah dari package database (go-ora,
// pure Go): program yang tidak mengimpor gormdb, seperti API gateway, tetap
// bisa di-build dengan CGO_ENABLED=0.
package gormdb

import (
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/godror/godror"
	"github.com/oracle-samples/gorm-oracle/oracle"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
)

// Open menyiapkan *gorm.DB ke Oracle. Seperti database.OpenOracle, fungsi ini
// tidak menunggu Oracle siap; status koneksi diperiksa lewat PingContext.
func Open(cfg config.DBConfig) (*gorm.DB, *sql.DB, error) {
	var params godror.ConnectionParams
	params.Username = cfg.User
	params.Password = godror.NewPassword(cfg.Password)
	params.ConnectString = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)) + "/" + cfg.Service
	// Semua waktu dibaca dan ditulis sebagai UTC.
	params.Timezone = time.UTC

	sqlDB := sql.OpenDB(godror.NewConnector(params))
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	db, err := gorm.Open(oracle.New(oracle.Config{
		Conn: sqlDB,
		// Tanpa tanda kutip, Oracle membaca nama tabel/kolom sebagai huruf
		// besar, sama seperti tabel buatan migrasi Flyway (USERS, EMAIL, ...).
		SkipQuoteIdentifiers: true,
	}), &gorm.Config{
		// Oracle TIMESTAMP menyimpan sampai mikrodetik; dipotong di sini supaya
		// nilai yang dikembalikan Register sama dengan yang nanti dibaca ulang.
		NowFunc: func() time.Time { return time.Now().UTC().Truncate(time.Microsecond) },
		// Logger bawaan GORM mencetak SQL beserta nilainya (termasuk hash
		// password), jadi dimatikan. Error tetap dikembalikan ke pemanggil
		// dan dicatat oleh lapisan gRPC.
		Logger: logger.Default.LogMode(logger.Silent),
		// Repository menangani transaksi sendiri.
		SkipDefaultTransaction: true,
	})
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("menyiapkan gorm oracle %s:%d/%s: %w", cfg.Host, cfg.Port, cfg.Service, err)
	}
	return db, sqlDB, nil
}
