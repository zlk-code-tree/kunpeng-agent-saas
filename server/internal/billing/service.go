package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"gin-dify-saas/server/internal/platform/ids"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrAccountNotFound     = errors.New("billing account not found")
	ErrInsufficientCredits = errors.New("insufficient credits")
	ErrInvalidAmount       = errors.New("invalid adjustment amount")
)

type Service struct{ db *pgxpool.Pool }

type Account struct {
	ID           string `json:"id"`
	SubjectType  string `json:"subjectType"`
	Currency     string `json:"currency"`
	Balance      string `json:"balance"`
	FrozenAmount string `json:"frozenAmount"`
	Available    string `json:"available"`
	Status       string `json:"status"`
}

type Plan struct {
	ID            string         `json:"id"`
	Code          string         `json:"code"`
	Name          string         `json:"name"`
	Audience      string         `json:"audience"`
	VersionID     string         `json:"versionId"`
	Version       int            `json:"version"`
	BillingPeriod string         `json:"billingPeriod"`
	Price         string         `json:"price"`
	Currency      string         `json:"currency"`
	Benefits      map[string]any `json:"benefits"`
}

type Entitlement struct {
	Key       string    `json:"key"`
	Limit     string    `json:"limit"`
	Used      string    `json:"used"`
	Reserved  string    `json:"reserved"`
	PeriodEnd time.Time `json:"periodEnd"`
}

type Subscription struct {
	ID                 string        `json:"id"`
	PlanCode           string        `json:"planCode"`
	PlanName           string        `json:"planName"`
	Status             string        `json:"status"`
	CurrentPeriodStart time.Time     `json:"currentPeriodStart"`
	CurrentPeriodEnd   time.Time     `json:"currentPeriodEnd"`
	CancelAtPeriodEnd  bool          `json:"cancelAtPeriodEnd"`
	Entitlements       []Entitlement `json:"entitlements"`
}

type LedgerEntry struct {
	ID           string         `json:"id"`
	EntryType    string         `json:"entryType"`
	Amount       string         `json:"amount"`
	BalanceAfter string         `json:"balanceAfter"`
	FrozenAfter  string         `json:"frozenAfter"`
	BusinessType string         `json:"businessType"`
	BusinessID   *string        `json:"businessId,omitempty"`
	Metadata     map[string]any `json:"metadata"`
	CreatedAt    time.Time      `json:"createdAt"`
}

