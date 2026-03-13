# Report Tab — Period-Based Usage & Cost Reports

## Background
CC-Bridge currently has real-time monitoring in the Logs tab, but admins cannot generate summary reports for longer periods or export breakdown data. This change adds a dedicated Report tab backed by enhanced request log aggregations and a daily report endpoint.

## Approach
Enhance the existing request log stats aggregation to include success/failure and latency metrics plus endpoint filtering, add a fixed daily aggregation endpoint for report charts, then build a frontend Report view with period selection, summary cards, breakdown tables, charts, and CSV export.

## Steps
- [x] Step 1: Enhance backend `GetStats` to support endpoint filter plus success/failure and latency metrics in totals and grouped breakdowns.
- [x] Step 2: Add backend daily report endpoint `GET /api/logs/stats/daily` for arbitrary date ranges with 1-day buckets.
- [x] Step 3: Add frontend API types and methods for report stats and daily report data.
- [x] Step 4: Create `frontend/src/components/ReportView.vue` with period selector, summary cards, breakdown tables, and export actions.
- [x] Step 5: Add report charts for daily cost, model distribution, and token breakdown.
- [x] Step 6: Add CSV export for each breakdown table.
- [x] Step 7: Wire the Report tab into `frontend/src/App.vue`.
- [x] Step 8: Add report i18n strings in `frontend/src/locales/en.ts` and `frontend/src/locales/zh-CN.ts`.

## Commits
