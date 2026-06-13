package plugin

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/ignacioballester/oc-plugin-sdk/pluginclient"
)

// openTestDB opens a fresh per-(plugin, org) SQLite in a temp dir with all
// migrations applied. Mirrors the production OpenDB path so the test exercises
// real migration SQL, not stubs.
func openTestDB(t *testing.T) *pluginclient.Client {
	t.Helper()
	root := t.TempDir()
	c, err := pluginclient.NewFromConfig(pluginclient.Config{
		PluginID:      "yfinance-app_test",
		PlatformToken: "test-platform-token",
		OrgID:         "00000000-0000-0000-0000-000000000001",
		PluginsRoot:   root,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func testCtx() context.Context {
	return pluginclient.WithIdentity(context.Background(), pluginclient.Identity{
		PluginID: "yfinance-app_test",
		OrgID:    uuid.MustParse("00000000-0000-0000-0000-000000000001"),
	})
}

func TestGwClassificationView(t *testing.T) {
	c := openTestDB(t)
	ctx := testCtx()

	db, err := c.OpenDB(ctx, migrationsFS)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}

	t.Run("view exists with correct column order", func(t *testing.T) {
		rows, err := db.QueryContext(ctx, `PRAGMA table_info(gw_classification)`)
		if err != nil {
			t.Fatalf("PRAGMA table_info: %v", err)
		}
		defer rows.Close()

		want := []string{"portfolio", "instrument_id", "ts", "sector", "industry"}
		var got []string
		for rows.Next() {
			var cid int
			var name, typ string
			var notNull int
			var dfltValue interface{}
			var pk int
			if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
				t.Fatalf("scan: %v", err)
			}
			got = append(got, name)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if len(got) == 0 {
			t.Fatal("gw_classification view not found or has no columns")
		}
		if len(got) != len(want) {
			t.Fatalf("column count = %d, want %d; got %v", len(got), len(want), got)
		}
		for i, col := range want {
			if got[i] != col {
				t.Errorf("column[%d] = %q, want %q", i, got[i], col)
			}
		}
	})

	t.Run("@latest grain: newest sector wins", func(t *testing.T) {
		const portfolio = "port-1"
		const instrument = "instr-A"
		t1 := int64(1_000_000)
		t2 := int64(2_000_000)
		sector1 := "Energy"
		sector2 := "Technology"

		_, err := db.ExecContext(ctx, `
			INSERT INTO instrument_ticker_mapping
			    (instrument_id, portfolio_id, symbol, vendor_meta, created_at, updated_at, updated_by, sector, subindustry)
			VALUES (?, ?, 'TICK1', '{}', ?, ?, 'test', ?, 'Semiconductors')
		`, instrument, portfolio, t1, t1, sector1)
		if err != nil {
			t.Fatalf("insert row1: %v", err)
		}

		_, err = db.ExecContext(ctx, `
			UPDATE instrument_ticker_mapping
			   SET sector = ?, updated_at = ?
			 WHERE instrument_id = ? AND portfolio_id = ?
		`, sector2, t2, instrument, portfolio)
		if err != nil {
			t.Fatalf("update row to t2: %v", err)
		}

		var sector string
		err = db.QueryRowContext(ctx, `
			SELECT sector FROM (
				SELECT *, ROW_NUMBER() OVER (PARTITION BY portfolio, instrument_id ORDER BY ts DESC) rn
				FROM gw_classification
			) WHERE rn = 1
			  AND portfolio = ? AND instrument_id = ?
		`, portfolio, instrument).Scan(&sector)
		if err != nil {
			t.Fatalf("@latest query: %v", err)
		}
		if sector != sector2 {
			t.Fatalf("sector = %q, want %q", sector, sector2)
		}
	})
}