type CostSummaryItem struct {
	CostType string `json:"costType"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type ReconciliationReport struct {
	SucceededRunsMissingUsage int64 `json:"succeededRunsMissingUsage"`
	ReservationsAwaitingUsage int64 `json:"reservationsAwaitingUsage"`
	ReservationsInReview      int64 `json:"reservationsInReview"`
	UsageMissingCost          int64 `json:"usageMissingCost"`
}

type Reservation struct {
	AccountID     string
	ReservationID string
	Reserved      string
}

type AdjustmentInput struct {
	Amount         string `json:"amount" binding:"required"`
	IdempotencyKey string `json:"idempotencyKey" binding:"required,min=8,max=128"`
	Reason         string `json:"reason" binding:"required,min=2,max=255"`
}

// NewService 创建计费领域服务。
func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// ProvisionAccount 在调用方事务中创建钱包、初始赠送流水、免费订阅和权益计数器。
// 这些记录必须原子落库，避免出现账户已经可见但没有期初流水的情况；钱包流水是余额变更的审计依据。
func ProvisionAccount(ctx context.Context, tx pgx.Tx, tenantID, subjectType, subjectID string) (string, error) {
	accountID, _ := ids.New()
	walletID, _ := ids.New()
	ledgerID, _ := ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO billing_accounts (id,tenant_id,subject_type,subject_id,status)
		VALUES ($1,$2,$3,$4,'active')
	`, accountID, tenantID, subjectType, subjectID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO wallet_accounts (id,billing_account_id,balance) VALUES ($1,$2,100)
	`, walletID, accountID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO wallet_ledger (
			id,tenant_id,billing_account_id,wallet_account_id,entry_type,amount,
			balance_after,frozen_after,business_type,idempotency_key,metadata
		) VALUES ($1,$2,$3,$4,'grant',100,100,0,'signup_trial','signup-trial',
			'{"source":"system","reason":"signup trial credit"}'::jsonb)
	`, ledgerID, tenantID, accountID, walletID); err != nil {
		return "", err
	}
	var planVersionID string
	var benefits []byte
	if err := tx.QueryRow(ctx, `
		SELECT pv.id::text,pv.benefits
		FROM plan_versions pv JOIN plans p ON p.id=pv.plan_id
		WHERE p.code='free' AND p.status='active' AND pv.status='active'
		ORDER BY pv.version DESC LIMIT 1
	`).Scan(&planVersionID, &benefits); errors.Is(err, pgx.ErrNoRows) {
		return accountID, nil
	} else if err != nil {
		return "", err
	}
	subscriptionID, _ := ids.New()
	periodStart := time.Now().UTC()
	periodEnd := periodStart.AddDate(0, 1, 0)
	if _, err := tx.Exec(ctx, `
		INSERT INTO subscriptions (
			id,tenant_id,billing_account_id,plan_version_id,status,current_period_start,current_period_end
		) VALUES ($1,$2,$3,$4,'active',$5,$6)
	`, subscriptionID, tenantID, accountID, planVersionID, periodStart, periodEnd); err != nil {
		return "", err
	}
	var benefitValues map[string]any
	if err := json.Unmarshal(benefits, &benefitValues); err != nil {
		return "", err
	}
	for key, value := range benefitValues {
		entitlementID, _ := ids.New()
		usageID, _ := ids.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO entitlements (
				id,tenant_id,subscription_id,billing_account_id,entitlement_key,limit_value,period_start,period_end
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		`, entitlementID, tenantID, subscriptionID, accountID, key, value, periodStart, periodEnd); err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO entitlement_usages (id,tenant_id,entitlement_id) VALUES ($1,$2,$3)`, usageID, tenantID, entitlementID); err != nil {
			return "", err
		}
	}
	return accountID, nil
}

// CurrentAccount 按当前租户类型解析并返回实际承担费用的计费账户。
func (s *Service) CurrentAccount(ctx context.Context, tenantID, userID string) (Account, error) {
	var item Account
	// 个人空间由用户账户付费，企业空间则共用租户账户。所有计费入口必须遵循同一规则，
	// 防止切换租户后仍错误扣减之前的个人钱包。
	err := s.db.QueryRow(ctx, `
		SELECT b.id::text,b.subject_type,w.currency,w.balance::text,w.frozen_amount::text,
		       (w.balance-w.frozen_amount)::text,b.status
		FROM tenants t
		JOIN billing_accounts b ON b.tenant_id=t.id AND b.status='active'
		  AND ((t.type='personal' AND b.subject_type='user' AND b.subject_id=$2)
		    OR (t.type='organization' AND b.subject_type='tenant' AND b.subject_id=t.id))
		JOIN wallet_accounts w ON w.billing_account_id=b.id
		WHERE t.id=$1 AND t.status='active'
	`, tenantID, userID).Scan(&item.ID, &item.SubjectType, &item.Currency, &item.Balance, &item.FrozenAmount, &item.Available, &item.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrAccountNotFound
	}
	return item, err
}

