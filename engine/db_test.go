package engine_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nateranda/djtools/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDB_OpenAndPing(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	// 1. Open new database in read-write mode
	db, err := engine.Open(tempDir, false)
	require.NoError(t, err)
	defer db.Close()

	assert.Equal(t, tempDir, db.Path())
	assert.False(t, db.IsReadOnly())
	assert.NotNil(t, db.MDB())

	// Ping
	err = db.Ping(ctx)
	assert.NoError(t, err)

	// Create schema
	err = db.CreateSchema(ctx)
	assert.NoError(t, err)

	// Ping again
	err = db.Ping(ctx)
	assert.NoError(t, err)
}

func TestDB_ReadOnlyMode(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	// Bootstrap database
	dbRW, err := engine.Open(tempDir, false)
	require.NoError(t, err)
	err = dbRW.CreateSchema(ctx)
	require.NoError(t, err)
	require.NoError(t, dbRW.Close())

	// Open read-only
	dbRO, err := engine.Open(tempDir, true)
	require.NoError(t, err)
	defer dbRO.Close()

	assert.True(t, dbRO.IsReadOnly())

	// Write operation in read-only should fail
	err = dbRO.WithTx(ctx, func(tx *sql.Tx) error {
		return nil
	})
	assert.ErrorContains(t, err, "cannot begin write transaction on read-only database")

	err = dbRO.CreateSchema(ctx)
	assert.ErrorContains(t, err, "cannot create schema on read-only database")
}

func TestDB_Transactions(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	db, err := engine.Open(tempDir, false)
	require.NoError(t, err)
	defer db.Close()

	err = db.CreateSchema(ctx)
	require.NoError(t, err)

	// Transaction commit
	err = db.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO AlbumArt (hash, albumArt) VALUES ('txhash', X'1234')`)
		return err
	})
	assert.NoError(t, err)

	// Verify committed
	var count int
	err = db.MDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM AlbumArt WHERE hash = 'txhash'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Transaction rollback on error
	err = db.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO AlbumArt (hash, albumArt) VALUES ('rollbackhash', X'5678')`)
		require.NoError(t, err)
		return errors.New("simulated error")
	})
	assert.ErrorContains(t, err, "simulated error")

	// Verify not committed
	err = db.MDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM AlbumArt WHERE hash = 'rollbackhash'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestDB_DirectDatabasePathFallback(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	// Create m.db directly in tempDir rather than Database2/m.db
	mPath := filepath.Join(tempDir, "m.db")
	f, err := os.Create(mPath)
	require.NoError(t, err)
	_ = f.Close()

	db, err := engine.Open(tempDir, false)
	require.NoError(t, err)
	defer db.Close()

	err = db.CreateSchema(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, db.MDB())
}
