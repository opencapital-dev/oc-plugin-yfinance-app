package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ignacioballester/oc-plugin-sdk/pluginclient"
)

// TickerMapping is the per-(plugin, org, portfolio) Yahoo-symbol mapping.
// Keyed by (instrument_id, portfolio_id) so that the same broker ticker can
// map to different securities across portfolios. sector/subindustry are
// plugin-private enrichment — they never travel to RisingWave or the wire.
type TickerMapping struct {
	InstrumentID string         `json:"instrument_id"`
	PortfolioID  string         `json:"portfolio_id"`
	Symbol       string         `json:"symbol"`
	Sector       *string        `json:"sector,omitempty"`
	Subindustry  *string        `json:"subindustry,omitempty"`
	VendorMeta   map[string]any `json:"vendor_meta"`
	CreatedAt    *int64         `json:"created_at"`
	UpdatedAt    *int64         `json:"updated_at"`
	UpdatedBy    *string        `json:"updated_by"`
}

// openSQLite is the small wrapper around pluginclient.OpenDB that every
// SQLite-touching code path goes through. Cached inside the client cache;
// the call is cheap on subsequent hits.
func (a *App) openSQLite(ctx context.Context) (*sql.DB, error) {
	if a.client == nil {
		return nil, errors.New("yfinance: pluginclient not initialised")
	}
	return a.client.OpenDB(ctx, migrationsFS)
}

// UpsertTickerMapping inserts or updates the symbol for a (instrument, portfolio) pair.
func (a *App) UpsertTickerMapping(ctx context.Context, instrumentID, portfolioID, symbol string, vendorMeta map[string]any, updatedBy string) (TickerMapping, error) {
	db, err := a.openSQLite(ctx)
	if err != nil {
		return TickerMapping{}, err
	}
	if vendorMeta == nil {
		vendorMeta = map[string]any{}
	}
	metaJSON, err := json.Marshal(vendorMeta)
	if err != nil {
		return TickerMapping{}, fmt.Errorf("marshal vendor_meta: %w", err)
	}
	now := nowMicros()
	_, err = db.ExecContext(ctx, `
		INSERT INTO instrument_ticker_mapping
		    (instrument_id, portfolio_id, symbol, vendor_meta, created_at, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instrument_id, portfolio_id) DO UPDATE
		  SET symbol      = excluded.symbol,
		      vendor_meta = excluded.vendor_meta,
		      updated_at  = excluded.updated_at,
		      updated_by  = excluded.updated_by
	`, instrumentID, portfolioID, symbol, string(metaJSON), now, now, updatedBy)
	if err != nil {
		return TickerMapping{}, fmt.Errorf("upsert mapping: %w", err)
	}
	return a.GetTickerMapping(ctx, instrumentID, portfolioID)
}

// SetClassification updates the sector and/or industry (subindustry column)
// for an existing (instrument, portfolio) mapping and records each updated
// field's origin in vendor_meta via setClassificationSource. A nil sector or
// industry pointer leaves that column unchanged. Only fields with a non-nil
// pointer have their *_source recorded. Returns the refreshed row.
func (a *App) SetClassification(ctx context.Context, instrumentID, portfolioID string, sector, industry *string, source string) (TickerMapping, error) {
	db, err := a.openSQLite(ctx)
	if err != nil {
		return TickerMapping{}, err
	}
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
	// COALESCE keeps the existing column value when the bound param is NULL
	// (nil pointer), so callers can update one field without touching the other.
	_, err = db.ExecContext(ctx, `
		UPDATE instrument_ticker_mapping
		   SET sector      = COALESCE(?, sector),
		       subindustry = COALESCE(?, subindustry),
		       vendor_meta = ?,
		       updated_at  = ?
		 WHERE instrument_id = ?
		   AND portfolio_id  = ?
	`, sector, industry, string(metaJSON), nowMicros(), instrumentID, portfolioID)
	if err != nil {
		return TickerMapping{}, fmt.Errorf("set classification: %w", err)
	}
	return a.GetTickerMapping(ctx, instrumentID, portfolioID)
}

