package dfm_test

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/dfm"
	"github.com/saudm6/dfm-be/internal/dfm/dfmtest"
	"github.com/saudm6/dfm-be/internal/domain"
)

// recordedAt is when the fixtures were recorded: 12:00 on 2026-10-15 in Dubai.
var recordedAt = time.Date(2026, time.October, 15, 8, 0, 0, 0, time.UTC)

func newClient(srv *dfmtest.Server) *dfm.Client {
	return &dfm.Client{BaseURL: srv.URL, OverviewInterval: time.Millisecond, RetryDelay: time.Millisecond}
}

// wantInstruments is the security master of the recorded responses.
var wantInstruments = []domain.Instrument{
	{Symbol: "EIB", Exchange: domain.ExchangeDFM,
		NameEN: "Emirates Islamic Bank PJSC", NameAR: "مصرف الإمارات الإسلامي ش.م.ع",
		Class: domain.ClassEquity, Currency: "AED", Sector: "Financials", Board: "510", Status: domain.StatusSuspended},
	{Symbol: "EMAAR", Exchange: domain.ExchangeDFM, ISIN: "AEE000301011",
		NameEN: "Emaar Properties PJSC", NameAR: "إعمار العقارية ش.م.ع",
		Class: domain.ClassEquity, Currency: "AED", Sector: "Real Estate", Board: "510", Status: domain.StatusListed,
		ListedAt: time.Date(2000, 3, 25, 20, 0, 0, 0, time.UTC)},
	{Symbol: "NBD", Exchange: domain.ExchangeDFM,
		NameEN: "National Bank of Dubai", NameAR: "بنك  دبي الوطني",
		Class: domain.ClassEquity, Currency: "AED", Status: domain.StatusDelisted},
	{Symbol: "DEPA", Exchange: domain.ExchangeNasdaqDubai,
		NameEN: "Depa Limited", NameAR: "ديبا",
		Class: domain.ClassEquity, Currency: "AED", Sector: "Nasdaq Dubai", Board: "210", Status: domain.StatusListed},
	{Symbol: "OC", Exchange: domain.ExchangeNasdaqDubai,
		NameEN: "Orascom Construction PLC", NameAR: "Orascom Construction PLC",
		Class: domain.ClassEquity, Currency: "USD", Sector: "Nasdaq Dubai", Board: "200", Status: domain.StatusSuspended},
	{Symbol: "BHMCAPITAL", Exchange: domain.ExchangeDFM, ISIN: "AEA006001019",
		NameEN: "BHM Capital Financial Services PSC", NameAR: "بي اتش ام كابيتال للخدمات المالية ش.م.خ",
		Class: domain.ClassEquity, Currency: "AED", Sector: "Private Joint Stock Companies (PrJSC)", Board: "520", Status: domain.StatusListed,
		ListedAt: time.Date(2015, 11, 26, 6, 0, 0, 0, time.UTC)},
	{Symbol: "ENBDREIT", Exchange: domain.ExchangeNasdaqDubai,
		NameEN: "ENBD REIT (CEIC) PLC", NameAR: "ENBD REIT (CEIC) PLC",
		Class: domain.ClassEquity, Currency: "USD", Sector: "Nasdaq Dubai", Board: "200", Status: domain.StatusListed},
	{Symbol: "DPW", Exchange: domain.ExchangeNasdaqDubai,
		NameEN: "DP World PLC", NameAR: "موانئ دبي العالمية",
		Class: domain.ClassEquity, Currency: "AED", Sector: "Nasdaq Dubai", Status: domain.StatusDelisted},
	{Symbol: "SHUAA-2007", Exchange: domain.ExchangeDFM,
		NameEN: "SHUAA Bonds 2007 (Matured)", NameAR: "سندات شعاع كابيتال 2007 (تم استحقاقه)",
		Class: domain.ClassBond, Currency: "AED", Sector: "Debt Instrument", Status: domain.StatusMatured},
	{Symbol: "DPWDU0926EUR", Exchange: domain.ExchangeNasdaqDubai,
		NameEN: "DP World Ltd 2.375% 25 Sep 2026 - 144A", NameAR: "DP World Ltd 2.375% 25 Sep 2026 - 144A",
		Class: domain.ClassBond, Currency: "EUR", Board: "202", Status: domain.StatusSuspended},
	{Symbol: "DUBAIFXN0143", Exchange: domain.ExchangeDFM,
		NameEN: "Government of Dubai - 5.25% - 2043", NameAR: "حكومة دبي - 5.25% - 2043",
		Class: domain.ClassBond, Currency: "AED", Sector: "Debt Instrument", Status: domain.StatusDelisted},
	{Symbol: "BCHNA0926CNY", Exchange: domain.ExchangeNasdaqDubai,
		NameEN: "Bank of China Ltd LUX 2.980% 18Sep2026", NameAR: "Bank of China Ltd LUX 2.980% 18Sep2026",
		Class: domain.ClassBond, Currency: "CNY", Sector: "Debt Instrument", Board: "202", Status: domain.StatusSuspended},
	{Symbol: "EBIUH1129USD", Exchange: domain.ExchangeNasdaqDubai,
		NameEN: "EBIUH1129USD",
		Class:  domain.ClassBond, Currency: "USD", Sector: "Debt Instrument", Board: "202", Status: domain.StatusListed},
	{Symbol: "AMLAKSK2010", Exchange: domain.ExchangeDFM,
		NameEN: "AMLAK SUKUK  (Matured)", NameAR: "صكوك املاك (تم استحقاقة)",
		Class: domain.ClassSukuk, Currency: "AED", Sector: "Debt Instrument", Status: domain.StatusMatured},
	{Symbol: "AUBDUPRP2USD", Exchange: domain.ExchangeNasdaqDubai,
		NameEN: "Ahli United Sukuk Ltd 3.875%  PRP", NameAR: "Ahli United Sukuk Ltd 3.875% PRP",
		Class: domain.ClassSukuk, Currency: "USD", Board: "202", Status: domain.StatusMatured},
	{Symbol: "SIB0931USD", Exchange: domain.ExchangeNasdaqDubai,
		NameEN: "SIB Sukuk Co. III Ltd 5.850% 29 Sep 2031", NameAR: "SIB Sukuk Company III Limited",
		Class: domain.ClassSukuk, Currency: "USD", Sector: "Debt Instrument", Board: "202", Status: domain.StatusListed},
	{Symbol: "AMCUAE", Exchange: domain.ExchangeDFM,
		NameEN: "AL Mal UAE Equity Fund", NameAR: "صندوق المال",
		Class: domain.ClassFund, Currency: "AED", Status: domain.StatusDelisted},
	{Symbol: "EFF", Exchange: domain.ExchangeDFM,
		NameEN: "Emirates Flagship", NameAR: "صندوق الإمارات الرائد",
		Class: domain.ClassFund, Currency: "AED", Status: domain.StatusDelisted},
	{Symbol: "ABTC", Exchange: domain.ExchangeNasdaqDubai,
		NameEN: "21Shares Bitcoin ETP", NameAR: "21شايرز بيتكوين إي تي بي",
		Class: domain.ClassFund, Currency: "USD", Board: "200", Status: domain.StatusListed},
	{Symbol: "UAETF", Exchange: domain.ExchangeDFM,
		NameEN: "AFKAR S&P UAE UCITS ETF", NameAR: "صندوق أفكار لمؤشر ستاندرد اند بورز الامارات",
		Class: domain.ClassETF, Currency: "AED", Sector: "Exchange Traded Funds", Status: domain.StatusDelisted},
	{Symbol: "AMCREIT", Exchange: domain.ExchangeDFM, ISIN: "AEA003630067",
		NameEN: "Al Mal Capital REIT", NameAR: "المال كابيتال ريت",
		Class: domain.ClassREIT, Currency: "AED", Sector: "Real Estate Investment Trust", Board: "510", Status: domain.StatusListed,
		ListedAt: time.Date(2021, 1, 18, 4, 39, 0, 0, time.UTC)},
	{Symbol: "AMCREIT-RI", Exchange: domain.ExchangeDFM,
		NameEN: "Al Mal Capital REIT – Rights Issue", NameAR: "المال كابيتال ريت – حقوق الاكتتاب",
		Class: domain.ClassREIT, Currency: "AED", Sector: "Real Estate Investment Trust", Status: domain.StatusDelisted},
	{Symbol: "DFMGI", Exchange: domain.ExchangeDFM,
		NameEN: "DFM Index",
		Class:  domain.ClassIndex, Currency: "AED", Status: domain.StatusListed},
	{Symbol: "DFMFI", Exchange: domain.ExchangeDFM,
		NameEN: "Financials", NameAR: "القطاع المالي",
		Class: domain.ClassIndex, Currency: "AED", Sector: "Financials", Status: domain.StatusListed},
	{Symbol: "DFMII", Exchange: domain.ExchangeDFM,
		NameEN: "Industrials", NameAR: "قطاع الصناعة",
		Class: domain.ClassIndex, Currency: "AED", Sector: "Industrials", Status: domain.StatusListed},
	{Symbol: "DFMREI", Exchange: domain.ExchangeDFM,
		NameEN: "Real Estate", NameAR: "قطاع العقارات",
		Class: domain.ClassIndex, Currency: "AED", Sector: "Real Estate", Status: domain.StatusListed},
	{Symbol: "DFMUI", Exchange: domain.ExchangeDFM,
		NameEN: "Utilities", NameAR: "قطاع المرافق العامة",
		Class: domain.ClassIndex, Currency: "AED", Sector: "Utilities", Status: domain.StatusListed},
	{Symbol: "DFMCMI", Exchange: domain.ExchangeDFM,
		NameEN: "Communication Services", NameAR: "قطاع الاتصالات",
		Class: domain.ClassIndex, Currency: "AED", Sector: "Communication Services", Status: domain.StatusListed},
	{Symbol: "DFMMI", Exchange: domain.ExchangeDFM,
		NameEN: "Materials", NameAR: "قطاع المواد الأساسية",
		Class: domain.ClassIndex, Currency: "AED", Sector: "Materials", Status: domain.StatusListed},
	{Symbol: "DFMCSI", Exchange: domain.ExchangeDFM,
		NameEN: "Consumer Staples", NameAR: "قطاع السلع الاستهلاكية",
		Class: domain.ClassIndex, Currency: "AED", Sector: "Consumer Staples", Status: domain.StatusListed},
	{Symbol: "DFMCDI", Exchange: domain.ExchangeDFM,
		NameEN: "Consumer Discretionary", NameAR: "قطاع الخدمات الاستهلاكية",
		Class: domain.ClassIndex, Currency: "AED", Sector: "Consumer Discretionary", Status: domain.StatusListed},
}

