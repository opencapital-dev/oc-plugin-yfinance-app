package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

// TickerMapping is the per-(plugin, portfolio) Yahoo-symbol mapping.
type TickerMapping struct {
	InstrumentID string         `json:"instrument_id"`
	PortfolioID  string         `json:"portfolio_id"`
	Symbol       string         `json:"symbol"`
	Sector       *string        `json:"sector,omitempty"`
	Subindustry  *string        `json:"subindustry,omitempty"`
	VendorMeta   map[string]any `json:"vendor_meta"`
	Subscribed   bool           `json:"subscribed"`
	CreatedAt    *int64         `json:"created_at"`
	UpdatedAt    *int64         `json:"updated_at"`
	UpdatedBy    *string        `json:"updated_by"`
}

func scanPGTickerMapping(row []any, col map[string]int) (TickerMapping, error) {
	var m TickerMapping
	if v, ok := row[col["instrument_id"]].(string); ok {
		m.InstrumentID = v
	}
	if v, ok := row[col["portfolio_id"]].(string); ok {
		m.PortfolioID = v
	}
	if v, ok := row[col["symbol"]].(string); ok {
		m.Symbol = v
	}
	if v, ok := row[col["sector"]].(string); ok {
		m.Sector = &v
	}
	if v, ok := row[col["subindustry"]].(string); ok {
		m.Subindustry = &v
	}
	// vendor_meta comes back as map[string]interface{} from pgx JSONB
	switch vm := row[col["vendor_meta"]].(type) {
	case map[string]interface{}:
		m.VendorMeta = vm
	case []byte:
		_ = json.Unmarshal(vm, &m.VendorMeta)
	case string:
		_ = json.Unmarshal([]byte(vm), &m.VendorMeta)
	}
	if m.VendorMeta == nil {
		m.VendorMeta = map[string]any{}
	}
	if v, ok := row[col["subscribed"]].(bool); ok {
		m.Subscribed = v
	} else {
		m.Subscribed = true
	}
	if v, ok := row[col["created_at"]].(int64); ok {
		m.CreatedAt = &v
	}
	if v, ok := row[col["updated_at"]].(int64); ok {
		m.UpdatedAt = &v
	}
	if v, ok := row[col["updated_by"]].(string); ok {
		m.UpdatedBy = &v
	}
	return m, nil
}

func (a *App) UpsertTickerMapping(ctx context.Context, instrumentID, portfolioID, symbol string, vendorMeta map[string]any, updatedBy string) (TickerMapping, error) {
	if vendorMeta == nil {
		vendorMeta = map[string]any{}
	}
	metaJSON, err := json.Marshal(vendorMeta)
	if err != nil {
		return TickerMapping{}, fmt.Errorf("marshal vendor_meta: %w", err)
	}

	// Capture the prior symbol before the upsert. A changed symbol is a
	// remap to a different company, so every price previously written under
	// this instrument_id (backfilled bars + live quotes) is stale and must
	// be purged — otherwise an old quote in a different currency lingers and
	// inflates NAV (the leftover-datapoint bug).
	priorSymbol := ""
	if prior, perr := a.GetTickerMapping(ctx, instrumentID, portfolioID); perr == nil {
		priorSymbol = prior.Symbol
	}

	now := nowMicros()
	_, err = a.client.PGExec(ctx, `
		INSERT INTO basic_data.instrument_ticker_mapping
			(instrument_id, portfolio_id, symbol, vendor_meta, subscribed, created_at, updated_at, updated_by)
		VALUES ($1, $2, $3, $4::jsonb, TRUE, $5, $6, $7)
		ON CONFLICT (instrument_id, portfolio_id) DO UPDATE
		  SET symbol      = EXCLUDED.symbol,
		      vendor_meta = EXCLUDED.vendor_meta,
		      updated_at  = EXCLUDED.updated_at,
		      updated_by  = EXCLUDED.updated_by
	`, instrumentID, portfolioID, symbol, string(metaJSON), now, now, updatedBy)
	if err != nil {
		return TickerMapping{}, fmt.Errorf("upsert mapping: %w", err)
	}

	if priorSymbol != "" && priorSymbol != symbol {
		if perr := a.PurgeInstrumentPrices(ctx, instrumentID, portfolioID); perr != nil {
			log.DefaultLogger.Warn("price purge on symbol remap failed",
				"instrument_id", instrumentID, "portfolio_id", portfolioID,
				"old_symbol", priorSymbol, "new_symbol", symbol, "err", perr)
		}
	}

	return a.GetTickerMapping(ctx, instrumentID, portfolioID)
}

func (a *App) SetClassification(ctx context.Context, instrumentID, portfolioID string, sector, industry *string, source string) (TickerMapping, error) {
	cur, err := a.GetTickerMapping(ctx, instrumentID, portfolioID)
	if err != nil {
		return TickerMapping{}, err
	}
	meta := cur.VendorMeta
	if sector != nil {
		meta = setClassificationSource(meta, fieldSector, source)
	}
	if industry != nil {
		meta = setClassificationSource(meta, fieldIndustry, source)
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return TickerMapping{}, fmt.Errorf("marshal vendor_meta: %w", err)
	}
	_, err = a.client.PGExec(ctx, `
		UPDATE basic_data.instrument_ticker_mapping
		   SET sector      = COALESCE($1, sector),
		       subindustry = COALESCE($2, subindustry),
		       vendor_meta = $3::jsonb,
		       updated_at  = $4,
		       updated_by  = $5
		 WHERE instrument_id = $6
		   AND portfolio_id  = $7
	`, sector, industry, string(metaJSON), nowMicros(), source, instrumentID, portfolioID)
	if err != nil {
		return TickerMapping{}, fmt.Errorf("set classification: %w", err)
	}
	return a.GetTickerMapping(ctx, instrumentID, portfolioID)
}