// GetTickerMapping returns the row for (instrumentID, portfolioID) or errNotFound.
func (a *App) GetTickerMapping(ctx context.Context, instrumentID, portfolioID string) (TickerMapping, error) {
	db, err := a.openSQLite(ctx)
	if err != nil {
		return TickerMapping{}, err
	}
	row := db.QueryRowContext(ctx, `
		SELECT instrument_id, portfolio_id, symbol, sector, subindustry,
		       vendor_meta, created_at, updated_at, updated_by
		  FROM instrument_ticker_mapping
		 WHERE instrument_id = ?
		   AND portfolio_id  = ?
	`, instrumentID, portfolioID)
	m, err := scanTickerMappingRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return m, errNotFound
		}
		return m, fmt.Errorf("get mapping: %w", err)
	}
	return m, nil
}

// ListSubscribedTickerMappings returns every mapping the discovery loop +
// live subscriber treat as active (vendor_meta.subscribed != false).
func (a *App) ListSubscribedTickerMappings(ctx context.Context) ([]TickerMapping, error) {
	db, err := a.openSQLite(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT instrument_id, portfolio_id, symbol, sector, subindustry,
		       vendor_meta, created_at, updated_at, updated_by
		  FROM instrument_ticker_mapping
		 ORDER BY portfolio_id, instrument_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list mappings: %w", err)
	}
	defer rows.Close()
	out := []TickerMapping{}
	for rows.Next() {
		m, err := scanTickerMappingRow(rows)
		if err != nil {
			return nil, err
		}
		if sub, ok := m.VendorMeta["subscribed"].(bool); ok && !sub {
			continue
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListTickerMappings is the variant used by /yf/instruments (returns
// every mapping including muted ones — the handler filters as needed).
func (a *App) ListTickerMappings(ctx context.Context) ([]TickerMapping, error) {
	db, err := a.openSQLite(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT instrument_id, portfolio_id, symbol, sector, subindustry,
		       vendor_meta, created_at, updated_at, updated_by
		  FROM instrument_ticker_mapping
		 ORDER BY portfolio_id, instrument_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list mappings: %w", err)
	}
	defer rows.Close()
	out := []TickerMapping{}
	for rows.Next() {
		m, err := scanTickerMappingRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// scanTickerMappingRow extracts the column tuple into a TickerMapping.
// Used by every SELECT against instrument_ticker_mapping.
func scanTickerMappingRow(s scanRow) (TickerMapping, error) {
	var (
		m           TickerMapping
		sector      sql.NullString
		subindustry sql.NullString
		meta        string
		created     sql.NullInt64
		updated     sql.NullInt64
		updBy       sql.NullString
	)
	if err := s.Scan(
		&m.InstrumentID, &m.PortfolioID, &m.Symbol,
		&sector, &subindustry, &meta,
		&created, &updated, &updBy,
	); err != nil {
		return m, err
	}
	if sector.Valid {
		v := sector.String
		m.Sector = &v
	}
	if subindustry.Valid {
		v := subindustry.String
		m.Subindustry = &v
	}
	if meta != "" {
		_ = json.Unmarshal([]byte(meta), &m.VendorMeta)
	}
	if m.VendorMeta == nil {
		m.VendorMeta = map[string]any{}
	}
	if created.Valid {
		v := created.Int64
		m.CreatedAt = &v
	}
	if updated.Valid {
		v := updated.Int64
		m.UpdatedAt = &v
	}
	if updBy.Valid {
		v := updBy.String
		m.UpdatedBy = &v
	}
	return m, nil
}

type scanRow interface {
	Scan(dst ...any) error
}

// Pluginclient is the App's accessor for tests that need to reach the
// underlying client (avoids exposing the field).
func (a *App) Pluginclient() *pluginclient.Client { return a.client }