func TestSecurityMasterParsesRecordedResponses(t *testing.T) {
	srv := dfmtest.NewServer(t, dfmtest.SecurityMasterDir())

	m, err := newClient(srv).SecurityMaster(t.Context(), recordedAt)
	if err != nil {
		t.Fatalf("SecurityMaster() error = %v", err)
	}

	if len(m.Instruments) != len(wantInstruments) {
		t.Errorf("got %d instruments, want %d", len(m.Instruments), len(wantInstruments))
	}
	for i := range min(len(m.Instruments), len(wantInstruments)) {
		if got, want := m.Instruments[i], wantInstruments[i]; !reflect.DeepEqual(got, want) {
			t.Errorf("instrument %d:\n got %+v\nwant %+v", i, got, want)
		}
	}
	wantBoards := []dfm.Board{
		{Code: "200", Exchanges: []domain.Exchange{"NASDAQ_DUBAI"}, Classes: []domain.InstrumentClass{"equity", "fund"}, Lines: 3},
		{Code: "202", Exchanges: []domain.Exchange{"NASDAQ_DUBAI"}, Classes: []domain.InstrumentClass{"bond", "sukuk"}, Lines: 6},
		{Code: "203", Exchanges: []domain.Exchange{}, Classes: []domain.InstrumentClass{}, Lines: 1},
		{Code: "210", Exchanges: []domain.Exchange{"NASDAQ_DUBAI"}, Classes: []domain.InstrumentClass{"equity"}, Lines: 1},
		{Code: "510", Exchanges: []domain.Exchange{"DFM"}, Classes: []domain.InstrumentClass{"equity", "reit"}, Lines: 3},
		{Code: "520", Exchanges: []domain.Exchange{"DFM"}, Classes: []domain.InstrumentClass{"equity"}, Lines: 1},
		{Code: "602", Exchanges: []domain.Exchange{}, Classes: []domain.InstrumentClass{"future"}, Lines: 1},
		{Code: "605", Exchanges: []domain.Exchange{}, Classes: []domain.InstrumentClass{"future"}, Lines: 1},
	}
	if !reflect.DeepEqual(m.Boards, wantBoards) {
		t.Errorf("Boards =\n%+v\nwant\n%+v", m.Boards, wantBoards)
	}
	if want := []dfm.Skip{{Symbol: "DPW", Reason: "duplicate of an earlier line"}}; !reflect.DeepEqual(m.Skipped, want) {
		t.Errorf("Skipped = %+v, want %+v", m.Skipped, want)
	}
}

