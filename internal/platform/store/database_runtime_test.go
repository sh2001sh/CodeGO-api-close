package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveSQLPoolConfigUsesSafeDefaults(t *testing.T) {
	t.Setenv("SQL_MAX_OPEN_CONNS", "")
	t.Setenv("SQL_MAX_IDLE_CONNS", "")
	t.Setenv("SQL_MAX_LIFETIME", "")

	config := resolveSQLPoolConfig()
	require.Equal(t, defaultSQLMaxOpenConnections, config.maxOpen)
	require.Equal(t, defaultSQLMaxIdleConnections, config.maxIdle)
	require.Equal(t, defaultSQLMaxLifetimeSeconds, config.maxLifetimeSecs)
}

func TestResolveSQLPoolConfigNormalizesUnsafeValues(t *testing.T) {
	t.Setenv("SQL_MAX_OPEN_CONNS", "8")
	t.Setenv("SQL_MAX_IDLE_CONNS", "20")
	t.Setenv("SQL_MAX_LIFETIME", "0")

	config := resolveSQLPoolConfig()
	require.Equal(t, 8, config.maxOpen)
	require.Equal(t, 8, config.maxIdle)
	require.Equal(t, defaultSQLMaxLifetimeSeconds, config.maxLifetimeSecs)
}
