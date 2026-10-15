package bus

import (
	"time"

	"github.com/shopspring/decimal"

	"github.com/saudm6/dfm-be/internal/domain"
)

// The topics. Each carries one event type.
var (
	QuoteUpdated        = &Topic[QuoteUpdatedEvent]{name: "quote.updated"}
	DisclosurePublished = &Topic[DisclosurePublishedEvent]{name: "disclosure.published"}
	OrderChanged        = &Topic[OrderChangedEvent]{name: "order.changed"}
	PositionChanged     = &Topic[PositionChangedEvent]{name: "position.changed"}
	AlertTriggered      = &Topic[AlertTriggeredEvent]{name: "alert.triggered"}
	SourceHealthChanged = &Topic[SourceHealthChangedEvent]{name: "source.health.changed"}
)

// QuoteUpdatedEvent carries the latest quote of one instrument.
type QuoteUpdatedEvent struct {
	Quote domain.Quote
}

// DisclosurePublishedEvent announces a new company disclosure.
type DisclosurePublishedEvent struct {
	ID          string
	Symbol      domain.Symbol
	Title       string
	PublishedAt time.Time
}

// OrderChangedEvent announces a change in an order's state.
type OrderChangedEvent struct {
	OrderID string
	Symbol  domain.Symbol
	Status  string
	At      time.Time
}

// PositionChangedEvent announces a new quantity held in one instrument.
type PositionChangedEvent struct {
	Account  string
	Symbol   domain.Symbol
	Quantity decimal.Decimal
	At       time.Time
}

// AlertTriggeredEvent announces that an alert's condition was met.
type AlertTriggeredEvent struct {
	AlertID string
	Symbol  domain.Symbol
	Message string
	At      time.Time
}

// SourceHealthChangedEvent announces a change in a data source's health.
type SourceHealthChangedEvent struct {
	Source    domain.Source
	Healthy   bool
	Freshness domain.Freshness
	Detail    string
	At        time.Time
}
