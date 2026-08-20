package certcheck

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math"
	"net"
	"strings"
	"time"
)

// CertResult holds the result of a TLS certificate check.
type CertResult struct {
	Subject       string
	Issuer        string
	ValidFrom     time.Time
	ValidTo       time.Time
	DaysRemaining int
	SANs          []string
	ChainComplete bool
	SerialNumber  string
	Error         string
}

// CheckEndpoint connects to the given endpoint via TLS and inspects the certificate.
// The endpoint should be in the format "host:port" (e.g. "www.example.com:443").
// timeout specifies the maximum duration for the TCP+TLS handshake.
func CheckEndpoint(endpoint string, timeout time.Duration) (*CertResult, error) {
	// Ensure endpoint has a port
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		// If no port specified, default to 443
		host = endpoint
		port = "443"
		endpoint = net.JoinHostPort(host, port)
	}

	// Establish TLS connection
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", endpoint, &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true, // We want to inspect even invalid certs
	})
	if err != nil {
		return &CertResult{
			Error: fmt.Sprintf("TLS connection failed: %v", err),
		}, nil
	}
	defer conn.Close()

	// Get peer certificates
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return &CertResult{
			Error: "no certificates presented by server",
		}, nil
	}

	// Use the leaf certificate (first in chain)
	leaf := certs[0]

	now := time.Now()
	daysRemaining := int(math.Ceil(leaf.NotAfter.Sub(now).Hours() / 24))

	// Check chain validity
	chainComplete := verifyChain(certs, host)

	// Extract SANs
	sans := extractSANs(leaf)

	return &CertResult{
		Subject:       leaf.Subject.CommonName,
		Issuer:        leaf.Issuer.CommonName,
		ValidFrom:     leaf.NotBefore,
		ValidTo:       leaf.NotAfter,
		DaysRemaining: daysRemaining,
		SANs:          sans,
		ChainComplete: chainComplete,
		SerialNumber:  leaf.SerialNumber.Text(16),
	}, nil
}

// verifyChain attempts to verify the certificate chain.
func verifyChain(certs []*x509.Certificate, host string) bool {
	if len(certs) == 0 {
		return false
	}

	// Build intermediate pool from presented chain
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}

	opts := x509.VerifyOptions{
		Intermediates: intermediates,
		DNSName:       host,
		CurrentTime:   time.Now(),
	}

	_, err := certs[0].Verify(opts)
	return err == nil
}

// extractSANs returns the Subject Alternative Names from a certificate.
func extractSANs(cert *x509.Certificate) []string {
	sans := make([]string, 0, len(cert.DNSNames)+len(cert.IPAddresses))
	sans = append(sans, cert.DNSNames...)
	for _, ip := range cert.IPAddresses {
		sans = append(sans, ip.String())
	}
	// Deduplicate with subject CN
	result := make([]string, 0, len(sans))
	for _, s := range sans {
		if strings.TrimSpace(s) != "" {
			result = append(result, s)
		}
	}
	return result
}
