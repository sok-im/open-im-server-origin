package api

import (
	"testing"

	"github.com/openimsdk/tools/errs"
	"github.com/stretchr/testify/require"
)

func TestRequireSelfUID(t *testing.T) {
	require.True(t, errs.ErrNoPermission.Is(requireSelfUID("", "u1")))
	require.True(t, errs.ErrNoPermission.Is(requireSelfUID("u2", "u1")))
	require.NoError(t, requireSelfUID("u1", "u1"))
}

func TestValidateSetBackupInfo(t *testing.T) {
	require.True(t, errs.ErrArgs.Is(validateSetBackupInfo("", "n", 1, 0)))
	require.True(t, errs.ErrArgs.Is(validateSetBackupInfo("u1", "", 1, 0)))
	require.True(t, errs.ErrArgs.Is(validateSetBackupInfo("u1", "n", 0, 0)))
	require.True(t, errs.ErrArgs.Is(validateSetBackupInfo("u1", "n", 1, -1)))
	require.NoError(t, validateSetBackupInfo("u1", "n", 1, 0))
	require.NoError(t, validateSetBackupInfo("u1", "wallet.zip", 1720000000, 1048576))
}
