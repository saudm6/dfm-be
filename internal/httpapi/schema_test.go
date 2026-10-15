package httpapi_test

import (
	"encoding/json"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/shopspring/decimal"

	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/httpapi/gen"
)

func TestSchemaWireFormat(t *testing.T) {
	listed := openapi_types.Date{Time: time.Date(2000, time.March, 26, 0, 0, 0, 0, time.UTC)}
	isin := "AEE000301011"
	tests := []struct {
		name string
		v    any
		want string
	}{
		{
			name: "instrument",
			v: gen.Instrument{
				Symbol:   "EMAAR",
				Exchange: domain.ExchangeDFM,
				Isin:     &isin,
				NameEn:   "Emaar Properties",
				Class:    domain.ClassEquity,
				Currency: domain.CurrencyAED,
				Status:   domain.StatusListed,
				ListedAt: &listed,
			},
			want: `{"class":"equity","currency":"AED","exchange":"DFM","isin":"AEE000301011","listedAt":"2000-03-26","nameEn":"Emaar Properties","status":"listed","symbol":"EMAAR"}`,
		},
		{
			name: "quote before first trade",
			v: gen.Quote{
				Symbol:    "NBD GULF",
				Last:      decimal.Zero,
				Bid:       decimal.RequireFromString("11.35"),
				Ask:       decimal.RequireFromString("11.40"),
				BidSize:   1000,
				AskSize:   2500,
				Value:     decimal.Zero,
				PrevClose: decimal.RequireFromString("11.30"),
				AsOf:      time.Date(2026, time.October, 15, 10, 0, 1, 0, domain.DubaiTZ),
				Source:    domain.SourceDFMMarketWatch,
				Freshness: domain.Delayed,
			},
			want: `{"asOf":"2026-10-15T10:00:01+04:00","ask":"11.4","askSize":2500,"bid":"11.35","bidSize":1000,"freshness":"delayed","last":"0","prevClose":"11.3","source":"dfm-mw","symbol":"NBD GULF","trades":0,"value":"0","volume":0}`,
		},
		{
			name: "day bar",
			v: gen.Bar{
				Symbol:    "EMAAR",
				Interval:  domain.OneDay,
				Start:     domain.TradingDayStart(2026, time.October, 14),
				Open:      decimal.RequireFromString("11.20"),
				High:      decimal.RequireFromString("11.45"),
				Low:       decimal.RequireFromString("11.15"),
				Close:     decimal.RequireFromString("11.40"),
				Volume:    5_000_000,
				Value:     decimal.RequireFromString("56500000.50"),
				Source:    domain.SourceDFMHistory,
				Freshness: domain.EndOfDay,
			},
			want: `{"close":"11.4","freshness":"eod","high":"11.45","interval":"1d","low":"11.15","open":"11.2","source":"dfm-history","start":"2026-10-14T00:00:00+04:00","symbol":"EMAAR","value":"56500000.5","volume":5000000}`,
		},
		{
			name: "error",
			v:    gen.Error{Code: "not_found", Message: "instrument EMAAR not found"},
			want: `{"code":"not_found","message":"instrument EMAAR not found"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.v)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("json = %s\nwant   %s", got, tt.want)
			}
		})
	}
}

func TestSchemaDecodeRejectsUnknownEnum(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr string
	}{
		{
			name:    "unknown freshness",
			json:    `{"freshness":"stale"}`,
			wantErr: `domain: invalid freshness: unknown value "stale"`,
		},
		{
			name:    "empty interval",
			json:    `{"interval":""}`,
			wantErr: `domain: invalid interval: unknown value ""`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b gen.Bar
			err := json.Unmarshal([]byte(tt.json), &b)
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("unmarshal error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
