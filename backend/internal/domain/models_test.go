package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllModels_ReturnsExpectedCount(t *testing.T) {
	models := AllModels()
	assert.Len(t, models, 16, "AllModels should return all 16 model types")
}

func TestNormalizedDomain_TableName(t *testing.T) {
	d := NormalizedDomain{}
	assert.Equal(t, "domains", d.TableName())
}

func TestJSON_MarshalUnmarshal(t *testing.T) {
	original := JSON{"ns1.example.com", "ns2.example.com"}

	// Test Value (marshal)
	val, err := original.Value()
	require.NoError(t, err)
	require.NotNil(t, val)

	// The marshaled value should be a valid JSON string
	str, ok := val.(string)
	require.True(t, ok)

	var result []string
	err = json.Unmarshal([]byte(str), &result)
	require.NoError(t, err)
	assert.Equal(t, []string{"ns1.example.com", "ns2.example.com"}, result)
}

func TestJSON_ScanFromBytes(t *testing.T) {
	input := []byte(`["ns1.example.com","ns2.example.com"]`)

	var j JSON
	err := j.Scan(input)
	require.NoError(t, err)
	assert.Equal(t, JSON{"ns1.example.com", "ns2.example.com"}, j)
}

func TestJSON_ScanFromString(t *testing.T) {
	input := `["ns1.example.com","ns2.example.com"]`

	var j JSON
	err := j.Scan(input)
	require.NoError(t, err)
	assert.Equal(t, JSON{"ns1.example.com", "ns2.example.com"}, j)
}

func TestJSON_ScanNil(t *testing.T) {
	var j JSON
	err := j.Scan(nil)
	require.NoError(t, err)
	assert.Nil(t, j)
}

func TestJSON_ValueNil(t *testing.T) {
	var j JSON
	val, err := j.Value()
	require.NoError(t, err)
	assert.Nil(t, val)
}

func TestJSON_GormDataType(t *testing.T) {
	var j JSON
	assert.Equal(t, "text", j.GormDataType())
}

func TestNormalizedDomain_DefaultFields(t *testing.T) {
	now := time.Now()
	expiry := now.Add(90 * 24 * time.Hour)

	d := NormalizedDomain{
		DomainName:     "example.com",
		ExpirationDate: &expiry,
		Nameservers:    JSON{"ns1.example.com", "ns2.example.com"},
	}

	assert.Equal(t, "example.com", d.DomainName)
	assert.Equal(t, 2, len(d.Nameservers))
	assert.NotNil(t, d.ExpirationDate)
}

func TestDefaultDatabaseConfig(t *testing.T) {
	config := DefaultDatabaseConfig()

	assert.Equal(t, 25, config.MaxOpenConns)
	assert.Equal(t, 10, config.MaxIdleConns)
	assert.Equal(t, 5*time.Minute, config.ConnMaxLifetime)
}

func TestNewDatabase_EmptyURL(t *testing.T) {
	db, err := NewDatabase("")
	assert.Nil(t, db)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database URL is required")
}

func TestMaskDSN(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "long DSN is masked",
			input:    "postgres://user:password@localhost:5432/db",
			expected: "postgres://user:pass...",
		},
		{
			name:     "short DSN is fully masked",
			input:    "short",
			expected: "***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskDSN(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
