package commands

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_parseDevice(t *testing.T) {
	b, err := base64.RawStdEncoding.DecodeString("c2hhZ3Vhci5pZGVudGlmeQoKICAgIG5hbWU6IEhlc3QKCiAgICBhZGRyZXNzOiAxOTIuMTY4LjEzMC4xMzo5MDAwCiAgICA")
	fmt.Printf("Str: '%s'\n", string(b))
	require.NoError(t, err)
	_, err = parseDevice(b)
	require.NoError(t, err)
}
