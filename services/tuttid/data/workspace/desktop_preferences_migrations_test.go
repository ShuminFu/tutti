package workspace

import (
	"context"
	"testing"

	preferencesbiz "github.com/tutti-os/tutti/services/tuttid/biz/preferences"
)

func TestDesktopPreferencesRuntimeRetentionLegacySchemaUpgrade(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		v1Marked   bool
		keepAlive  any
		idleSecond any
		wantKeep   int
		wantIdle   int
	}{
		{"marked-defaults", true, 1, 300, 1, 5},
		{"unmarked-settings", false, 0, 61, 0, 2},
		{"never-expire", true, 1, 0, 1, 0},
		{"over-limit", true, 1, 100000, 1, 1440},
		{"invalid-settings", true, 2, -1, 1, 30},
		{"null-settings", true, nil, nil, 1, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := openTestSQLiteStore(t)
			ctx := context.Background()
			if _, err := store.writeDB.ExecContext(ctx, `
ALTER TABLE desktop_preferences DROP COLUMN agent_runtime_keep_alive_enabled;
ALTER TABLE desktop_preferences DROP COLUMN agent_runtime_idle_minutes;
ALTER TABLE desktop_preferences DROP COLUMN agent_runtime_max_resident;
ALTER TABLE desktop_preferences ADD COLUMN agent_runtime_keep_alive INTEGER;
ALTER TABLE desktop_preferences ADD COLUMN agent_runtime_idle_ttl_seconds INTEGER;
DELETE FROM tuttid_schema_migrations WHERE id = 'desktop_preferences_agent_runtime_retention_compat_v2';
`); err != nil {
				t.Fatalf("prepare legacy schema: %v", err)
			}
			if !tc.v1Marked {
				if _, err := store.writeDB.ExecContext(ctx, `DELETE FROM tuttid_schema_migrations WHERE id = ?`, schemaMigrationDesktopPreferencesAgentRuntimeRetentionV1); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.writeDB.ExecContext(ctx, `INSERT INTO desktop_preferences (id, locale, theme_source, dock_icon_style, updated_at_unix_ms, agent_runtime_keep_alive, agent_runtime_idle_ttl_seconds) VALUES ('desktop', 'en', 'dark', 'flat', 1, ?, ?)`, tc.keepAlive, tc.idleSecond); err != nil {
				t.Fatalf("seed legacy preferences: %v", err)
			}
			if err := store.Migrate(ctx); err != nil {
				t.Fatalf("Migrate legacy schema: %v", err)
			}
			var keep, idle, max int
			if err := store.writeDB.QueryRowContext(ctx, `SELECT agent_runtime_keep_alive_enabled, agent_runtime_idle_minutes, agent_runtime_max_resident FROM desktop_preferences WHERE id='desktop'`).Scan(&keep, &idle, &max); err != nil {
				t.Fatalf("read migrated preferences: %v", err)
			}
			if keep != tc.wantKeep || idle != tc.wantIdle || max != 10 {
				t.Fatalf("migrated preferences = %d/%d/%d, want %d/%d/10", keep, idle, max, tc.wantKeep, tc.wantIdle)
			}
			if _, err := store.writeDB.ExecContext(ctx, `UPDATE desktop_preferences SET agent_runtime_keep_alive_enabled = 0, agent_runtime_idle_minutes = 17 WHERE id='desktop'`); err != nil {
				t.Fatal(err)
			}
			if err := store.Migrate(ctx); err != nil {
				t.Fatalf("repeat Migrate: %v", err)
			}
			if err := store.writeDB.QueryRowContext(ctx, `SELECT agent_runtime_keep_alive_enabled, agent_runtime_idle_minutes FROM desktop_preferences WHERE id='desktop'`).Scan(&keep, &idle); err != nil {
				t.Fatal(err)
			}
			if keep != 0 || idle != 17 {
				t.Fatalf("repeat migration overwrote user values: %d/%d", keep, idle)
			}
		})
	}
}

func TestDesktopPreferencesRuntimeRetentionCompatPreservesExistingColumn(t *testing.T) {
	t.Parallel()
	store := openTestSQLiteStore(t)
	ctx := context.Background()
	if _, err := store.writeDB.ExecContext(ctx, `
ALTER TABLE desktop_preferences DROP COLUMN agent_runtime_idle_minutes;
ALTER TABLE desktop_preferences DROP COLUMN agent_runtime_max_resident;
ALTER TABLE desktop_preferences ADD COLUMN agent_runtime_keep_alive INTEGER;
ALTER TABLE desktop_preferences ADD COLUMN agent_runtime_idle_ttl_seconds INTEGER;
DELETE FROM tuttid_schema_migrations WHERE id = 'desktop_preferences_agent_runtime_retention_compat_v2';
INSERT INTO desktop_preferences (id, locale, theme_source, dock_icon_style, updated_at_unix_ms, agent_runtime_keep_alive_enabled, agent_runtime_keep_alive, agent_runtime_idle_ttl_seconds)
VALUES ('desktop', 'en', 'dark', 'flat', 1, 0, 1, 300);
`); err != nil {
		t.Fatalf("prepare partial legacy schema: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate partial legacy schema: %v", err)
	}
	var keep, idle, max int
	if err := store.writeDB.QueryRowContext(ctx, `SELECT agent_runtime_keep_alive_enabled, agent_runtime_idle_minutes, agent_runtime_max_resident FROM desktop_preferences WHERE id='desktop'`).Scan(&keep, &idle, &max); err != nil {
		t.Fatal(err)
	}
	if keep != 0 || idle != 5 || max != 10 {
		t.Fatalf("partially upgraded preferences = %d/%d/%d, want 0/5/10", keep, idle, max)
	}
}