func TestSecurityMasterRequests(t *testing.T) {
	srv := dfmtest.NewServer(t, dfmtest.SecurityMasterDir())

	if _, err := newClient(srv).SecurityMaster(t.Context(), recordedAt); err != nil {
		t.Fatalf("SecurityMaster() error = %v", err)
	}

	var got []string
	for _, r := range srv.Requests() {
		if r.UserAgent != "dfm-be/1.0 (+https://github.com/saudm6/dfm-be)" {
			t.Errorf("%s %s User-Agent = %q, want the identifying one", r.Method, r.Path, r.UserAgent)
		}
		switch {
		case r.Method == http.MethodPost:
			got = append(got, "POST "+r.Path+" "+r.Form.Encode())
		case len(r.Query) > 0:
			got = append(got, "GET "+r.Path+"?"+r.Query.Encode())
		default:
			got = append(got, "GET "+r.Path)
		}
	}
	overview := func(s string) string { return "GET /cdn2/web/v1/get_security_overview?symbol=" + s }
	want := []string{
		"POST /web/widgets/v1/data Command=LiteSecuritiesLists&Language=en",
		"POST /web/widgets/v1/data Command=LiteSecuritiesLists&Language=ar",
		"GET /mw/v1/stocks",
		"GET /web/sf/v1/sectors?sf_culture=en",
		"GET /web/sf/v1/sectors?sf_culture=ar",
		overview("EIB"), overview("EMAAR"), overview("NBD"), overview("DEPA"), overview("OC"), overview("BHMCAPITAL"),
		overview("ENBDREIT"), overview("DPW"), overview("SHUAA-2007"), overview("DPWDU0926EUR"), overview("DUBAIFXN0143"),
		overview("BCHNA0926CNY"), overview("EBIUH1129USD"), overview("AMLAKSK2010"), overview("AUBDUPRP2USD"),
		overview("SIB0931USD"), overview("AMCUAE"), overview("EFF"), overview("ABTC"), overview("UAETF"),
		overview("AMCREIT"), overview("AMCREIT-RI"),
	}
	if !slices.Equal(got, want) {
		t.Errorf("requests =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestSecurityMasterThrottlesOverviews(t *testing.T) {
	srv := dfmtest.NewServer(t, dfmtest.SecurityMasterDir())
	const every = 40 * time.Millisecond
	c := &dfm.Client{BaseURL: srv.URL, OverviewInterval: every}

	if _, err := c.SecurityMaster(t.Context(), recordedAt); err != nil {
		t.Fatalf("SecurityMaster() error = %v", err)
	}

	var at []time.Time
	for _, r := range srv.Requests() {
		if r.Path == "/cdn2/web/v1/get_security_overview" {
			at = append(at, r.At)
		}
	}
	if len(at) != 22 {
		t.Fatalf("got %d overview calls, want 22", len(at))
	}
	// 21 gaps of 40ms; one gap of slack absorbs network and timer jitter
	// without letting unthrottled calls, a few ms in all, pass.
	if span, least := at[len(at)-1].Sub(at[0]), 20*every; span < least {
		t.Errorf("22 overview calls took %s, want at least %s at one per %s", span, least, every)
	}
}

func TestDefaultOverviewIntervalIsTwoPerSecond(t *testing.T) {
	if dfm.DefaultOverviewInterval != 500*time.Millisecond {
		t.Errorf("DefaultOverviewInterval = %s, want 500ms", dfm.DefaultOverviewInterval)
	}
}

func TestSecurityMasterStatusAsOf(t *testing.T) {
	// The market watch gives DPWDU0926EUR a maturity date of 2026-10-23.
	tests := []struct {
		name string
		now  time.Time
		want domain.InstrumentStatus
	}{
		{"on its maturity day", time.Date(2026, time.October, 23, 6, 0, 0, 0, time.UTC), domain.StatusSuspended},
		{"the day after in Dubai", time.Date(2026, time.October, 23, 20, 0, 0, 0, time.UTC), domain.StatusMatured},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := dfmtest.NewServer(t, dfmtest.SecurityMasterDir())
			m, err := newClient(srv).SecurityMaster(t.Context(), tt.now)
			if err != nil {
				t.Fatalf("SecurityMaster() error = %v", err)
			}
			if got := find(t, m, "DPWDU0926EUR").Status; got != tt.want {
				t.Errorf("DPWDU0926EUR status = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSecurityMasterRetriesServerErrors(t *testing.T) {
	srv := dfmtest.NewServer(t, dfmtest.SecurityMasterDir())
	srv.FailNext("/mw/v1/stocks", http.StatusServiceUnavailable, http.StatusBadGateway)
	srv.FailNext("/cdn2/web/v1/get_security_overview", http.StatusInternalServerError)

	m, err := newClient(srv).SecurityMaster(t.Context(), recordedAt)
	if err != nil {
		t.Fatalf("SecurityMaster() error = %v", err)
	}
	if got := find(t, m, "EIB").Board; got != "510" {
		t.Errorf("EIB board = %q, want 510", got)
	}
}

func TestSecurityMasterErrors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*testing.T, *dfmtest.Server)
		wantErr string
	}{
		{
			name: "market watch keeps failing",
			setup: func(_ *testing.T, s *dfmtest.Server) {
				s.FailNext("/mw/v1/stocks", http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable)
			},
			wantErr: "dfm: /mw/v1/stocks: unexpected status 503",
		},
		{
			name: "overview not found",
			setup: func(_ *testing.T, s *dfmtest.Server) {
				s.FailNext("/cdn2/web/v1/get_security_overview", http.StatusNotFound)
			},
			wantErr: "dfm: /cdn2/web/v1/get_security_overview: unexpected status 404",
		},
		{
			name: "securities list is not JSON",
			setup: func(t *testing.T, s *dfmtest.Server) {
				s.Replace("lite-securities-lists-ar.json", writeFile(t, "<companies/>"))
			},
			wantErr: "dfm: decode LiteSecuritiesLists ar: invalid character '<' looking for beginning of value",
		},
		{
			name: "securities list without equities",
			setup: func(t *testing.T, s *dfmtest.Server) {
				s.Replace("lite-securities-lists-en.json", writeFile(t, `{"Bonds":[]}`))
			},
			wantErr: "dfm: LiteSecuritiesLists en has no equities",
		},
		{
			name: "sectors list without the DFM Index",
			setup: func(t *testing.T, s *dfmtest.Server) {
				s.Replace("sectors-en.json", writeFile(t, `{"value":[{"SectorID":"11"}]}`))
			},
			wantErr: "dfm: sectors list has no sector 9999",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := dfmtest.NewServer(t, dfmtest.SecurityMasterDir())
			tt.setup(t, srv)

			_, err := newClient(srv).SecurityMaster(t.Context(), recordedAt)
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("SecurityMaster() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestSecurityMasterSkipsUnusableLines(t *testing.T) {
	srv := dfmtest.NewServer(t, dfmtest.SecurityMasterDir())
	srv.Replace("lite-securities-lists-en.json", writeFile(t, `{
		"Equities":[{"ID":"1","SecuritySymbol":"EMAAR","FullName":"Emaar Properties PJSC","Exchange":"DFM","Sector":"Real Estate","SecurityType":"Equity"},
		            {"ID":"2","SecuritySymbol":"BAD/CODE","FullName":"Bad","Exchange":"DFM","Sector":"","SecurityType":"Equity"}],
		"Bonds":[{"ID":"3","SecuritySymbol":"DPWDU0926EUR","FullName":"DP World","Exchange":"ADX","Sector":"","SecurityType":"Equity"},
		         {"ID":"4","SecuritySymbol":"EIB0130JPY","FullName":"Yen bond","Exchange":"Nasdaq Dubai","Sector":"","SecurityType":"Equity"}],
		"Warrants":[{"ID":"5","SecuritySymbol":"EMAARW","FullName":"Emaar warrant","Exchange":"DFM","Sector":"","SecurityType":"Equity"}]}`))
	srv.Replace("overview/EIB0130JPY.json", filepath.Join(dfmtest.SecurityMasterDir(), "overview", "EIB.json"))

	m, err := newClient(srv).SecurityMaster(t.Context(), recordedAt)
	if err != nil {
		t.Fatalf("SecurityMaster() error = %v", err)
	}
	want := []dfm.Skip{
		{Symbol: "BAD/CODE", Reason: `domain: invalid symbol: "BAD/CODE" is not a trading code`},
		{Symbol: "DPWDU0926EUR", Reason: `unknown exchange "ADX"`},
		{Symbol: "EIB0130JPY", Reason: `currency suffix "JPY" is not a known currency`},
		{Symbol: "EMAARW", Reason: `unknown securities group "Warrants"`},
	}
	if !reflect.DeepEqual(m.Skipped, want) {
		t.Errorf("Skipped =\n%+v\nwant\n%+v", m.Skipped, want)
	}
	var got []domain.Symbol
	for _, in := range m.Instruments {
		got = append(got, in.Symbol)
	}
	if want := []domain.Symbol{"EMAAR", "DFMGI", "DFMFI", "DFMII", "DFMREI", "DFMUI", "DFMCMI", "DFMMI", "DFMCSI", "DFMCDI"}; !slices.Equal(got, want) {
		t.Errorf("instruments = %q, want %q", got, want)
	}
}

func find(t *testing.T, m dfm.SecurityMaster, sym domain.Symbol) domain.Instrument {
	t.Helper()
	for _, in := range m.Instruments {
		if in.Symbol == sym {
			return in
		}
	}
	t.Fatalf("no instrument %s", sym)
	return domain.Instrument{}
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
