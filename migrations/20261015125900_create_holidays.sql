-- +goose Up
-- Days DFM is closed besides its weekly days off, which come from the
-- configured trading week.
CREATE TABLE holidays (
    day    date PRIMARY KEY,
    reason text NOT NULL CHECK (reason <> ''),
    kind   text NOT NULL CHECK (kind IN ('scheduled', 'unscheduled'))
);

-- Scheduled holidays are from DFM's securities-market holiday circulars
-- DClear/2024/13 (2025) and DClear/2025/12 (2026), linked from
-- https://www.dfm.ae/the-exchange/news-disclosures/settlement-trading-calendar.
-- Islamic holidays there are tentative. Where DFM later moved one, the day
-- below is the one DFM actually closed, read from the gaps in its DFM Index
-- history (api2.dfm.ae SearchDFMIndex, 2025-01-02 to 2026-10-15):
-- Hijri New Year 2025 26 Jun -> 27 Jun, Prophet's Birthday 2025 4 Sep -> 5 Sep,
-- UAE National Day 2025 2-3 Dec -> 1-2 Dec, Hijri New Year 2026 16 Jun -> 15 Jun,
-- Prophet's Birthday 2026 25 Aug -> 28 Aug. The same history shows the
-- unscheduled closure of 2 and 3 March 2026.
INSERT INTO holidays (day, reason, kind) VALUES
    ('2025-01-01', 'New Year''s Day', 'scheduled'),
    ('2025-03-30', 'Eid Al Fitr', 'scheduled'),
    ('2025-03-31', 'Eid Al Fitr', 'scheduled'),
    ('2025-04-01', 'Eid Al Fitr', 'scheduled'),
    ('2025-06-05', 'Arafah (Haj) Day', 'scheduled'),
    ('2025-06-06', 'Eid Al Adha', 'scheduled'),
    ('2025-06-07', 'Eid Al Adha', 'scheduled'),
    ('2025-06-27', 'Hijri New Year''s Day', 'scheduled'),
    ('2025-09-05', 'Prophet Muhammad''s Birthday', 'scheduled'),
    ('2025-12-01', 'UAE National Day', 'scheduled'),
    ('2025-12-02', 'UAE National Day', 'scheduled'),
    ('2026-01-01', 'New Year''s Day', 'scheduled'),
    ('2026-03-02', 'Unscheduled closure', 'unscheduled'),
    ('2026-03-03', 'Unscheduled closure', 'unscheduled'),
    ('2026-03-19', 'Eid Al Fitr', 'scheduled'),
    ('2026-03-20', 'Eid Al Fitr', 'scheduled'),
    ('2026-03-21', 'Eid Al Fitr', 'scheduled'),
    ('2026-05-26', 'Arafah (Haj) Day', 'scheduled'),
    ('2026-05-27', 'Eid Al Adha', 'scheduled'),
    ('2026-05-28', 'Eid Al Adha', 'scheduled'),
    ('2026-05-29', 'Eid Al Adha', 'scheduled'),
    ('2026-06-15', 'Hijri New Year''s Day', 'scheduled'),
    ('2026-08-28', 'Prophet Muhammad''s Birthday', 'scheduled'),
    ('2026-12-02', 'UAE National Day', 'scheduled'),
    ('2026-12-03', 'UAE National Day', 'scheduled');

-- +goose Down
DROP TABLE holidays;
