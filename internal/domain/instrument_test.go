package domain_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/domain"
)

// emaar is EMAAR from GET https://api2.dfm.ae/cdn2/web/v1/get_security_overview?symbol=EMAAR
// and GET https://api2.dfm.ae/mw/v1/stocks on 14 Oct 2026.
func emaar() domain.Instrument {
	return domain.Instrument{
		Symbol:   "EMAAR",
		Exchange: domain.ExchangeDFM,
		ISIN:     "AEE000301011",
		NameEN:   "Emaar Properties PJSC",
		NameAR:   "إعمار العقارية ش.م.ع",
		Class:    domain.ClassEquity,
		Currency: domain.CurrencyAED,
		Sector:   "Real Estate",
		Board:    "510",
		Status:   domain.StatusListed,
		ListedAt: time.Date(2000, 3, 25, 20, 0, 0, 0, time.UTC),
	}
}

func TestNewInstrument(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(*domain.Instrument)
		wantErr string
	}{
		{name: "valid", edit: func(*domain.Instrument) {}},
		{name: "optional fields empty", edit: func(in *domain.Instrument) {
			in.ISIN, in.NameAR, in.Sector, in.Board, in.ListedAt = "", "", "", "", time.Time{}
		}},
		{name: "Nasdaq Dubai in USD", edit: func(in *domain.Instrument) {
			in.Exchange, in.Currency, in.Status = domain.ExchangeNasdaqDubai, domain.CurrencyUSD, domain.StatusSuspended
		}},
		{name: "ISIN US0378331005", edit: func(in *domain.Instrument) { in.ISIN = "US0378331005" }},
		{name: "zero value", edit: func(in *domain.Instrument) { *in = domain.Instrument{} },
			wantErr: `domain: invalid instrument: symbol "" is not a trading code`},
		{name: "bad symbol", edit: func(in *domain.Instrument) { in.Symbol = " EFF" },
			wantErr: `domain: invalid instrument: symbol " EFF" is not a trading code`},
		{name: "unknown exchange", edit: func(in *domain.Instrument) { in.Exchange = "Nasdaq Dubai" },
			wantErr: `domain: invalid instrument: unknown exchange "Nasdaq Dubai"`},
		{name: "ISIN check digit wrong", edit: func(in *domain.Instrument) { in.ISIN = "AEE000301012" },
			wantErr: `domain: invalid instrument: ISIN "AEE000301012" is not valid`},
		{name: "ISIN too short", edit: func(in *domain.Instrument) { in.ISIN = "AEE00030101" },
			wantErr: `domain: invalid instrument: ISIN "AEE00030101" is not valid`},
		{name: "ISIN lower case", edit: func(in *domain.Instrument) { in.ISIN = "aee000301011" },
			wantErr: `domain: invalid instrument: ISIN "aee000301011" is not valid`},
		{name: "ISIN digit country", edit: func(in *domain.Instrument) { in.ISIN = "1E0000301011" },
			wantErr: `domain: invalid instrument: ISIN "1E0000301011" is not valid`},
		{name: "missing English name", edit: func(in *domain.Instrument) { in.NameEN = "" },
			wantErr: `domain: invalid instrument: English name is missing`},
		{name: "blank English name", edit: func(in *domain.Instrument) { in.NameEN = "  " },
			wantErr: `domain: invalid instrument: English name is missing`},
		{name: "unknown class", edit: func(in *domain.Instrument) { in.Class = "Equities" },
			wantErr: `domain: invalid instrument: unknown class "Equities"`},
		{name: "unknown currency", edit: func(in *domain.Instrument) { in.Currency = "EUR" },
			wantErr: `domain: invalid instrument: unknown currency "EUR"`},
		{name: "unknown status", edit: func(in *domain.Instrument) { in.Status = "S" },
			wantErr: `domain: invalid instrument: unknown status "S"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := emaar()
			tt.edit(&in)
			got, err := domain.NewInstrument(in)
			checkErr(t, err, tt.wantErr)
			want := in
			if tt.wantErr != "" {
				want = domain.Instrument{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("NewInstrument() = %+v, want %+v", got, want)
			}
		})
	}
}