func (a *App) GetTickerMapping(ctx context.Context, instrumentID, portfolioID string) (TickerMapping, error) {
	res, err := a.client.PGQuery(ctx, `
		SELECT instrument_id, portfolio_id, symbol, sector, subindustry,
		       vendor_meta, subscribed, created_at, updated_at, updated_by
		  FROM basic_data.instrument_ticker_mapping
		 WHERE instrument_id = $1
		   AND portfolio_id  = $2
	`, instrumentID, portfolioID)
	if err != nil {
		return TickerMapping{}, fmt.Errorf("get mapping: %w", err)
	}
	if len(res.Rows) == 0 {
		return TickerMapping{}, errNotFound
	}
	col := colIndex(res.Columns)
	return scanPGTickerMapping(res.Rows[0], col)
}

func (a *App) ListSubscribedTickerMappings(ctx context.Context) ([]TickerMapping, error) {
	res, err := a.client.PGQuery(ctx, `
		SELECT instrument_id, portfolio_id, symbol, sector, subindustry,
		       vendor_meta, subscribed, created_at, updated_at, updated_by
		  FROM basic_data.instrument_ticker_mapping
		 WHERE subscribed
		   AND (vendor_meta->>'kind') IS DISTINCT FROM 'option_underlying'
		 ORDER BY portfolio_id, instrument_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list subscribed mappings: %w", err)
	}
	col := colIndex(res.Columns)
	out := make([]TickerMapping, 0, len(res.Rows))
	for _, row := range res.Rows {
		m, err := scanPGTickerMapping(row, col)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (a *App) ListTickerMappings(ctx context.Context) ([]TickerMapping, error) {
	res, err := a.client.PGQuery(ctx, `
		SELECT instrument_id, portfolio_id, symbol, sector, subindustry,
		       vendor_meta, subscribed, created_at, updated_at, updated_by
		  FROM basic_data.instrument_ticker_mapping
		 ORDER BY portfolio_id, instrument_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list mappings: %w", err)
	}
	col := colIndex(res.Columns)
	out := make([]TickerMapping, 0, len(res.Rows))
	for _, row := range res.Rows {
		m, err := scanPGTickerMapping(row, col)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// SetCanonicalIdentity records the REST-resolved canonical Yahoo identity for an
// instrument under vendor_meta.canonical = {symbol, exch, currency, ref_price}.
// The live subscriber reads symbol via canonicalSymbol() so it subscribes the
// same listing REST resolved, and reads currency + ref_price via canonicalUnit()
// so it normalizes minor-unit (pence) ticks without trusting the unreliable ws
// currency field. currency is Yahoo's raw metadata currency (e.g. "GBp");
// refPrice is the minor-unit reference (FastInfo) used as the classifier anchor.
// A backfill that cannot determine the unit (currency == "" / refPrice <= 0,
// e.g. a major-unit ticker or a FastInfo miss) preserves the previously-resolved
// values so the live anchor stays stable. Best-effort; a no-op when symbol is empty.
func (a *App) SetCanonicalIdentity(ctx context.Context, instrumentID, portfolioID, symbol, exchange, currency string, refPrice float64) error {
	if symbol == "" {
		return nil
	}
	cur, err := a.GetTickerMapping(ctx, instrumentID, portfolioID)
	if err != nil {
		return err
	}
	meta := cur.VendorMeta
	if meta == nil {
		meta = map[string]any{}
	}
	// Carry forward a prior minor-unit anchor when this backfill didn't resolve one.
	if prev, ok := meta["canonical"].(map[string]any); ok {
		if currency == "" {
			if pc, ok := prev["currency"].(string); ok {
				currency = pc
			}
		}
		if refPrice <= 0 {
			if pr, ok := prev["ref_price"].(float64); ok {
				refPrice = pr
			}
		}
	}
	canonical := map[string]any{
		"symbol": symbol,
		"exch":   strings.ToUpper(strings.TrimSpace(exchange)),
	}
	if currency != "" {
		canonical["currency"] = currency
	}
	if refPrice > 0 {
		canonical["ref_price"] = refPrice
	}
	meta["canonical"] = canonical
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal vendor_meta: %w", err)
	}
	_, err = a.client.PGExec(ctx, `
		UPDATE basic_data.instrument_ticker_mapping
		   SET vendor_meta = $1::jsonb,
		       updated_at  = $2
		 WHERE instrument_id = $3
		   AND portfolio_id  = $4
	`, string(metaJSON), nowMicros(), instrumentID, portfolioID)
	if err != nil {
		return fmt.Errorf("set canonical identity: %w", err)
	}
	return nil
}