func TestDesktopPreferencesRuntimeRetentionCompatRollsBackOnMarkerFailure(t *testing.T) {
	t.Parallel()
	store := openTestSQLiteStore(t)
	ctx := context.Background()
	if _, err := store.writeDB.ExecContext(ctx, `
ALTER TABLE desktop_preferences DROP COLUMN agent_runtime_keep_alive_enabled;
ALTER TABLE desktop_preferences DROP COLUMN agent_runtime_idle_minutes;
ALTER TABLE desktop_preferences DROP COLUMN agent_runtime_max_resident;
DELETE FROM tuttid_schema_migrations WHERE id = 'desktop_preferences_agent_runtime_retention_compat_v2';
CREATE TRIGGER reject_retention_compat_marker BEFORE INSERT ON tuttid_schema_migrations
WHEN NEW.id = 'desktop_preferences_agent_runtime_retention_compat_v2'
BEGIN SELECT RAISE(ABORT, 'test marker write failure'); END;
`); err != nil {
		t.Fatalf("prepare marker failure: %v", err)
	}
	if err := store.applyDesktopPreferencesAgentRuntimeRetentionCompatV2(ctx); err == nil {
		t.Fatal("compat migration unexpectedly succeeded")
	}
	for _, column := range []string{"agent_runtime_keep_alive_enabled", "agent_runtime_idle_minutes", "agent_runtime_max_resident"} {
		has, err := store.hasColumn(ctx, "desktop_preferences", column)
		if err != nil || has {
			t.Fatalf("column %s after rollback: has=%v err=%v", column, has, err)
		}
	}
	if _, err := store.writeDB.ExecContext(ctx, `DROP TRIGGER reject_retention_compat_marker`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("retry migration after rollback: %v", err)
	}
}

func TestFeatureFlagsMigrationAddsColumns(t *testing.T) {
	t.Parallel()

	store := openTestSQLiteStore(t)
	ctx := context.Background()
	for _, col := range []string{"feature_flags_json", "workbench_shortcuts_json"} {
		ok, err := store.hasColumn(ctx, "desktop_preferences", col)
		if err != nil || !ok {
			t.Fatalf("column %s missing (err=%v)", col, err)
		}
	}
}

func TestAgentSessionLaunchModesMigrationAddsColumn(t *testing.T) {
	t.Parallel()

	store := openTestSQLiteStore(t)
	ok, err := store.hasColumn(context.Background(), "desktop_preferences", "agent_session_launch_modes_by_workspace_json")
	if err != nil || !ok {
		t.Fatalf("agent_session_launch_modes_by_workspace_json column missing (err=%v)", err)
	}
}

func TestAgentCLIUpdateCheckMigrationAddsEnabledByDefaultColumn(t *testing.T) {
	t.Parallel()

	store := openTestSQLiteStore(t)
	ctx := context.Background()
	ok, err := store.hasColumn(ctx, "desktop_preferences", "agent_cli_update_check_enabled")
	if err != nil || !ok {
		t.Fatalf("agent_cli_update_check_enabled column missing (err=%v)", err)
	}
	defaults := preferencesbiz.DefaultDesktopPreferences()
	_, err = store.writeDB.ExecContext(ctx, `
INSERT INTO desktop_preferences (id, locale, theme_source, dock_icon_style, updated_at_unix_ms)
VALUES ('migration-default', ?, ?, ?, 1)
`, defaults.Locale, defaults.ThemeSource, defaults.DockIconStyle)
	if err != nil {
		t.Fatalf("insert legacy-shaped desktop preferences: %v", err)
	}
	var enabled bool
	if err := store.readDB.QueryRowContext(ctx, `
SELECT agent_cli_update_check_enabled FROM desktop_preferences WHERE id = 'migration-default'
`).Scan(&enabled); err != nil {
		t.Fatalf("read migrated agent CLI update check preference: %v", err)
	}
	if !enabled {
		t.Fatal("migrated agent CLI update check preference = false, want true")
	}
}