// ListPlans 返回适用于当前租户类型且当前生效的套餐版本。
func (s *Service) ListPlans(ctx context.Context, tenantID string) ([]Plan, error) {
	rows, err := s.db.Query(ctx, `
		SELECT p.id::text,p.code,p.name,p.audience,pv.id::text,pv.version,
		       pv.billing_period,pv.price::text,pv.currency,pv.benefits
		FROM plans p
		JOIN LATERAL (
			SELECT * FROM plan_versions candidate
			WHERE candidate.plan_id=p.id AND candidate.status='active'
			  AND candidate.effective_at<=NOW()
			  AND (candidate.expires_at IS NULL OR candidate.expires_at>NOW())
			ORDER BY candidate.version DESC LIMIT 1
		) pv ON true
		JOIN tenants t ON t.id=$1
		WHERE p.status='active' AND (p.audience='all' OR p.audience=t.type)
		ORDER BY pv.price,p.created_at
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Plan, 0)
	for rows.Next() {
		var item Plan
		var benefits []byte
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Audience, &item.VersionID, &item.Version, &item.BillingPeriod, &item.Price, &item.Currency, &benefits); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(benefits, &item.Benefits)
		items = append(items, item)
	}
	return items, rows.Err()
}

// CurrentSubscription 返回当前计费主体的有效订阅及本周期权益使用情况。
func (s *Service) CurrentSubscription(ctx context.Context, tenantID, userID string) (Subscription, error) {
	var item Subscription
	err := s.db.QueryRow(ctx, `
		SELECT s.id::text,p.code,p.name,s.status,s.current_period_start,s.current_period_end,s.cancel_at_period_end
		FROM tenants t
		JOIN billing_accounts b ON b.tenant_id=t.id AND b.status='active'
		  AND ((t.type='personal' AND b.subject_type='user' AND b.subject_id=$2)
		    OR (t.type='organization' AND b.subject_type='tenant' AND b.subject_id=t.id))
		JOIN subscriptions s ON s.billing_account_id=b.id AND s.status IN ('trialing','active','past_due')
		JOIN plan_versions pv ON pv.id=s.plan_version_id
		JOIN plans p ON p.id=pv.plan_id
		WHERE t.id=$1 AND t.status='active'
		ORDER BY s.created_at DESC LIMIT 1
	`, tenantID, userID).Scan(
		&item.ID, &item.PlanCode, &item.PlanName, &item.Status,
		&item.CurrentPeriodStart, &item.CurrentPeriodEnd, &item.CancelAtPeriodEnd,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, ErrAccountNotFound
	}
	if err != nil {
		return Subscription{}, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT e.entitlement_key,e.limit_value::text,u.used_value::text,u.reserved_value::text,e.period_end
		FROM entitlements e
		JOIN entitlement_usages u ON u.entitlement_id=e.id
		WHERE e.subscription_id=$1
		ORDER BY e.entitlement_key
	`, item.ID)
	if err != nil {
		return Subscription{}, err
	}
	defer rows.Close()
	item.Entitlements = make([]Entitlement, 0)
	for rows.Next() {
		var entitlement Entitlement
		if err := rows.Scan(&entitlement.Key, &entitlement.Limit, &entitlement.Used, &entitlement.Reserved, &entitlement.PeriodEnd); err != nil {
			return Subscription{}, err
		}
		item.Entitlements = append(item.Entitlements, entitlement)
	}
	return item, rows.Err()
}

// ListLedger 按时间倒序返回当前计费主体的钱包流水，最大返回 100 条。
func (s *Service) ListLedger(ctx context.Context, tenantID, userID string, limit int) ([]LedgerEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `
		SELECT l.id::text,l.entry_type,l.amount::text,l.balance_after::text,l.frozen_after::text,
		       l.business_type,l.business_id::text,l.metadata,l.created_at
		FROM tenants t
		JOIN billing_accounts b ON b.tenant_id=t.id AND b.status='active'
		  AND ((t.type='personal' AND b.subject_type='user' AND b.subject_id=$2)
		    OR (t.type='organization' AND b.subject_type='tenant' AND b.subject_id=t.id))
		JOIN wallet_ledger l ON l.billing_account_id=b.id
		WHERE t.id=$1 AND t.status='active'
		ORDER BY l.created_at DESC,l.id DESC LIMIT $3
	`, tenantID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]LedgerEntry, 0)
	for rows.Next() {
		var item LedgerEntry
		var metadata []byte
		if err := rows.Scan(&item.ID, &item.EntryType, &item.Amount, &item.BalanceAfter, &item.FrozenAfter, &item.BusinessType, &item.BusinessID, &metadata, &item.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(metadata, &item.Metadata)
		items = append(items, item)
	}
	return items, rows.Err()
}

