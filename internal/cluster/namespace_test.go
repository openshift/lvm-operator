package cluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetOperatorNamespace(t *testing.T) {
	got, err := GetOperatorNamespace()
	assert.Error(t, err)
	assert.Equal(t, "", got)

	t.Setenv(OperatorNamespaceEnvVar, "test-namespace")
	got, err = GetOperatorNamespace()
	assert.NoError(t, err)
	assert.Equal(t, "test-namespace", got)
}
