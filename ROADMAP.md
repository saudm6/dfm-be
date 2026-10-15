# Build order

This file is the build order for the DFM trader terminal across dfm-be and dfm-fe. It lists all 96 PRs, 54 of them in dfm-be. The same order is in [dfm-fe/ROADMAP.md](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).

## Use this file

- Work from step 1 down. A step can start once every step it waits on has merged.
- Rows in dfm-be have a checkbox. Tick it in the PR that closes the issue.
- Rows in dfm-fe have no checkbox here. Their checkbox is in [dfm-fe/ROADMAP.md](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- Steps in the same wave do not depend on each other. A second person can take any step whose wave has started.
- Each issue repeats its step number and links back here.
- A step can sit under another feature's heading when an earlier feature needs it. Its issue still belongs to its own epic.

Start the spikes early. They need account setup or browser work, and their answers shape later steps.

- Step 21, [dfm-be#12](https://github.com/saudm6/dfm-be/issues/12) Spike: choose the IBKR API and prove DFM quotes on a paper account. It can start at wave 2.
- Step 37, [dfm-be#24](https://github.com/saudm6/dfm-be/issues/24) Spike: find the request behind DFM's 2-year price download. It can start at wave 2.

## When each feature works end to end

| Feature | Works after step | Epic |
|---|---|---|
| F00 Foundation | 15 | [dfm-be#55](https://github.com/saudm6/dfm-be/issues/55) |
| F01 Security master, search, and command line | 20 | [dfm-be#57](https://github.com/saudm6/dfm-be/issues/57) |
| F02 Live quotes and watchlists | 29 | [dfm-be#58](https://github.com/saudm6/dfm-be/issues/58) |
| F03 Movers and heat map | 34 | [dfm-be#59](https://github.com/saudm6/dfm-be/issues/59) |
| H Market history and price charts | 43 | [dfm-be#56](https://github.com/saudm6/dfm-be/issues/56) |
| F07 Technical studies | 47 | [dfm-be#63](https://github.com/saudm6/dfm-be/issues/63) |
| F04 News and disclosures | 56 | [dfm-be#60](https://github.com/saudm6/dfm-be/issues/60) |
| F10 Order entry | 63 | [dfm-be#66](https://github.com/saudm6/dfm-be/issues/66) |
| F11 Positions and live P&L | 68 | [dfm-be#67](https://github.com/saudm6/dfm-be/issues/67) |
| F06 Launchpad workspace | 72 | [dfm-be#62](https://github.com/saudm6/dfm-be/issues/62) |
| F08 Correlation and pair spreads | 77 | [dfm-be#64](https://github.com/saudm6/dfm-be/issues/64) |
| F05 Alerts | 80 | [dfm-be#61](https://github.com/saudm6/dfm-be/issues/61) |
| F09 Custom spreads and baskets | 82 | [dfm-be#65](https://github.com/saudm6/dfm-be/issues/65) |
| F13 Portfolio analytics | 89 | [dfm-be#69](https://github.com/saudm6/dfm-be/issues/69) |
| F12 Chat | 96 | [dfm-be#68](https://github.com/saudm6/dfm-be/issues/68) |

Price and percent-change alerts work after step 53. F05 finishes later because its last PR, [dfm-be#38](https://github.com/saudm6/dfm-be/issues/38), adds technical, disclosure, and custom series conditions after F09.

## Steps

Each heading names the feature its steps finish. Shared steps sit under the first feature that needs them.

### F00 Foundation

- [x] **1.** [dfm-be#1](https://github.com/saudm6/dfm-be/issues/1) Scaffold the Go service. Wave 1. Waits on nothing.
- [x] **2.** [dfm-be#2](https://github.com/saudm6/dfm-be/issues/2) Add Postgres, TimescaleDB, and migrations. Wave 2. Waits on step 1 ([dfm-be#1](https://github.com/saudm6/dfm-be/issues/1)).
- [x] **3.** [dfm-be#3](https://github.com/saudm6/dfm-be/issues/3) Define the core market domain types. Wave 2. Waits on step 1 ([dfm-be#1](https://github.com/saudm6/dfm-be/issues/1)).
- [ ] **4.** [dfm-be#4](https://github.com/saudm6/dfm-be/issues/4) Publish the OpenAPI contract and generate the server. Wave 3. Waits on step 1 ([dfm-be#1](https://github.com/saudm6/dfm-be/issues/1)), step 3 ([dfm-be#3](https://github.com/saudm6/dfm-be/issues/3)).
- [ ] **5.** [dfm-be#5](https://github.com/saudm6/dfm-be/issues/5) Add local accounts, sessions, and roles. Wave 4. Waits on step 2 ([dfm-be#2](https://github.com/saudm6/dfm-be/issues/2)), step 4 ([dfm-be#4](https://github.com/saudm6/dfm-be/issues/4)).
- [x] **6.** [dfm-be#6](https://github.com/saudm6/dfm-be/issues/6) Add the in-process event bus. Wave 3. Waits on step 3 ([dfm-be#3](https://github.com/saudm6/dfm-be/issues/3)).
- [ ] **7.** [dfm-be#7](https://github.com/saudm6/dfm-be/issues/7) Add the WebSocket gateway and topic protocol. Wave 5. Waits on step 5 ([dfm-be#5](https://github.com/saudm6/dfm-be/issues/5)), step 6 ([dfm-be#6](https://github.com/saudm6/dfm-be/issues/6)).
- [ ] **8.** [dfm-be#8](https://github.com/saudm6/dfm-be/issues/8) Add the DFM trading calendar. Wave 3. Waits on step 2 ([dfm-be#2](https://github.com/saudm6/dfm-be/issues/2)), step 3 ([dfm-be#3](https://github.com/saudm6/dfm-be/issues/3)).
- [ ] **9.** [dfm-be#9](https://github.com/saudm6/dfm-be/issues/9) Report source health and data freshness. Wave 6. Waits on step 6 ([dfm-be#6](https://github.com/saudm6/dfm-be/issues/6)), step 7 ([dfm-be#7](https://github.com/saudm6/dfm-be/issues/7)).
- **10.** [dfm-fe#1](https://github.com/saudm6/dfm-fe/issues/1) Scaffold the Angular app. Wave 1. Waits on nothing. Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **11.** [dfm-fe#2](https://github.com/saudm6/dfm-fe/issues/2) Build the theme, tokens, and number formatting. Wave 2. Waits on step 10 ([dfm-fe#1](https://github.com/saudm6/dfm-fe/issues/1)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **12.** [dfm-fe#3](https://github.com/saudm6/dfm-fe/issues/3) Generate the typed API client. Wave 4. Waits on step 4 ([dfm-be#4](https://github.com/saudm6/dfm-be/issues/4)), step 10 ([dfm-fe#1](https://github.com/saudm6/dfm-fe/issues/1)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **13.** [dfm-fe#4](https://github.com/saudm6/dfm-fe/issues/4) Add the WebSocket client service. Wave 6. Waits on step 7 ([dfm-be#7](https://github.com/saudm6/dfm-be/issues/7)), step 10 ([dfm-fe#1](https://github.com/saudm6/dfm-fe/issues/1)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **14.** [dfm-fe#5](https://github.com/saudm6/dfm-fe/issues/5) Add login and the auth guard. Wave 5. Waits on step 5 ([dfm-be#5](https://github.com/saudm6/dfm-be/issues/5)), step 12 ([dfm-fe#3](https://github.com/saudm6/dfm-fe/issues/3)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **15.** [dfm-fe#6](https://github.com/saudm6/dfm-fe/issues/6) Build the terminal shell with panels and a status bar. Wave 7. Waits on step 8 ([dfm-be#8](https://github.com/saudm6/dfm-be/issues/8)), step 9 ([dfm-be#9](https://github.com/saudm6/dfm-be/issues/9)), step 11 ([dfm-fe#2](https://github.com/saudm6/dfm-fe/issues/2)), step 13 ([dfm-fe#4](https://github.com/saudm6/dfm-fe/issues/4)), step 14 ([dfm-fe#5](https://github.com/saudm6/dfm-fe/issues/5)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).

### F01 Security master, search, and command line

- [ ] **16.** [dfm-be#10](https://github.com/saudm6/dfm-be/issues/10) Ingest the DFM security master. Wave 4. Waits on step 2 ([dfm-be#2](https://github.com/saudm6/dfm-be/issues/2)), step 3 ([dfm-be#3](https://github.com/saudm6/dfm-be/issues/3)), step 8 ([dfm-be#8](https://github.com/saudm6/dfm-be/issues/8)).
- [ ] **17.** [dfm-be#11](https://github.com/saudm6/dfm-be/issues/11) Add instrument search and description endpoints. Wave 5. Waits on step 4 ([dfm-be#4](https://github.com/saudm6/dfm-be/issues/4)), step 16 ([dfm-be#10](https://github.com/saudm6/dfm-be/issues/10)).
- **18.** [dfm-fe#7](https://github.com/saudm6/dfm-fe/issues/7) Build the command line and function registry. Wave 8. Waits on step 15 ([dfm-fe#6](https://github.com/saudm6/dfm-fe/issues/6)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **19.** [dfm-fe#8](https://github.com/saudm6/dfm-fe/issues/8) Autocomplete instruments and functions. Wave 9. Waits on step 17 ([dfm-be#11](https://github.com/saudm6/dfm-be/issues/11)), step 18 ([dfm-fe#7](https://github.com/saudm6/dfm-fe/issues/7)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **20.** [dfm-fe#9](https://github.com/saudm6/dfm-fe/issues/9) Build the DES security description panel. Wave 9. Waits on step 17 ([dfm-be#11](https://github.com/saudm6/dfm-be/issues/11)), step 18 ([dfm-fe#7](https://github.com/saudm6/dfm-fe/issues/7)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).

### F02 Live quotes and watchlists

- [ ] **21.** [dfm-be#12](https://github.com/saudm6/dfm-be/issues/12) Spike: choose the IBKR API and prove DFM quotes on a paper account. Wave 2. Waits on step 1 ([dfm-be#1](https://github.com/saudm6/dfm-be/issues/1)).
- [ ] **22.** [dfm-be#13](https://github.com/saudm6/dfm-be/issues/13) Manage the IBKR session lifecycle. Wave 7. Waits on step 9 ([dfm-be#9](https://github.com/saudm6/dfm-be/issues/9)), step 21 ([dfm-be#12](https://github.com/saudm6/dfm-be/issues/12)).
- [ ] **23.** [dfm-be#14](https://github.com/saudm6/dfm-be/issues/14) Map DFM symbols to IBKR contracts. Wave 8. Waits on step 16 ([dfm-be#10](https://github.com/saudm6/dfm-be/issues/10)), step 22 ([dfm-be#13](https://github.com/saudm6/dfm-be/issues/13)).
- [ ] **24.** [dfm-be#15](https://github.com/saudm6/dfm-be/issues/15) Stream IBKR level 1 quotes within the line budget. Wave 9. Waits on step 6 ([dfm-be#6](https://github.com/saudm6/dfm-be/issues/6)), step 23 ([dfm-be#14](https://github.com/saudm6/dfm-be/issues/14)).
- [ ] **25.** [dfm-be#16](https://github.com/saudm6/dfm-be/issues/16) Poll the DFM market-watch snapshot. Wave 5. Waits on step 6 ([dfm-be#6](https://github.com/saudm6/dfm-be/issues/6)), step 8 ([dfm-be#8](https://github.com/saudm6/dfm-be/issues/8)), step 16 ([dfm-be#10](https://github.com/saudm6/dfm-be/issues/10)).
- [ ] **26.** [dfm-be#17](https://github.com/saudm6/dfm-be/issues/17) Merge quote sources into one best quote per instrument. Wave 10. Waits on step 7 ([dfm-be#7](https://github.com/saudm6/dfm-be/issues/7)), step 24 ([dfm-be#15](https://github.com/saudm6/dfm-be/issues/15)), step 25 ([dfm-be#16](https://github.com/saudm6/dfm-be/issues/16)).
- [ ] **27.** [dfm-be#18](https://github.com/saudm6/dfm-be/issues/18) Add the watchlists API. Wave 11. Waits on step 5 ([dfm-be#5](https://github.com/saudm6/dfm-be/issues/5)), step 16 ([dfm-be#10](https://github.com/saudm6/dfm-be/issues/10)), step 26 ([dfm-be#17](https://github.com/saudm6/dfm-be/issues/17)).
- **28.** [dfm-fe#10](https://github.com/saudm6/dfm-fe/issues/10) Build the streaming watchlist monitor (W). Wave 12. Waits on step 18 ([dfm-fe#7](https://github.com/saudm6/dfm-fe/issues/7)), step 27 ([dfm-be#18](https://github.com/saudm6/dfm-be/issues/18)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **29.** [dfm-fe#11](https://github.com/saudm6/dfm-fe/issues/11) Build the quote panel (Q). Wave 11. Waits on step 18 ([dfm-fe#7](https://github.com/saudm6/dfm-fe/issues/7)), step 26 ([dfm-be#17](https://github.com/saudm6/dfm-be/issues/17)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).

### F03 Movers and heat map

- [ ] **30.** [dfm-be#19](https://github.com/saudm6/dfm-be/issues/19) Add movers, most-active, and breadth endpoints. Wave 11. Waits on step 25 ([dfm-be#16](https://github.com/saudm6/dfm-be/issues/16)), step 26 ([dfm-be#17](https://github.com/saudm6/dfm-be/issues/17)).
- [ ] **31.** [dfm-be#20](https://github.com/saudm6/dfm-be/issues/20) Aggregate the sector heat map. Wave 12. Waits on step 30 ([dfm-be#19](https://github.com/saudm6/dfm-be/issues/19)).
- **32.** [dfm-fe#12](https://github.com/saudm6/dfm-fe/issues/12) Build the movers panel (MOST). Wave 12. Waits on step 18 ([dfm-fe#7](https://github.com/saudm6/dfm-fe/issues/7)), step 30 ([dfm-be#19](https://github.com/saudm6/dfm-be/issues/19)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **33.** [dfm-fe#13](https://github.com/saudm6/dfm-fe/issues/13) Build the sector heat map (IMAP). Wave 13. Waits on step 18 ([dfm-fe#7](https://github.com/saudm6/dfm-fe/issues/7)), step 31 ([dfm-be#20](https://github.com/saudm6/dfm-be/issues/20)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **34.** [dfm-fe#14](https://github.com/saudm6/dfm-fe/issues/14) Build the market summary bar. Wave 12. Waits on step 15 ([dfm-fe#6](https://github.com/saudm6/dfm-fe/issues/6)), step 30 ([dfm-be#19](https://github.com/saudm6/dfm-be/issues/19)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).

### H Market history and price charts

- [ ] **35.** [dfm-be#21](https://github.com/saudm6/dfm-be/issues/21) Ingest DFM disclosures from EFSAH. Part of F04. Wave 5. Waits on step 6 ([dfm-be#6](https://github.com/saudm6/dfm-be/issues/6)), step 8 ([dfm-be#8](https://github.com/saudm6/dfm-be/issues/8)), step 16 ([dfm-be#10](https://github.com/saudm6/dfm-be/issues/10)).
- [ ] **36.** [dfm-be#23](https://github.com/saudm6/dfm-be/issues/23) Ingest market announcements and corporate actions. Part of F04. Wave 6. Waits on step 35 ([dfm-be#21](https://github.com/saudm6/dfm-be/issues/21)).
- [x] **37.** [dfm-be#24](https://github.com/saudm6/dfm-be/issues/24) Spike: find the request behind DFM's 2-year price download. Wave 2. Waits on step 1 ([dfm-be#1](https://github.com/saudm6/dfm-be/issues/1)).
- [ ] **38.** [dfm-be#25](https://github.com/saudm6/dfm-be/issues/25) Backfill two years of daily bars. Wave 5. Waits on step 2 ([dfm-be#2](https://github.com/saudm6/dfm-be/issues/2)), step 16 ([dfm-be#10](https://github.com/saudm6/dfm-be/issues/10)), step 37 ([dfm-be#24](https://github.com/saudm6/dfm-be/issues/24)).
- [ ] **39.** [dfm-be#26](https://github.com/saudm6/dfm-be/issues/26) Capture the daily bar at each close. Wave 6. Waits on step 8 ([dfm-be#8](https://github.com/saudm6/dfm-be/issues/8)), step 25 ([dfm-be#16](https://github.com/saudm6/dfm-be/issues/16)), step 38 ([dfm-be#25](https://github.com/saudm6/dfm-be/issues/25)).
- [ ] **40.** [dfm-be#27](https://github.com/saudm6/dfm-be/issues/27) Adjust daily bars for corporate actions. Wave 7. Waits on step 36 ([dfm-be#23](https://github.com/saudm6/dfm-be/issues/23)), step 38 ([dfm-be#25](https://github.com/saudm6/dfm-be/issues/25)).
- [ ] **41.** [dfm-be#28](https://github.com/saudm6/dfm-be/issues/28) Record one-minute bars from live quotes. Wave 11. Waits on step 26 ([dfm-be#17](https://github.com/saudm6/dfm-be/issues/17)), step 38 ([dfm-be#25](https://github.com/saudm6/dfm-be/issues/25)).
- [ ] **42.** [dfm-be#29](https://github.com/saudm6/dfm-be/issues/29) Add the bars API. Wave 6. Waits on step 4 ([dfm-be#4](https://github.com/saudm6/dfm-be/issues/4)), step 38 ([dfm-be#25](https://github.com/saudm6/dfm-be/issues/25)).
- **43.** [dfm-fe#17](https://github.com/saudm6/dfm-fe/issues/17) Build the price chart component (GP). Wave 9. Waits on step 18 ([dfm-fe#7](https://github.com/saudm6/dfm-fe/issues/7)), step 42 ([dfm-be#29](https://github.com/saudm6/dfm-be/issues/29)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).

### F07 Technical studies

- [x] **44.** [dfm-be#30](https://github.com/saudm6/dfm-be/issues/30) Build the technical studies package. Wave 3. Waits on step 3 ([dfm-be#3](https://github.com/saudm6/dfm-be/issues/3)).
- [ ] **45.** [dfm-be#31](https://github.com/saudm6/dfm-be/issues/31) Serve technical studies over bars. Wave 7. Waits on step 42 ([dfm-be#29](https://github.com/saudm6/dfm-be/issues/29)), step 44 ([dfm-be#30](https://github.com/saudm6/dfm-be/issues/30)).
- [ ] **46.** [dfm-be#40](https://github.com/saudm6/dfm-be/issues/40) Store per-user layouts and chart templates. Part of F06. Wave 5. Waits on step 5 ([dfm-be#5](https://github.com/saudm6/dfm-be/issues/5)).
- **47.** [dfm-fe#18](https://github.com/saudm6/dfm-fe/issues/18) Add study overlays and panes to the chart. Wave 10. Waits on step 43 ([dfm-fe#17](https://github.com/saudm6/dfm-fe/issues/17)), step 45 ([dfm-be#31](https://github.com/saudm6/dfm-be/issues/31)), step 46 ([dfm-be#40](https://github.com/saudm6/dfm-be/issues/40)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).

### F05 Alerts

- [ ] **48.** [dfm-be#36](https://github.com/saudm6/dfm-be/issues/36) Build the alert engine for price conditions. Wave 11. Waits on step 2 ([dfm-be#2](https://github.com/saudm6/dfm-be/issues/2)), step 8 ([dfm-be#8](https://github.com/saudm6/dfm-be/issues/8)), step 26 ([dfm-be#17](https://github.com/saudm6/dfm-be/issues/17)).
- [ ] **49.** [dfm-be#37](https://github.com/saudm6/dfm-be/issues/37) Add the alerts API and triggered-alert topic. Wave 12. Waits on step 5 ([dfm-be#5](https://github.com/saudm6/dfm-be/issues/5)), step 7 ([dfm-be#7](https://github.com/saudm6/dfm-be/issues/7)), step 48 ([dfm-be#36](https://github.com/saudm6/dfm-be/issues/36)).
- [ ] **50.** [dfm-be#39](https://github.com/saudm6/dfm-be/issues/39) Send alert notifications by email and web push. Wave 13. Waits on step 49 ([dfm-be#37](https://github.com/saudm6/dfm-be/issues/37)).
- **51.** [dfm-fe#24](https://github.com/saudm6/dfm-fe/issues/24) Build the alert manager (ALRT). Wave 13. Waits on step 18 ([dfm-fe#7](https://github.com/saudm6/dfm-fe/issues/7)), step 49 ([dfm-be#37](https://github.com/saudm6/dfm-be/issues/37)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **52.** [dfm-fe#25](https://github.com/saudm6/dfm-fe/issues/25) Show triggered alerts as toasts, notifications, and an inbox. Wave 14. Waits on step 50 ([dfm-be#39](https://github.com/saudm6/dfm-be/issues/39)), step 51 ([dfm-fe#24](https://github.com/saudm6/dfm-fe/issues/24)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **53.** [dfm-fe#26](https://github.com/saudm6/dfm-fe/issues/26) Create alerts from the watchlist, quote panel, and chart. Wave 14. Waits on step 28 ([dfm-fe#10](https://github.com/saudm6/dfm-fe/issues/10)), step 43 ([dfm-fe#17](https://github.com/saudm6/dfm-fe/issues/17)), step 51 ([dfm-fe#24](https://github.com/saudm6/dfm-fe/issues/24)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).

### F04 News and disclosures

- [ ] **54.** [dfm-be#22](https://github.com/saudm6/dfm-be/issues/22) Add the disclosures API and live topic. Wave 6. Waits on step 5 ([dfm-be#5](https://github.com/saudm6/dfm-be/issues/5)), step 7 ([dfm-be#7](https://github.com/saudm6/dfm-be/issues/7)), step 35 ([dfm-be#21](https://github.com/saudm6/dfm-be/issues/21)).
- **55.** [dfm-fe#15](https://github.com/saudm6/dfm-fe/issues/15) Build the disclosures feed (N and CN). Wave 9. Waits on step 18 ([dfm-fe#7](https://github.com/saudm6/dfm-fe/issues/7)), step 54 ([dfm-be#22](https://github.com/saudm6/dfm-be/issues/22)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **56.** [dfm-fe#16](https://github.com/saudm6/dfm-fe/issues/16) Toast new disclosures for watched instruments. Wave 12. Waits on step 27 ([dfm-be#18](https://github.com/saudm6/dfm-be/issues/18)), step 55 ([dfm-fe#15](https://github.com/saudm6/dfm-fe/issues/15)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).

### F10 Order entry

- [ ] **57.** [dfm-be#41](https://github.com/saudm6/dfm-be/issues/41) Model orders as a state machine with an audit log. Wave 3. Waits on step 2 ([dfm-be#2](https://github.com/saudm6/dfm-be/issues/2)), step 3 ([dfm-be#3](https://github.com/saudm6/dfm-be/issues/3)).
- [ ] **58.** [dfm-be#42](https://github.com/saudm6/dfm-be/issues/42) Route orders to IBKR and reconcile their status. Wave 9. Waits on step 23 ([dfm-be#14](https://github.com/saudm6/dfm-be/issues/14)), step 57 ([dfm-be#41](https://github.com/saudm6/dfm-be/issues/41)).
- [ ] **59.** [dfm-be#43](https://github.com/saudm6/dfm-be/issues/43) Add pre-trade risk checks and a kill switch. Wave 11. Waits on step 8 ([dfm-be#8](https://github.com/saudm6/dfm-be/issues/8)), step 26 ([dfm-be#17](https://github.com/saudm6/dfm-be/issues/17)), step 57 ([dfm-be#41](https://github.com/saudm6/dfm-be/issues/41)).
- [ ] **60.** [dfm-be#44](https://github.com/saudm6/dfm-be/issues/44) Add the orders API with paper and live modes. Wave 12. Waits on step 7 ([dfm-be#7](https://github.com/saudm6/dfm-be/issues/7)), step 58 ([dfm-be#42](https://github.com/saudm6/dfm-be/issues/42)), step 59 ([dfm-be#43](https://github.com/saudm6/dfm-be/issues/43)).
- **61.** [dfm-fe#31](https://github.com/saudm6/dfm-fe/issues/31) Build the order ticket. Wave 13. Waits on step 29 ([dfm-fe#11](https://github.com/saudm6/dfm-fe/issues/11)), step 60 ([dfm-be#44](https://github.com/saudm6/dfm-be/issues/44)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **62.** [dfm-fe#32](https://github.com/saudm6/dfm-fe/issues/32) Build the order blotter. Wave 13. Waits on step 15 ([dfm-fe#6](https://github.com/saudm6/dfm-fe/issues/6)), step 60 ([dfm-be#44](https://github.com/saudm6/dfm-be/issues/44)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **63.** [dfm-fe#33](https://github.com/saudm6/dfm-fe/issues/33) Prefill orders from bid and ask clicks and hotkeys. Wave 14. Waits on step 28 ([dfm-fe#10](https://github.com/saudm6/dfm-fe/issues/10)), step 61 ([dfm-fe#31](https://github.com/saudm6/dfm-fe/issues/31)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).

### F11 Positions and live P&L

- [ ] **64.** [dfm-be#45](https://github.com/saudm6/dfm-be/issues/45) Sync positions, cash, and executions from IBKR. Wave 10. Waits on step 58 ([dfm-be#42](https://github.com/saudm6/dfm-be/issues/42)).
- [ ] **65.** [dfm-be#46](https://github.com/saudm6/dfm-be/issues/46) Compute live P&L from positions and quotes. Wave 11. Waits on step 26 ([dfm-be#17](https://github.com/saudm6/dfm-be/issues/17)), step 64 ([dfm-be#45](https://github.com/saudm6/dfm-be/issues/45)).
- [ ] **66.** [dfm-be#47](https://github.com/saudm6/dfm-be/issues/47) Add the positions and account API. Wave 12. Waits on step 7 ([dfm-be#7](https://github.com/saudm6/dfm-be/issues/7)), step 65 ([dfm-be#46](https://github.com/saudm6/dfm-be/issues/46)).
- **67.** [dfm-fe#34](https://github.com/saudm6/dfm-fe/issues/34) Build the positions grid. Wave 13. Waits on step 15 ([dfm-fe#6](https://github.com/saudm6/dfm-fe/issues/6)), step 66 ([dfm-be#47](https://github.com/saudm6/dfm-be/issues/47)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **68.** [dfm-fe#35](https://github.com/saudm6/dfm-fe/issues/35) Build the account summary bar. Wave 14. Waits on step 67 ([dfm-fe#34](https://github.com/saudm6/dfm-fe/issues/34)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).

### F06 Launchpad workspace

- **69.** [dfm-fe#27](https://github.com/saudm6/dfm-fe/issues/27) Build the docking workspace (BLP). Wave 9. Waits on step 18 ([dfm-fe#7](https://github.com/saudm6/dfm-fe/issues/7)), step 46 ([dfm-be#40](https://github.com/saudm6/dfm-be/issues/40)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **70.** [dfm-fe#28](https://github.com/saudm6/dfm-fe/issues/28) Link panels with security groups. Wave 10. Waits on step 69 ([dfm-fe#27](https://github.com/saudm6/dfm-fe/issues/27)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **71.** [dfm-fe#29](https://github.com/saudm6/dfm-fe/issues/29) Pop panels out into separate windows. Wave 11. Waits on step 70 ([dfm-fe#28](https://github.com/saudm6/dfm-fe/issues/28)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **72.** [dfm-fe#30](https://github.com/saudm6/dfm-fe/issues/30) Navigate panels from the keyboard. Wave 10. Waits on step 69 ([dfm-fe#27](https://github.com/saudm6/dfm-fe/issues/27)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).

### F08 Correlation and pair spreads

- [x] **73.** [dfm-be#32](https://github.com/saudm6/dfm-be/issues/32) Build the returns and pairs analytics package. Wave 3. Waits on step 3 ([dfm-be#3](https://github.com/saudm6/dfm-be/issues/3)).
- [ ] **74.** [dfm-be#33](https://github.com/saudm6/dfm-be/issues/33) Serve correlation, regression, and spread endpoints. Wave 8. Waits on step 40 ([dfm-be#27](https://github.com/saudm6/dfm-be/issues/27)), step 42 ([dfm-be#29](https://github.com/saudm6/dfm-be/issues/29)), step 73 ([dfm-be#32](https://github.com/saudm6/dfm-be/issues/32)).
- **75.** [dfm-fe#19](https://github.com/saudm6/dfm-fe/issues/19) Build the correlation matrix (CORR). Wave 9. Waits on step 18 ([dfm-fe#7](https://github.com/saudm6/dfm-fe/issues/7)), step 74 ([dfm-be#33](https://github.com/saudm6/dfm-be/issues/33)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **76.** [dfm-fe#20](https://github.com/saudm6/dfm-fe/issues/20) Build the regression and hedge ratio view (HRA). Wave 9. Waits on step 18 ([dfm-fe#7](https://github.com/saudm6/dfm-fe/issues/7)), step 74 ([dfm-be#33](https://github.com/saudm6/dfm-be/issues/33)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **77.** [dfm-fe#21](https://github.com/saudm6/dfm-fe/issues/21) Build the pair spread chart (HS). Wave 10. Waits on step 43 ([dfm-fe#17](https://github.com/saudm6/dfm-fe/issues/17)), step 74 ([dfm-be#33](https://github.com/saudm6/dfm-be/issues/33)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).

### F09 Custom spreads and baskets

- [ ] **78.** [dfm-be#34](https://github.com/saudm6/dfm-be/issues/34) Build the custom series expression engine. Wave 11. Waits on step 26 ([dfm-be#17](https://github.com/saudm6/dfm-be/issues/17)), step 42 ([dfm-be#29](https://github.com/saudm6/dfm-be/issues/29)).
- [ ] **79.** [dfm-be#35](https://github.com/saudm6/dfm-be/issues/35) Save custom series as pseudo-instruments. Wave 12. Waits on step 27 ([dfm-be#18](https://github.com/saudm6/dfm-be/issues/18)), step 78 ([dfm-be#34](https://github.com/saudm6/dfm-be/issues/34)).
- [ ] **80.** [dfm-be#38](https://github.com/saudm6/dfm-be/issues/38) Add technical, disclosure, and custom series conditions. Part of F05. Wave 13. Waits on step 35 ([dfm-be#21](https://github.com/saudm6/dfm-be/issues/21)), step 45 ([dfm-be#31](https://github.com/saudm6/dfm-be/issues/31)), step 48 ([dfm-be#36](https://github.com/saudm6/dfm-be/issues/36)), step 79 ([dfm-be#35](https://github.com/saudm6/dfm-be/issues/35)).
- **81.** [dfm-fe#22](https://github.com/saudm6/dfm-fe/issues/22) Build the custom series editor (CIX). Wave 13. Waits on step 19 ([dfm-fe#8](https://github.com/saudm6/dfm-fe/issues/8)), step 43 ([dfm-fe#17](https://github.com/saudm6/dfm-fe/issues/17)), step 79 ([dfm-be#35](https://github.com/saudm6/dfm-be/issues/35)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **82.** [dfm-fe#23](https://github.com/saudm6/dfm-fe/issues/23) Accept custom series wherever a symbol is accepted. Wave 14. Waits on step 28 ([dfm-fe#10](https://github.com/saudm6/dfm-fe/issues/10)), step 81 ([dfm-fe#22](https://github.com/saudm6/dfm-fe/issues/22)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).

### F13 Portfolio analytics

- [ ] **83.** [dfm-be#51](https://github.com/saudm6/dfm-be/issues/51) Snapshot the portfolio after each close. Wave 12. Waits on step 8 ([dfm-be#8](https://github.com/saudm6/dfm-be/issues/8)), step 65 ([dfm-be#46](https://github.com/saudm6/dfm-be/issues/46)).
- [ ] **84.** [dfm-be#52](https://github.com/saudm6/dfm-be/issues/52) Compute portfolio performance and exposure. Wave 13. Waits on step 42 ([dfm-be#29](https://github.com/saudm6/dfm-be/issues/29)), step 83 ([dfm-be#51](https://github.com/saudm6/dfm-be/issues/51)).
- [ ] **85.** [dfm-be#53](https://github.com/saudm6/dfm-be/issues/53) Compute portfolio risk. Wave 14. Waits on step 40 ([dfm-be#27](https://github.com/saudm6/dfm-be/issues/27)), step 73 ([dfm-be#32](https://github.com/saudm6/dfm-be/issues/32)), step 84 ([dfm-be#52](https://github.com/saudm6/dfm-be/issues/52)).
- [ ] **86.** [dfm-be#54](https://github.com/saudm6/dfm-be/issues/54) Simulate a trade's effect on the portfolio. Wave 15. Waits on step 85 ([dfm-be#53](https://github.com/saudm6/dfm-be/issues/53)).
- **87.** [dfm-fe#40](https://github.com/saudm6/dfm-fe/issues/40) Build the portfolio overview (PORT). Wave 14. Waits on step 43 ([dfm-fe#17](https://github.com/saudm6/dfm-fe/issues/17)), step 84 ([dfm-be#52](https://github.com/saudm6/dfm-be/issues/52)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **88.** [dfm-fe#41](https://github.com/saudm6/dfm-fe/issues/41) Build the exposure and risk tabs. Wave 15. Waits on step 85 ([dfm-be#53](https://github.com/saudm6/dfm-be/issues/53)), step 87 ([dfm-fe#40](https://github.com/saudm6/dfm-fe/issues/40)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **89.** [dfm-fe#42](https://github.com/saudm6/dfm-fe/issues/42) Build the what-if trade simulator. Wave 16. Waits on step 61 ([dfm-fe#31](https://github.com/saudm6/dfm-fe/issues/31)), step 86 ([dfm-be#54](https://github.com/saudm6/dfm-be/issues/54)), step 88 ([dfm-fe#41](https://github.com/saudm6/dfm-fe/issues/41)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).

### F12 Chat

- [ ] **90.** [dfm-be#48](https://github.com/saudm6/dfm-be/issues/48) Add chat rooms and messages. Wave 6. Waits on step 5 ([dfm-be#5](https://github.com/saudm6/dfm-be/issues/5)), step 7 ([dfm-be#7](https://github.com/saudm6/dfm-be/issues/7)).
- [ ] **91.** [dfm-be#49](https://github.com/saudm6/dfm-be/issues/49) Parse instrument mentions and track unread counts. Wave 7. Waits on step 16 ([dfm-be#10](https://github.com/saudm6/dfm-be/issues/10)), step 90 ([dfm-be#48](https://github.com/saudm6/dfm-be/issues/48)).
- [ ] **92.** [dfm-be#50](https://github.com/saudm6/dfm-be/issues/50) Add message search, edits, and deletes. Wave 7. Waits on step 90 ([dfm-be#48](https://github.com/saudm6/dfm-be/issues/48)).
- **93.** [dfm-fe#36](https://github.com/saudm6/dfm-fe/issues/36) Build the chat panel (IB). Wave 8. Waits on step 15 ([dfm-fe#6](https://github.com/saudm6/dfm-fe/issues/6)), step 90 ([dfm-be#48](https://github.com/saudm6/dfm-be/issues/48)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **94.** [dfm-fe#37](https://github.com/saudm6/dfm-fe/issues/37) Autocomplete $SYMBOL mentions and render them as chips. Wave 10. Waits on step 19 ([dfm-fe#8](https://github.com/saudm6/dfm-fe/issues/8)), step 91 ([dfm-be#49](https://github.com/saudm6/dfm-be/issues/49)), step 93 ([dfm-fe#36](https://github.com/saudm6/dfm-fe/issues/36)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **95.** [dfm-fe#38](https://github.com/saudm6/dfm-fe/issues/38) Notify on mentions and new messages. Wave 15. Waits on step 52 ([dfm-fe#25](https://github.com/saudm6/dfm-fe/issues/25)), step 93 ([dfm-fe#36](https://github.com/saudm6/dfm-fe/issues/36)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
- **96.** [dfm-fe#39](https://github.com/saudm6/dfm-fe/issues/39) Search, edit, and delete chat messages. Wave 9. Waits on step 92 ([dfm-be#50](https://github.com/saudm6/dfm-be/issues/50)), step 93 ([dfm-fe#36](https://github.com/saudm6/dfm-fe/issues/36)). Tracked in [dfm-fe](https://github.com/saudm6/dfm-fe/blob/main/ROADMAP.md).