// Reserve 为一次智能体运行冻结额度，并记录预占单和不可变钱包流水。
func (s *Service) Reserve(ctx context.Context, tx pgx.Tx, tenantID, userID, runID string) (Reservation, error) {
	var accountID, walletID string
	if err := tx.QueryRow(ctx, `
		SELECT b.id::text,w.id::text
		FROM tenants t
		JOIN billing_accounts b ON b.tenant_id=t.id AND b.status='active'
		  AND ((t.type='personal' AND b.subject_type='user' AND b.subject_id=$2)
		    OR (t.type='organization' AND b.subject_type='tenant' AND b.subject_id=t.id))
		JOIN wallet_accounts w ON w.billing_account_id=b.id
		WHERE t.id=$1 AND t.status='active'
	`, tenantID, userID).Scan(&accountID, &walletID); errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, ErrAccountNotFound
	} else if err != nil {
		return Reservation{}, err
	}
	// 将本次选中的价格版本固化到预占单。即使运行期间管理员发布了新价格，
	// 后续结算仍必须使用创建任务时看到的历史价格版本。
	var priceVersionID string
	var pricing []byte
	if err := tx.QueryRow(ctx, `
		SELECT id::text,pricing FROM price_versions
		WHERE code='default-ai' AND status='active' AND effective_at<=NOW()
		  AND (expires_at IS NULL OR expires_at>NOW())
		ORDER BY version DESC LIMIT 1
	`).Scan(&priceVersionID, &pricing); err != nil {
		return Reservation{}, fmt.Errorf("load active price: %w", err)
	}
	reserved, err := pricingValue(pricing, "reserve_per_run")
	if err != nil {
		return Reservation{}, err
	}
	reservedText := reserved.FloatString(8)
	var balanceAfter, frozenAfter string
	// 条件 UPDATE 同时完成可用余额校验和行锁定，消除“先查询、后更新”的并发窗口，
	// 因此多个并发任务不会冻结超过可用余额的额度。
	err = tx.QueryRow(ctx, `
		UPDATE wallet_accounts
		SET frozen_amount=frozen_amount+$2::numeric,version=version+1,updated_at=NOW()
		WHERE id=$1 AND balance-frozen_amount >= $2::numeric
		RETURNING balance::text,frozen_amount::text
	`, walletID, reservedText).Scan(&balanceAfter, &frozenAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, ErrInsufficientCredits
	}
	if err != nil {
		return Reservation{}, err
	}
	reservationID, _ := ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO billing_reservations (
			id,tenant_id,billing_account_id,run_id,price_version_id,reserved_amount,status,idempotency_key,expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,'reserved',$7,NOW()+INTERVAL '15 minutes')
	`, reservationID, tenantID, accountID, runID, priceVersionID, reservedText, "run:"+runID); err != nil {
		return Reservation{}, err
	}
	ledgerID, _ := ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO wallet_ledger (
			id,tenant_id,billing_account_id,wallet_account_id,entry_type,amount,
			balance_after,frozen_after,business_type,business_id,idempotency_key
		) VALUES ($1,$2,$3,$4,'reserve',$5,$6,$7,'ai_run',$8,$9)
	`, ledgerID, tenantID, accountID, walletID, reservedText, balanceAfter, frozenAfter, runID, "reserve:"+runID); err != nil {
		return Reservation{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_runs SET billing_account_id=$2 WHERE id=$1`, runID, accountID); err != nil {
		return Reservation{}, err
	}
	return Reservation{AccountID: accountID, ReservationID: reservationID, Reserved: reservedText}, nil
}

// Release 释放未结算的运行预占额度；重复释放按成功处理，保证调用幂等。
func (s *Service) Release(ctx context.Context, runID, reason string) error {
	// SERIALIZABLE 隔离级别配合行锁，使释放与结算互斥，避免同一笔预占被同时处理。
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var reservationID, tenantID, accountID, walletID, reserved string
	err = tx.QueryRow(ctx, `
		SELECT r.id::text,r.tenant_id::text,r.billing_account_id::text,w.id::text,r.reserved_amount::text
		FROM billing_reservations r
		JOIN wallet_accounts w ON w.billing_account_id=r.billing_account_id
		WHERE r.run_id=$1 AND r.status IN ('reserved','awaiting_usage')
		FOR UPDATE OF r,w
	`, runID).Scan(&reservationID, &tenantID, &accountID, &walletID, &reserved)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var balanceAfter, frozenAfter string
	if err := tx.QueryRow(ctx, `
		UPDATE wallet_accounts SET frozen_amount=frozen_amount-$2::numeric,version=version+1,updated_at=NOW()
		WHERE id=$1 AND frozen_amount >= $2::numeric
		RETURNING balance::text,frozen_amount::text
	`, walletID, reserved).Scan(&balanceAfter, &frozenAfter); err != nil {
		return err
	}
	ledgerID, _ := ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO wallet_ledger (
			id,tenant_id,billing_account_id,wallet_account_id,entry_type,amount,
			balance_after,frozen_after,business_type,business_id,idempotency_key,metadata
		) VALUES ($1,$2,$3,$4,'release',-$5::numeric,$6,$7,'ai_run',$8,$9,$10)
	`, ledgerID, tenantID, accountID, walletID, reserved, balanceAfter, frozenAfter, runID, "release:"+runID, map[string]any{"reason": reason}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE billing_reservations SET status='released',actual_amount=0,updated_at=NOW() WHERE id=$1`, reservationID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Settle 根据实际 Token 用量结算运行费用，并将预占转换为正式消费流水。
func (s *Service) Settle(ctx context.Context, runID string, usage map[string]any) error {
	inputTokens, outputTokens, totalTokens := UsageTokenCounts(usage)
	if totalTokens == 0 {
		// 缺失用量不代表本次请求免费。保留冻结额度并进入待对账状态，
		// 避免静默释放额度导致收入和成本记录丢失。
		_, err := s.db.Exec(ctx, `
			UPDATE billing_reservations SET status='awaiting_usage',updated_at=NOW()
			WHERE run_id=$1 AND status='reserved'
		`, runID)
		return err
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var reservationID, tenantID, accountID, walletID, reserved, priceVersionID string
	var pricing []byte
	// 同时锁定预占单和钱包，避免结算、取消释放、人工调账读写到中间状态。
	err = tx.QueryRow(ctx, `
		SELECT r.id::text,r.tenant_id::text,r.billing_account_id::text,w.id::text,
		       r.reserved_amount::text,p.id::text,p.pricing
		FROM billing_reservations r
		JOIN wallet_accounts w ON w.billing_account_id=r.billing_account_id
		JOIN price_versions p ON p.id=r.price_version_id
		WHERE r.run_id=$1 AND r.status IN ('reserved','awaiting_usage')
		FOR UPDATE OF r,w
	`, runID).Scan(&reservationID, &tenantID, &accountID, &walletID, &reserved, &priceVersionID, &pricing)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	actual, err := calculateCost(pricing, inputTokens, outputTokens)
	if err != nil {
		return err
	}
	actualText := actual.FloatString(8)
	var balanceAfter, frozenAfter string
	err = tx.QueryRow(ctx, `
		UPDATE wallet_accounts
		SET balance=balance-$2::numeric,frozen_amount=frozen_amount-$3::numeric,
		    version=version+1,updated_at=NOW()
		WHERE id=$1 AND frozen_amount >= $3::numeric
		  AND balance-$2::numeric >= frozen_amount-$3::numeric
		RETURNING balance::text,frozen_amount::text
	`, walletID, actualText, reserved).Scan(&balanceAfter, &frozenAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		// 此处通常表示实际费用已不满足钱包约束，例如实际费用超过可用余额。
		// 保留预占并转为人工复核，不能强制扣出负余额。
		_, _ = tx.Exec(ctx, `UPDATE billing_reservations SET status='review',actual_amount=$2,updated_at=NOW() WHERE id=$1`, reservationID, actualText)
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	var usageRecordID *string
	_ = tx.QueryRow(ctx, `SELECT id::text FROM usage_records WHERE run_id=$1 ORDER BY created_at DESC LIMIT 1`, runID).Scan(&usageRecordID)
	settlementID, _ := ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO billing_settlements (
			id,tenant_id,billing_account_id,reservation_id,usage_record_id,price_version_id,
			amount,status,pricing_snapshot
		) VALUES ($1,$2,$3,$4,$5,$6,$7,'settled',$8)
	`, settlementID, tenantID, accountID, reservationID, usageRecordID, priceVersionID, actualText, pricing); err != nil {
		return err
	}
	ledgerID, _ := ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO wallet_ledger (
			id,tenant_id,billing_account_id,wallet_account_id,entry_type,amount,
			balance_after,frozen_after,business_type,business_id,idempotency_key,metadata
		) VALUES ($1,$2,$3,$4,'consume',-$5::numeric,$6,$7,'ai_run',$8,$9,$10)
	`, ledgerID, tenantID, accountID, walletID, actualText, balanceAfter, frozenAfter, runID, "settle:"+runID, map[string]any{"inputTokens": inputTokens, "outputTokens": outputTokens, "totalTokens": totalTokens}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE billing_reservations SET status='settled',actual_amount=$2,updated_at=NOW() WHERE id=$1`, reservationID, actualText); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Adjust 执行平台管理员人工调账，并以幂等键记录操作原因和操作者。
