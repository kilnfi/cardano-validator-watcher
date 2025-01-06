package http

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithHost(t *testing.T) {
	t.Parallel()

	opts := &options{}
	err := WithHost("0.0.0.0")(opts)

	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", opts.host)
}

func TestWithPort(t *testing.T) {
	t.Parallel()

	opts := &options{}
	err := WithPort(9090)(opts)

	require.NoError(t, err)
	assert.Equal(t, 9090, opts.port)
}

func TestWithTimeouts(t *testing.T) {
	t.Parallel()

	opts := &options{}
	err := WithReadTimeout(15 * time.Second)(opts)
	require.NoError(t, err)

	err = WithWriteTimeout(30 * time.Second)(opts)
	require.NoError(t, err)

	assert.Equal(t, 15*time.Second, opts.readTimeout)
	assert.Equal(t, 30*time.Second, opts.writeTimeout)
}
