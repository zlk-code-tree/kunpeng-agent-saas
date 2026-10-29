package billing

import "time"

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
