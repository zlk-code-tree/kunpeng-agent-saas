package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"gin-dify-saas/server/internal/config"
	"gin-dify-saas/server/internal/platform/database"
	"gin-dify-saas/server/internal/platform/ids"

	"github.com/jackc/pgx/v5"
)

type report struct {
	Applied              bool   `json:"applied"`
	DefaultTenantID      string `json:"defaultTenantId"`
	FromMode             string `json:"fromMode"`
	MissingOwnersFixed   int64  `json:"missingOwnersFixed"`
	RunsBackfilled       int64  `json:"runsBackfilled"`
	TenantBillingCreated bool   `json:"tenantBillingCreated"`
	ToMode               string `json:"toMode"`
}

func main() {
	toMode := flag.String("to", "multi", "target mode; only multi is supported")
	apply := flag.Bool("apply", false, "apply the migration; omission performs precheck only")
	flag.Parse()
	if *toMode != "multi" {
		slog.Error("unsupported target mode", "to", *toMode)
		os.Exit(2)
	}
	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database startup failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		fatal(err)
	}
	defer tx.Rollback(ctx)
	var installationID, fromMode, defaultTenantID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text, mode, default_tenant_id::text FROM installation_settings LIMIT 1 FOR UPDATE
	`).Scan(&installationID, &fromMode, &defaultTenantID); err != nil {
		fatal(err)
	}
	result := report{FromMode: fromMode, ToMode: *toMode, DefaultTenantID: defaultTenantID, Applied: *apply}
	if fromMode == "multi" {
		result.Applied = false
		printJSON(result)
		return
	}
	if fromMode != "single" {
		fatal(fmt.Errorf("unsupported source mode %s", fromMode))
	}

	var ownerCount int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM tenant_members WHERE tenant_id=$1 AND role='owner' AND status='active'`, defaultTenantID).Scan(&ownerCount); err != nil {
		fatal(err)
	}
	if ownerCount == 0 {
		command, err := tx.Exec(ctx, `
			UPDATE tenant_members SET role='owner', updated_at=NOW()
			WHERE id=(SELECT id FROM tenant_members WHERE tenant_id=$1 AND status='active' ORDER BY created_at LIMIT 1)
		`, defaultTenantID)
		if err != nil || command.RowsAffected() == 0 {
			fatal(fmt.Errorf("default tenant has no active member to promote"))
		}
		result.MissingOwnersFixed = command.RowsAffected()
	}

	var billingAccountID string
	err = tx.QueryRow(ctx, `SELECT id::text FROM billing_accounts WHERE subject_type='tenant' AND subject_id=$1`, defaultTenantID).Scan(&billingAccountID)
	if err == pgx.ErrNoRows {
		billingAccountID, _ = ids.New()
		walletID, _ := ids.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO billing_accounts (id,tenant_id,subject_type,subject_id,status)
			VALUES ($1,$2,'tenant',$2,'active')
		`, billingAccountID, defaultTenantID); err != nil {
			fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO wallet_accounts (id,billing_account_id) VALUES ($1,$2)`, walletID, billingAccountID); err != nil {
			fatal(err)
		}
		result.TenantBillingCreated = true
	} else if err != nil {
		fatal(err)
	}
	command, err := tx.Exec(ctx, `UPDATE ai_runs SET billing_account_id=$1 WHERE tenant_id=$2 AND billing_account_id IS NULL`, billingAccountID, defaultTenantID)
	if err != nil {
		fatal(err)
	}
	result.RunsBackfilled = command.RowsAffected()

	if !*apply {
		printJSON(result)
		return
	}
	migrationID, _ := ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant_mode_migrations (id,installation_id,from_mode,to_mode,status,report,started_at)
		VALUES ($1,$2,'single','multi','running',$3,NOW())
	`, migrationID, installationID, result); err != nil {
		fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE installation_settings SET mode='multi', updated_at=NOW() WHERE id=$1`, installationID); err != nil {
		fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE tenant_mode_migrations SET status='succeeded', report=$2, finished_at=NOW() WHERE id=$1`, migrationID, result); err != nil {
		fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		fatal(err)
	}
	printJSON(result)
}

func fatal(err error) {
	slog.Error("mode migration failed", "error", err)
	os.Exit(1)
}

func printJSON(value any) {
	encoded, _ := json.MarshalIndent(value, "", "  ")
	fmt.Println(string(encoded))
}
