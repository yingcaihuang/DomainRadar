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

// ChainCert represents one certificate in the chain.
type ChainCert struct {
	Subject      string    `json:"subject"`
	Issuer       string    `json:"issuer"`
	ValidFrom    time.Time `json:"valid_from"`
	ValidTo      time.Time `json:"valid_to"`
	SerialNumber string    `json:"serial_number"`
	IsCA         bool      `json:"is_ca"`
	SANs         []string  `json:"sans,omitempty"`
}

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
	Chain         []ChainCert
	Error         string

	// Connection details
	ConnectedIP   string        // The IP address connected to
	SNI           string        // The SNI hostname sent
	DNSResolveMs  int64         // DNS resolution time in ms
	HandshakeMs   int64         // TLS handshake time in ms
	TotalMs       int64         // Total connection time in ms
	TLSVersion    string        // TLS version negotiated
	CipherSuite   string        // Cipher suite negotiated
}

// CheckEndpoint connects to the given endpoint via TLS and inspects the certificate.
func CheckEndpoint(endpoint string, timeout time.Duration) (*CertResult, error) {
	totalStart := time.Now()

	// Parse host and port
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		host = endpoint
		port = "443"
		endpoint = net.JoinHostPort(host, port)
	}

	result := &CertResult{SNI: host}

	// Step 1: DNS Resolution
	dnsStart := time.Now()
	ips, err := net.LookupHost(host)
	result.DNSResolveMs = time.Since(dnsStart).Milliseconds()

	if err != nil {
		result.Error = fmt.Sprintf("DNS resolution failed: %v", err)
		result.TotalMs = time.Since(totalStart).Milliseconds()
		return result, nil
	}

	if len(ips) > 0 {
		result.ConnectedIP = ips[0]
	}

	// Step 2: TCP + TLS connection
	handshakeStart := time.Now()
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(result.ConnectedIP, port), &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true,
	})
	result.HandshakeMs = time.Since(handshakeStart).Milliseconds()
	result.TotalMs = time.Since(totalStart).Milliseconds()

	if err != nil {
		result.Error = fmt.Sprintf("TLS connection failed: %v", err)
		return result, nil
	}
	defer conn.Close()

	// Connection state
	state := conn.ConnectionState()
	result.TLSVersion = tlsVersionString(state.Version)
	result.CipherSuite = tls.CipherSuiteName(state.CipherSuite)

	// Get peer certificates
	certs := state.PeerCertificates
	if len(certs) == 0 {
		result.Error = "no certificates presented by server"
		return result, nil
	}

	// Leaf certificate
	leaf := certs[0]
	now := time.Now()

	result.Subject = leaf.Subject.CommonName
	result.Issuer = leaf.Issuer.CommonName
	result.ValidFrom = leaf.NotBefore
	result.ValidTo = leaf.NotAfter
	result.DaysRemaining = int(math.Ceil(leaf.NotAfter.Sub(now).Hours() / 24))
	result.SANs = extractSANs(leaf)
	result.SerialNumber = leaf.SerialNumber.Text(16)
	result.ChainComplete = verifyChain(certs, host)

	// Build chain details
	result.Chain = make([]ChainCert, 0, len(certs))
	for _, cert := range certs {
		cc := ChainCert{
			Subject:      cert.Subject.CommonName,
			Issuer:       cert.Issuer.CommonName,
			ValidFrom:    cert.NotBefore,
			ValidTo:      cert.NotAfter,
			SerialNumber: cert.SerialNumber.Text(16),
			IsCA:         cert.IsCA,
		}
		if !cert.IsCA {
			cc.SANs = extractSANs(cert)
		}
		result.Chain = append(result.Chain, cc)
	}

	return result, nil
}

// verifyChain attempts to verify the certificate chain.
func verifyChain(certs []*x509.Certificate, host string) bool {
	if len(certs) == 0 {
		return false
	}

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
	result := make([]string, 0, len(sans))
	for _, s := range sans {
		if strings.TrimSpace(s) != "" {
			result = append(result, s)
		}
	}
	return result
}

// tlsVersionString returns a human-readable TLS version string.
func tlsVersionString(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("0x%04x", version)
	}
}
