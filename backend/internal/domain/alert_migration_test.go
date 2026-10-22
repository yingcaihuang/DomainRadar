package domain_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"domainradar/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestAlertAccountScopeMigration(t *testing.T) {
	dsn := os.Getenv("DOMAINRADAR_MIGRATION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set DOMAINRADAR_MIGRATION_TEST_DATABASE_URL to run PostgreSQL migration tests")
	}
	for _, existingColumn := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing-account-column=%t", existingColumn), func(t *testing.T) {
			schema := fmt.Sprintf("alert_migration_test_%d", time.Now().UnixNano())
			config, err := pgx.ParseConfig(dsn)
			require.NoError(t, err)
			config.RuntimeParams["search_path"] = schema
			conn := stdlib.OpenDB(*config)
			conn.SetMaxOpenConns(1)
			_, err = conn.Exec("CREATE SCHEMA " + schema)
			require.NoError(t, err)
			t.Cleanup(func() {
				_, rollbackErr := conn.Exec("ROLLBACK")
				require.NoError(t, rollbackErr)
				_, dropErr := conn.Exec("DROP SCHEMA " + schema + " CASCADE")
				require.NoError(t, dropErr)
				require.NoError(t, conn.Close())
			})
			migration := func(name string) error {
				t.Helper()
				content, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
				require.NoError(t, err)
				_, err = conn.Exec(string(content))
				return err
			}
			require.NoError(t, migration("000001_initial_schema.up.sql"))
			require.NoError(t, migration("000002_registrar_sync_options.up.sql"))
			if existingColumn {
				_, err = conn.Exec("ALTER TABLE alerts ADD COLUMN registrar_account_id BIGINT")
				require.NoError(t, err)
			}
			var domainID uint
			require.NoError(t, conn.QueryRow("INSERT INTO domains (domain_name) VALUES ('migration.test') RETURNING id").Scan(&domainID))
			_, err = conn.Exec("INSERT INTO alerts (domain_id, alert_type, severity) VALUES ($1, 'expiration', 'warning')", domainID)
			require.NoError(t, err)
			require.NoError(t, migration("000003_alert_account_scope.up.sql"))
			require.NoError(t, migration("000003_alert_account_scope.up.sql"))

			db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{})
			require.NoError(t, err)
			domainAlert := domain.Alert{DomainID: &domainID, AlertType: "expiration", Severity: "warning", GeneratedAt: time.Now()}
			require.NoError(t, db.Create(&domainAlert).Error)
			registrarConfig := domain.RegistrarConfig{RegistrarType: "cloudflare", DisplayName: "Cloudflare"}
			require.NoError(t, db.Create(&registrarConfig).Error)
			account := domain.RegistrarAccount{RegistrarConfigID: registrarConfig.ID, AccountName: "migration"}
			require.NoError(t, db.Create(&account).Error)
			accountAlert := domain.Alert{RegistrarAccountID: &account.ID, AlertType: "sync_failure", Severity: "warning", GeneratedAt: time.Now()}
			require.NoError(t, db.Create(&accountAlert).Error)
			var got domain.Alert
			require.NoError(t, db.First(&got, accountAlert.ID).Error)
			require.Nil(t, got.DomainID)
			require.Equal(t, &account.ID, got.RegistrarAccountID)

			zero := uint(0)
			require.Error(t, db.Create(&domain.Alert{DomainID: &zero, AlertType: "expiration", Severity: "warning", GeneratedAt: time.Now()}).Error)
			require.Error(t, db.Create(&domain.Alert{RegistrarAccountID: &zero, AlertType: "sync_failure", Severity: "warning", GeneratedAt: time.Now()}).Error)
			require.Error(t, migration("000003_alert_account_scope.down.sql"), "rollback must not silently delete account-level alerts")
			_, err = conn.Exec("ROLLBACK")
			require.NoError(t, err)
			require.NoError(t, db.First(&domain.Alert{}, accountAlert.ID).Error)

			require.NoError(t, db.Delete(&account).Error)
			var count int64
			require.NoError(t, db.Model(&domain.Alert{}).Where("id = ?", accountAlert.ID).Count(&count).Error)
			require.Zero(t, count, "deleting the account must cascade to its alert")
			require.NoError(t, db.Model(&domain.Alert{}).Count(&count).Error)
			require.EqualValues(t, 2, count, "both pre-migration and new domain alerts must survive")
			require.NoError(t, migration("000003_alert_account_scope.down.sql"))
			var nullable string
			require.NoError(t, conn.QueryRow("SELECT is_nullable FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'alerts' AND column_name = 'domain_id'", schema).Scan(&nullable))
			require.Equal(t, "NO", nullable)
			require.NoError(t, conn.QueryRow("SELECT count(*) FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'alerts' AND column_name = 'registrar_account_id'", schema).Scan(&count))
			require.Zero(t, count)
			require.NoError(t, migration("000003_alert_account_scope.up.sql"))
		})
	}
}