func (s *Service) Adjust(ctx context.Context, tenantID, accountID, actorUserID string, input AdjustmentInput) (Account, error) {
	amount, ok := new(big.Rat).SetString(input.Amount)
	if !ok || amount.Sign() == 0 {
		return Account{}, ErrInvalidAmount
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback(ctx)
	var walletID, subjectType, currency, status, balanceAfter, frozenAfter, available string
	// 负向调账只能扣减未冻结额度。运行中的任务仍需要使用已预占额度完成结算，
	// 因此人工调账后余额不得低于冻结金额。
	err = tx.QueryRow(ctx, `
		UPDATE wallet_accounts w
		SET balance=w.balance+$3::numeric,version=w.version+1,updated_at=NOW()
		FROM billing_accounts b
		WHERE w.billing_account_id=b.id AND b.id=$1 AND b.tenant_id=$2
		  AND b.status='active' AND w.balance+$3::numeric >= w.frozen_amount
		RETURNING w.id::text,b.subject_type,w.currency,w.balance::text,w.frozen_amount::text,
		          (w.balance-w.frozen_amount)::text,b.status
	`, accountID, tenantID, amount.FloatString(8)).Scan(&walletID, &subjectType, &currency, &balanceAfter, &frozenAfter, &available, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrInvalidAmount
	}
	if err != nil {
		return Account{}, err
	}
	ledgerID, _ := ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO wallet_ledger (
			id,tenant_id,billing_account_id,wallet_account_id,entry_type,amount,
			balance_after,frozen_after,business_type,idempotency_key,metadata
		) VALUES ($1,$2,$3,$4,'adjust',$5,$6,$7,'manual_adjustment',$8,$9)
	`, ledgerID, tenantID, accountID, walletID, amount.FloatString(8), balanceAfter, frozenAfter, input.IdempotencyKey, map[string]any{"reason": input.Reason, "actorUserId": actorUserID}); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Account{}, err
	}
	return Account{ID: accountID, SubjectType: subjectType, Currency: currency, Balance: balanceAfter, FrozenAmount: frozenAfter, Available: available, Status: status}, nil
}

// RecordProviderCost 按模型价格版本记录本次调用产生的上游供应商成本。
func (s *Service) RecordProviderCost(ctx context.Context, tenantID, runID, usageRecordID string, usage map[string]any) error {
	// 供应商成本与客户 Credit 消费分账记录。即使产品额度与上游货币使用不同单位或
	// 定价方式，也能据此计算真实毛利。
	provider := firstString(usage, "provider", "model_provider")
	model := firstString(usage, "model", "model_name")
	if provider == "" || model == "" {
		return nil
	}
	inputTokens, outputTokens, _ := UsageTokenCounts(usage)
	var priceVersionID, inputRateText, outputRateText, currency string
	// 同一供应商编码和模型下，租户自有配置优先于平台共享配置。
	// 固化价格版本和原始用量快照，确保价格调整后仍可重算历史报表。
	err := s.db.QueryRow(ctx, `
		SELECT mpv.id::text,mpv.input_per_1000::text,mpv.output_per_1000::text,mpv.currency
		FROM model_providers p
		JOIN model_price_versions mpv ON mpv.provider_id=p.id AND mpv.model=$3
		WHERE p.code=$2 AND p.status='active' AND (p.tenant_id=$1 OR p.is_shared=TRUE)
		  AND mpv.status='active' AND mpv.effective_at<=NOW()
		  AND (mpv.expires_at IS NULL OR mpv.expires_at>NOW())
		ORDER BY (p.tenant_id IS NOT NULL) DESC,mpv.version DESC LIMIT 1
	`, tenantID, provider, model).Scan(&priceVersionID, &inputRateText, &outputRateText, &currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	inputRate, _ := new(big.Rat).SetString(inputRateText)
	outputRate, _ := new(big.Rat).SetString(outputRateText)
	if inputRate == nil || outputRate == nil {
		return fmt.Errorf("invalid model price")
	}
	inputAmount := new(big.Rat).Mul(inputRate, big.NewRat(inputTokens, 1000))
	outputAmount := new(big.Rat).Mul(outputRate, big.NewRat(outputTokens, 1000))
	snapshot, _ := json.Marshal(usage)
	entries := []struct {
		costType string
		quantity int64
		rate     *big.Rat
		amount   *big.Rat
	}{
		{costType: "model_input", quantity: inputTokens, rate: inputRate, amount: inputAmount},
		{costType: "model_output", quantity: outputTokens, rate: outputRate, amount: outputAmount},
	}
	for _, entry := range entries {
		id, _ := ids.New()
		if _, err := s.db.Exec(ctx, `
			INSERT INTO cost_entries (
				id,tenant_id,run_id,usage_record_id,cost_type,provider,model,quantity,unit_price,amount,currency,price_version_id,source_snapshot
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			ON CONFLICT (usage_record_id,cost_type) DO NOTHING
		`, id, tenantID, runID, usageRecordID, entry.costType, provider, model, entry.quantity, entry.rate.FloatString(10), entry.amount.FloatString(10), currency, priceVersionID, snapshot); err != nil {
			return err
		}
	}
	return nil
}

// CostSummary 按成本类型、供应商和模型汇总指定时间区间的上游成本。
func (s *Service) CostSummary(ctx context.Context, tenantID string, from, to time.Time) ([]CostSummaryItem, error) {
	rows, err := s.db.Query(ctx, `
		SELECT cost_type,COALESCE(provider,''),COALESCE(model,''),SUM(amount)::text,currency
		FROM cost_entries WHERE tenant_id=$1 AND created_at>=$2 AND created_at<$3
		GROUP BY cost_type,provider,model,currency ORDER BY cost_type,provider,model
	`, tenantID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CostSummaryItem, 0)
	for rows.Next() {
		var item CostSummaryItem
		if err := rows.Scan(&item.CostType, &item.Provider, &item.Model, &item.Amount, &item.Currency); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Reconcile 扫描运行、用量、预占和成本之间的不一致，并持久化对账报告。
func (s *Service) Reconcile(ctx context.Context, tenantID string) (ReconciliationReport, error) {
	// 对账只识别异常并生成报告，不猜测缺失用量，也不自动移动资金，
	// 避免错误数据触发不可逆的账务调整。
	var report ReconciliationReport
	err := s.db.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE r.status='succeeded' AND u.id IS NULL),
		  COUNT(*) FILTER (WHERE br.status='awaiting_usage'),
		  COUNT(*) FILTER (WHERE br.status='review'),
		  COUNT(*) FILTER (WHERE u.id IS NOT NULL AND ce.id IS NULL AND COALESCE(u.model,'')<>'')
		FROM ai_runs r
		LEFT JOIN usage_records u ON u.run_id=r.id
		LEFT JOIN billing_reservations br ON br.run_id=r.id
		LEFT JOIN cost_entries ce ON ce.usage_record_id=u.id
		WHERE r.tenant_id=$1
	`, tenantID).Scan(&report.SucceededRunsMissingUsage, &report.ReservationsAwaitingUsage, &report.ReservationsInReview, &report.UsageMissingCost)
	if err != nil {
		return ReconciliationReport{}, err
	}
	id, _ := ids.New()
	reportJSON, _ := json.Marshal(report)
	_, err = s.db.Exec(ctx, `
		INSERT INTO reconciliation_runs (id,scope,tenant_id,status,report,started_at,finished_at)
		VALUES ($1,'billing',$2,'succeeded',$3,NOW(),NOW())
	`, id, tenantID, reportJSON)
	return report, err
}

// UsageTokenCounts 兼容 Dify 和不同模型供应商的字段名，统一提取输入、输出和总 Token 数。
func UsageTokenCounts(usage map[string]any) (input, output, total int64) {
	// 结算和供应商成本必须共用同一套字段归一规则，避免两套口径产生差异。
	input = firstInt(usage, "prompt_tokens", "input_tokens")
	output = firstInt(usage, "completion_tokens", "output_tokens")
	total = firstInt(usage, "total_tokens")
	if total == 0 {
		total = input + output
	}
	return
}

// firstInt 按字段优先级读取第一个可转换为整数的值。
func firstInt(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch value := values[key].(type) {
		case float64:
			return int64(value)
		case int:
			return int64(value)
		case int64:
			return value
		case json.Number:
			parsed, _ := value.Int64()
			return parsed
		case string:
			parsed, _ := strconv.ParseInt(value, 10, 64)
			return parsed
		}
	}
	return 0
}

