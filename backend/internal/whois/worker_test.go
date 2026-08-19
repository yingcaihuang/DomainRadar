package whois

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestQueryDomain_Success(t *testing.T) {
	// Set up a mock who-dat server that returns a valid RDAP-style response.
	expDate := "2025-12-31T00:00:00Z"
	creDate := "2020-01-15T00:00:00Z"

	mockResponse := map[string]interface{}{
		"events": []map[string]interface{}{
			{"eventAction": "expiration", "eventDate": expDate},
			{"eventAction": "registration", "eventDate": creDate},
		},
		"entities": []map[string]interface{}{
			{
				"roles":  []string{"registrar"},
				"handle": "GoDaddy LLC",
			},
		},
		"nameservers": []map[string]interface{}{
			{"ldhName": "ns1.example.com", "objectClassName": "nameserver"},
			{"ldhName": "ns2.example.com", "objectClassName": "nameserver"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/example.com", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	worker := NewWHOISWorker(nil, nil, server.URL, zap.NewNop())

	result, err := worker.QueryDomain(context.Background(), "example.com")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify expiration date.
	expectedExp, _ := time.Parse(time.RFC3339, expDate)
	assert.Equal(t, expectedExp.UTC(), result.ExpirationDate.UTC())

	// Verify creation date.
	expectedCre, _ := time.Parse(time.RFC3339, creDate)
	assert.Equal(t, expectedCre.UTC(), result.CreationDate.UTC())

	// Verify registrar.
	assert.Equal(t, "GoDaddy LLC", result.Registrar)

	// Verify nameservers.
	assert.Equal(t, []string{"ns1.example.com", "ns2.example.com"}, result.Nameservers)

	// Verify raw response is captured.
	assert.NotNil(t, result.RawResponse)
}

func TestQueryDomain_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer server.Close()

	worker := NewWHOISWorker(nil, nil, server.URL, zap.NewNop())

	result, err := worker.QueryDomain(context.Background(), "example.com")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "429")
}

func TestQueryDomain_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	worker := NewWHOISWorker(nil, nil, server.URL, zap.NewNop())

	result, err := worker.QueryDomain(context.Background(), "example.com")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "parsing who-dat response")
}

func TestQueryDomain_NetworkError(t *testing.T) {
	// Use a URL that won't connect.
	worker := NewWHOISWorker(nil, nil, "http://127.0.0.1:1", zap.NewNop())
	worker.httpClient.Timeout = 1 * time.Second

	result, err := worker.QueryDomain(context.Background(), "example.com")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "calling who-dat API")
}