// firstString 按字段优先级读取第一个非空字符串。
func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// calculateCost 根据价格快照计算费用，并应用单次调用最低消费。
func calculateCost(pricing []byte, inputTokens, outputTokens int64) (*big.Rat, error) {
	// 使用有理数计算避免二进制浮点误差，只有在最终持久化金额时才执行十进制格式化。
	inputRate, err := pricingValue(pricing, "input_per_1000")
	if err != nil {
		return nil, err
	}
	outputRate, err := pricingValue(pricing, "output_per_1000")
	if err != nil {
		return nil, err
	}
	minimum, err := pricingValue(pricing, "minimum")
	if err != nil {
		return nil, err
	}
	cost := new(big.Rat).Mul(inputRate, big.NewRat(inputTokens, 1000))
	cost.Add(cost, new(big.Rat).Mul(outputRate, big.NewRat(outputTokens, 1000)))
	if cost.Cmp(minimum) < 0 {
		cost.Set(minimum)
	}
	return cost, nil
}

// pricingValue 从 JSON 价格快照读取并校验一个非负十进制价格项。
func pricingValue(pricing []byte, key string) (*big.Rat, error) {
	decoder := json.NewDecoder(bytes.NewReader(pricing))
	decoder.UseNumber()
	var values map[string]any
	if err := decoder.Decode(&values); err != nil {
		return nil, err
	}
	value, ok := values[key]
	if !ok {
		return nil, fmt.Errorf("pricing key %s is missing", key)
	}
	text := fmt.Sprint(value)
	result, ok := new(big.Rat).SetString(text)
	if !ok || result.Sign() < 0 {
		return nil, fmt.Errorf("pricing key %s is invalid", key)
	}
	return result, nil
}