func TestQueryDomain_PartialResponse(t *testing.T) {
	// Response with only expiration date, no registrar or nameservers.
	mockResponse := map[string]interface{}{
		"events": []map[string]interface{}{
			{"eventAction": "expiration", "eventDate": "2025-06-15T00:00:00Z"},
		},
		"entities":    []interface{}{},
		"nameservers": []interface{}{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	worker := NewWHOISWorker(nil, nil, server.URL, zap.NewNop())

	result, err := worker.QueryDomain(context.Background(), "example.com")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.NotNil(t, result.ExpirationDate)
	assert.Nil(t, result.CreationDate)
	assert.Empty(t, result.Registrar)
	assert.Empty(t, result.Nameservers)
}

func TestCalculateQueryInterval(t *testing.T) {
	worker := NewWHOISWorker(nil, nil, "", zap.NewNop())

	tests := []struct {
		name     string
		expires  time.Time
		expected time.Duration
	}{
		{
			name:     "more than 90 days - weekly",
			expires:  time.Now().Add(100 * 24 * time.Hour),
			expected: 7 * 24 * time.Hour,
		},
		{
			name:     "exactly 91 days - weekly",
			expires:  time.Now().Add(91 * 24 * time.Hour),
			expected: 7 * 24 * time.Hour,
		},
		{
			name:     "60 days - daily",
			expires:  time.Now().Add(60 * 24 * time.Hour),
			expected: 24 * time.Hour,
		},
		{
			name:     "31 days - daily",
			expires:  time.Now().Add(31 * 24 * time.Hour),
			expected: 24 * time.Hour,
		},
		{
			name:     "30 days - every 12 hours",
			expires:  time.Now().Add(30 * 24 * time.Hour),
			expected: 12 * time.Hour,
		},
		{
			name:     "15 days - every 12 hours",
			expires:  time.Now().Add(15 * 24 * time.Hour),
			expected: 12 * time.Hour,
		},
		{
			name:     "1 day - every 12 hours",
			expires:  time.Now().Add(1 * 24 * time.Hour),
			expected: 12 * time.Hour,
		},
		{
			name:     "already expired - every 12 hours",
			expires:  time.Now().Add(-5 * 24 * time.Hour),
			expected: 12 * time.Hour,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			interval := worker.CalculateQueryInterval(tc.expires)
			assert.Equal(t, tc.expected, interval)
		})
	}
}

func TestCalculateQueryInterval_BoundaryAt90Days(t *testing.T) {
	worker := NewWHOISWorker(nil, nil, "", zap.NewNop())

	// Just over 90 days should be weekly.
	justOver90 := time.Now().Add(90*24*time.Hour + 1*time.Hour)
	assert.Equal(t, 7*24*time.Hour, worker.CalculateQueryInterval(justOver90))

	// Just under 90 days should be daily.
	justUnder90 := time.Now().Add(89 * 24 * time.Hour)
	assert.Equal(t, 24*time.Hour, worker.CalculateQueryInterval(justUnder90))
}

func TestCalculateQueryInterval_BoundaryAt30Days(t *testing.T) {
	worker := NewWHOISWorker(nil, nil, "", zap.NewNop())

	// Just over 30 days should be daily.
	justOver30 := time.Now().Add(30*24*time.Hour + 1*time.Hour)
	assert.Equal(t, 24*time.Hour, worker.CalculateQueryInterval(justOver30))

	// Just under 30 days should be every 12 hours.
	justUnder30 := time.Now().Add(29 * 24 * time.Hour)
	assert.Equal(t, 12*time.Hour, worker.CalculateQueryInterval(justUnder30))
}

func TestNewWHOISWorker_Defaults(t *testing.T) {
	worker := NewWHOISWorker(nil, nil, "", nil)

	assert.Equal(t, DefaultWhoDatURL, worker.whoDatURL)
	assert.NotNil(t, worker.rateLimiter)
	assert.NotNil(t, worker.httpClient)
	assert.NotNil(t, worker.logger)
}

func TestNewWHOISWorker_CustomURL(t *testing.T) {
	worker := NewWHOISWorker(nil, nil, "http://custom:9090", zap.NewNop())
	assert.Equal(t, "http://custom:9090", worker.whoDatURL)
}

func TestParseWhoDatResponse_MultipleEntities(t *testing.T) {
	// Response with multiple entities, only one has the registrar role.
	response := `{
		"events": [
			{"eventAction": "expiration", "eventDate": "2026-03-01T00:00:00Z"},
			{"eventAction": "registration", "eventDate": "2018-03-01T00:00:00Z"},
			{"eventAction": "last changed", "eventDate": "2024-01-15T00:00:00Z"}
		],
		"entities": [
			{"roles": ["technical"], "handle": "Tech Contact"},
			{"roles": ["registrar"], "handle": "Cloudflare Inc"}
		],
		"nameservers": [
			{"ldhName": "ns1.cloudflare.com", "objectClassName": "nameserver"},
			{"ldhName": "ns2.cloudflare.com", "objectClassName": "nameserver"},
			{"ldhName": "ns3.cloudflare.com", "objectClassName": "nameserver"}
		]
	}`

	result, err := parseWhoDatResponse([]byte(response))
	require.NoError(t, err)

	assert.Equal(t, "Cloudflare Inc", result.Registrar)
	assert.Len(t, result.Nameservers, 3)

	expectedExp, _ := time.Parse(time.RFC3339, "2026-03-01T00:00:00Z")
	assert.Equal(t, expectedExp.UTC(), result.ExpirationDate.UTC())

	expectedCre, _ := time.Parse(time.RFC3339, "2018-03-01T00:00:00Z")
	assert.Equal(t, expectedCre.UTC(), result.CreationDate.UTC())
}

func TestParseWhoDatResponse_EmptyResponse(t *testing.T) {
	result, err := parseWhoDatResponse([]byte(`{}`))
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Nil(t, result.ExpirationDate)
	assert.Nil(t, result.CreationDate)
	assert.Empty(t, result.Registrar)
	assert.Empty(t, result.Nameservers)
}

func TestParseEventDate_Formats(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"2025-12-31T00:00:00Z", true},
		{"2025-12-31T00:00:00+08:00", true},
		{"2025-12-31 00:00:00", true},
		{"2025-12-31", true},
		{"not-a-date", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			_, err := parseEventDate(tc.input)
			if tc.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
