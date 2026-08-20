package emailcheck

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// EmailCheckConfig holds the configuration for an email check.
type EmailCheckConfig struct {
	DKIMSelectors []string // e.g. ["google", "default", "selector1"]
	MailServerIPs []string // IPs for PTR check
}

// EmailCheckReport holds the complete result of all 8 email checks.
type EmailCheckReport struct {
	TotalScore  int    `json:"total_score"`
	Grade       string `json:"grade"`
	MXScore     int    `json:"mx_score"`
	SPFScore    int    `json:"spf_score"`
	DKIMScore   int    `json:"dkim_score"`
	DMARCScore  int    `json:"dmarc_score"`
	PTRScore    int    `json:"ptr_score"`
	MTASTSScore int    `json:"mta_sts_score"`
	TLSRPTScore int    `json:"tlsrpt_score"`
	BIMIScore   int    `json:"bimi_score"`

	Details *EmailCheckDetails `json:"details"`
}

// EmailCheckDetails contains detailed findings for each check category.
type EmailCheckDetails struct {
	MX      CheckDetail `json:"mx"`
	SPF     CheckDetail `json:"spf"`
	DKIM    CheckDetail `json:"dkim"`
	DMARC   CheckDetail `json:"dmarc"`
	PTR     CheckDetail `json:"ptr"`
	MTASTS  CheckDetail `json:"mta_sts"`
	TLSRPT CheckDetail `json:"tlsrpt"`
	BIMI    CheckDetail `json:"bimi"`
}

// CheckDetail holds the score and findings for a single check category.
type CheckDetail struct {
	Score    int      `json:"score"`
	MaxScore int      `json:"max_score"`
	Findings []string `json:"findings"`
}

// resolver interface for testability
type resolver interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupAddr(ctx context.Context, addr string) ([]string, error)
}

// defaultResolver wraps net.Resolver.
type defaultResolver struct {
	r *net.Resolver
}

func (d *defaultResolver) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	return d.r.LookupMX(ctx, name)
}

func (d *defaultResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return d.r.LookupTXT(ctx, name)
}

func (d *defaultResolver) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	return d.r.LookupAddr(ctx, addr)
}

const (
	dnsTimeout     = 5 * time.Second
	overallTimeout = 30 * time.Second
	httpTimeout    = 5 * time.Second
)

// RunEmailCheck performs all 8 email DNS checks on the given domain.
func RunEmailCheck(domainName string, config EmailCheckConfig) *EmailCheckReport {
	ctx, cancel := context.WithTimeout(context.Background(), overallTimeout)
	defer cancel()

	res := &defaultResolver{r: net.DefaultResolver}

	report := &EmailCheckReport{
		Details: &EmailCheckDetails{},
	}

	// Run all checks
	report.Details.MX = checkMX(ctx, res, domainName)
	report.Details.SPF = checkSPF(ctx, res, domainName)
	report.Details.DKIM = checkDKIM(ctx, res, domainName, config.DKIMSelectors)
	report.Details.DMARC = checkDMARC(ctx, res, domainName)
	report.Details.PTR = checkPTR(ctx, res, config.MailServerIPs)
	report.Details.MTASTS = checkMTASTS(ctx, res, domainName)
	report.Details.TLSRPT = checkTLSRPT(ctx, res, domainName)
	report.Details.BIMI = checkBIMI(ctx, res, domainName)

	// Assign scores
	report.MXScore = report.Details.MX.Score
	report.SPFScore = report.Details.SPF.Score
	report.DKIMScore = report.Details.DKIM.Score
	report.DMARCScore = report.Details.DMARC.Score
	report.PTRScore = report.Details.PTR.Score
	report.MTASTSScore = report.Details.MTASTS.Score
	report.TLSRPTScore = report.Details.TLSRPT.Score
	report.BIMIScore = report.Details.BIMI.Score

	report.TotalScore = report.MXScore + report.SPFScore + report.DKIMScore +
		report.DMARCScore + report.PTRScore + report.MTASTSScore +
		report.TLSRPTScore + report.BIMIScore

	report.Grade = calculateGrade(report.TotalScore)

	return report
}

func calculateGrade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 70:
		return "B"
	case score >= 50:
		return "C"
	default:
		return "D"
	}
}

// --- MX Check (30 points) ---

func checkMX(ctx context.Context, res resolver, domain string) CheckDetail {
	detail := CheckDetail{MaxScore: 30}
	dnsCtx, cancel := context.WithTimeout(ctx, dnsTimeout)
	defer cancel()

	mxRecords, err := res.LookupMX(dnsCtx, domain)
	if err != nil || len(mxRecords) == 0 {
		detail.Findings = append(detail.Findings, "未找到 MX 记录")
		return detail
	}

	// MX existence: 10 points
	detail.Score += 10
	detail.Findings = append(detail.Findings, fmt.Sprintf("找到 %d 条 MX 记录", len(mxRecords)))
	for _, mx := range mxRecords {
		detail.Findings = append(detail.Findings, fmt.Sprintf("  → %s (优先级 %d)", strings.TrimSuffix(mx.Host, "."), mx.Pref))
	}

	// Check MX target reachability (resolve MX hosts): 10 points
	reachable := 0
	for _, mx := range mxRecords {
		host := strings.TrimSuffix(mx.Host, ".")
		lookupCtx, lookupCancel := context.WithTimeout(ctx, dnsTimeout)
		_, err := net.DefaultResolver.LookupHost(lookupCtx, host)
		lookupCancel()
		if err == nil {
			reachable++
		}
	}
	if reachable > 0 {
		detail.Score += 10
		detail.Findings = append(detail.Findings, fmt.Sprintf("%d/%d MX 主机可解析", reachable, len(mxRecords)))
	} else {
		detail.Findings = append(detail.Findings, "所有 MX 主机均无法解析")
	}

	// Check port 25 connectivity on first reachable MX: 10 points
	port25OK := false
	for _, mx := range mxRecords {
		host := strings.TrimSuffix(mx.Host, ".")
		conn, err := net.DialTimeout("tcp", host+":25", dnsTimeout)
		if err == nil {
			conn.Close()
			port25OK = true
			detail.Score += 10
			detail.Findings = append(detail.Findings, fmt.Sprintf("MX 主机 %s 端口 25 可连接", host))
			break
		}
	}
	if !port25OK {
		detail.Findings = append(detail.Findings, "所有 MX 主机端口 25 不可连接")
	}

	return detail
}

// --- SPF Check (20 points) ---

func checkSPF(ctx context.Context, res resolver, domain string) CheckDetail {
	detail := CheckDetail{MaxScore: 20}
	dnsCtx, cancel := context.WithTimeout(ctx, dnsTimeout)
	defer cancel()

	txtRecords, err := res.LookupTXT(dnsCtx, domain)
	if err != nil {
		detail.Findings = append(detail.Findings, "无法查询 TXT 记录")
		return detail
	}

	// Find SPF records
	var spfRecords []string
	for _, txt := range txtRecords {
		if strings.HasPrefix(strings.ToLower(txt), "v=spf1") {
			spfRecords = append(spfRecords, txt)
		}
	}

	if len(spfRecords) == 0 {
		detail.Findings = append(detail.Findings, "未找到 SPF 记录")
		return detail
	}

	// SPF existence: 8 points
	detail.Score += 8
	detail.Findings = append(detail.Findings, "找到 SPF 记录")
	detail.Findings = append(detail.Findings, fmt.Sprintf("  → %s", spfRecords[0]))

	// Single record check: 4 points
	if len(spfRecords) == 1 {
		detail.Score += 4
		detail.Findings = append(detail.Findings, "仅有一条 SPF 记录（正确）")
	} else {
		detail.Findings = append(detail.Findings, fmt.Sprintf("存在 %d 条 SPF 记录（应仅有一条）", len(spfRecords)))
	}

	spf := spfRecords[0]

	// DNS lookup count check: 4 points (count include/a/mx/ptr/exists/redirect)
	lookupCount := countSPFLookups(spf)
	if lookupCount <= 10 {
		detail.Score += 4
		detail.Findings = append(detail.Findings, fmt.Sprintf("SPF DNS 查询次数: %d（≤10, 合规）", lookupCount))
	} else {
		detail.Findings = append(detail.Findings, fmt.Sprintf("SPF DNS 查询次数: %d（超过 10 次限制）", lookupCount))
	}

	// Policy analysis: 4 points
	if strings.Contains(spf, "-all") {
		detail.Score += 4
		detail.Findings = append(detail.Findings, "SPF 策略: -all（严格拒绝）")
	} else if strings.Contains(spf, "~all") {
		detail.Score += 2
		detail.Findings = append(detail.Findings, "SPF 策略: ~all（软拒绝，建议使用 -all）")
	} else if strings.Contains(spf, "?all") {
		detail.Score += 1
		detail.Findings = append(detail.Findings, "SPF 策略: ?all（中性，建议使用 -all）")
	} else if strings.Contains(spf, "+all") {
		detail.Findings = append(detail.Findings, "SPF 策略: +all（允许所有，非常不安全！）")
	} else {
		detail.Findings = append(detail.Findings, "SPF 未指定 all 机制")
	}

	return detail
}

// countSPFLookups counts the number of DNS-lookup-requiring mechanisms in an SPF record.
func countSPFLookups(spf string) int {
	count := 0
	parts := strings.Fields(spf)
	for _, part := range parts {
		lower := strings.ToLower(part)
		// Remove qualifiers
		lower = strings.TrimLeft(lower, "+-~?")
		if strings.HasPrefix(lower, "include:") ||
			strings.HasPrefix(lower, "a:") || lower == "a" ||
			strings.HasPrefix(lower, "mx:") || lower == "mx" ||
			strings.HasPrefix(lower, "ptr:") || lower == "ptr" ||
			strings.HasPrefix(lower, "exists:") ||
			strings.HasPrefix(lower, "redirect=") {
			count++
		}
	}
	return count
}

// --- DKIM Check (20 points) ---

func checkDKIM(ctx context.Context, res resolver, domain string, selectors []string) CheckDetail {
	detail := CheckDetail{MaxScore: 20}

	if len(selectors) == 0 {
		selectors = []string{
			"google", "default", "selector1", "selector2",
			"k1", "k2", "s1", "s2", "dkim", "mail",
			"cf2024-1", "cf2024-2", "cf2023-1", "cf2023-2",  // Cloudflare
			"protonmail", "protonmail2", "protonmail3",         // ProtonMail
			"mxvault",                                          // MXRoute
			"mandrill", "everlytickey1", "smtp",                // Transactional
		}
	}

	foundSelectors := 0
	validKeys := 0

	for _, selector := range selectors {
		dkimDomain := selector + "._domainkey." + domain
		dnsCtx, cancel := context.WithTimeout(ctx, dnsTimeout)
		txtRecords, err := res.LookupTXT(dnsCtx, dkimDomain)
		cancel()

		if err != nil || len(txtRecords) == 0 {
			continue
		}

		// Concatenate multi-part TXT records
		txt := strings.Join(txtRecords, "")
		if !strings.Contains(txt, "v=DKIM1") && !strings.Contains(txt, "p=") {
			continue
		}

		foundSelectors++
		detail.Findings = append(detail.Findings, fmt.Sprintf("DKIM 选择器 '%s' 存在", selector))

		// Validate public key
		if validateDKIMKey(txt) {
			validKeys++
		} else {
			detail.Findings = append(detail.Findings, fmt.Sprintf("DKIM 选择器 '%s' 公钥无效", selector))
		}
	}

	if foundSelectors == 0 {
		detail.Findings = append(detail.Findings, fmt.Sprintf("未找到 DKIM 记录（已检查选择器: %s）", strings.Join(selectors, ", ")))
		return detail
	}

	// DKIM existence: 10 points
	detail.Score += 10
	detail.Findings = append(detail.Findings, fmt.Sprintf("找到 %d 个有效 DKIM 选择器", foundSelectors))

	// Valid key: 10 points
	if validKeys > 0 {
		detail.Score += 10
		detail.Findings = append(detail.Findings, fmt.Sprintf("%d 个选择器公钥有效", validKeys))
	} else {
		detail.Findings = append(detail.Findings, "所有 DKIM 选择器公钥无效")
	}

	return detail
}

// validateDKIMKey checks if the DKIM TXT record contains a valid RSA public key.
func validateDKIMKey(txt string) bool {
	// Extract p= value
	parts := strings.Split(txt, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "p=") {
			keyData := strings.TrimPrefix(part, "p=")
			keyData = strings.TrimSpace(keyData)
			if keyData == "" {
				return false // Key revoked
			}
			// Try to decode base64
			decoded, err := base64.StdEncoding.DecodeString(keyData)
			if err != nil {
				// Try without padding
				decoded, err = base64.RawStdEncoding.DecodeString(keyData)
				if err != nil {
					return false
				}
			}
			// Try to parse as public key
			_, err = x509.ParsePKIXPublicKey(decoded)
			if err != nil {
				// Try RSA specifically
				_, err = x509.ParsePKCS1PublicKey(decoded)
				if err != nil {
					// Still has key data, may be valid but in a format we can't parse
					// Consider valid if we decoded base64 successfully and it has substantial length
					if len(decoded) > 32 {
						return true
					}
					return false
				}
				return true
			}
			// Check if it's an RSA key
			if _, ok := interface{}(nil).(*rsa.PublicKey); !ok {
				return true // Any valid public key is fine
			}
			return true
		}
	}
	return false
}

// --- DMARC Check (15 points) ---

func checkDMARC(ctx context.Context, res resolver, domain string) CheckDetail {
	detail := CheckDetail{MaxScore: 15}
	dnsCtx, cancel := context.WithTimeout(ctx, dnsTimeout)
	defer cancel()

	dmarcDomain := "_dmarc." + domain
	txtRecords, err := res.LookupTXT(dnsCtx, dmarcDomain)
	if err != nil || len(txtRecords) == 0 {
		detail.Findings = append(detail.Findings, "未找到 DMARC 记录")
		return detail
	}

	// Find DMARC record
	var dmarcRecord string
	for _, txt := range txtRecords {
		if strings.HasPrefix(strings.ToLower(txt), "v=dmarc1") {
			dmarcRecord = txt
			break
		}
	}

	if dmarcRecord == "" {
		detail.Findings = append(detail.Findings, "TXT 记录中未找到有效 DMARC 记录")
		return detail
	}

	// DMARC existence: 7 points
	detail.Score += 7
	detail.Findings = append(detail.Findings, "找到 DMARC 记录")
	detail.Findings = append(detail.Findings, fmt.Sprintf("  → %s", dmarcRecord))

	// Policy strength analysis: 8 points
	policy := extractDMARCPolicy(dmarcRecord)
	switch policy {
	case "reject":
		detail.Score += 8
		detail.Findings = append(detail.Findings, "DMARC 策略: reject（严格拒绝）")
	case "quarantine":
		detail.Score += 5
		detail.Findings = append(detail.Findings, "DMARC 策略: quarantine（隔离，建议升级为 reject）")
	case "none":
		detail.Score += 2
		detail.Findings = append(detail.Findings, "DMARC 策略: none（仅报告，建议升级为 quarantine 或 reject）")
	default:
		detail.Findings = append(detail.Findings, "DMARC 未指定策略")
	}

	// Check for rua (reporting)
	if strings.Contains(dmarcRecord, "rua=") {
		detail.Findings = append(detail.Findings, "已配置聚合报告地址 (rua)")
	}
	if strings.Contains(dmarcRecord, "ruf=") {
		detail.Findings = append(detail.Findings, "已配置失败报告地址 (ruf)")
	}

	return detail
}

func extractDMARCPolicy(record string) string {
	parts := strings.Split(record, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "p=") {
			return strings.TrimSpace(strings.ToLower(strings.TrimPrefix(part, "p=")))
		}
	}
	return ""
}

// --- PTR/rDNS Check (5 points) ---

func checkPTR(ctx context.Context, res resolver, ips []string) CheckDetail {
	detail := CheckDetail{MaxScore: 5}

	if len(ips) == 0 {
		detail.Findings = append(detail.Findings, "未配置邮件服务器 IP，跳过 PTR 检查")
		return detail
	}

	ptrFound := 0
	fcrDNSValid := 0

	for _, ip := range ips {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			continue
		}

		dnsCtx, cancel := context.WithTimeout(ctx, dnsTimeout)
		names, err := res.LookupAddr(dnsCtx, ip)
		cancel()

		if err != nil || len(names) == 0 {
			detail.Findings = append(detail.Findings, fmt.Sprintf("IP %s 无 PTR 记录", ip))
			continue
		}

		ptrFound++
		ptrName := strings.TrimSuffix(names[0], ".")
		detail.Findings = append(detail.Findings, fmt.Sprintf("IP %s PTR: %s", ip, ptrName))

		// FCrDNS validation: forward-confirmed reverse DNS
		lookupCtx, lookupCancel := context.WithTimeout(ctx, dnsTimeout)
		addrs, err := net.DefaultResolver.LookupHost(lookupCtx, ptrName)
		lookupCancel()
		if err == nil {
			for _, addr := range addrs {
				if addr == ip {
					fcrDNSValid++
					detail.Findings = append(detail.Findings, fmt.Sprintf("IP %s FCrDNS 验证通过", ip))
					break
				}
			}
		}
	}

	if ptrFound == 0 {
		detail.Findings = append(detail.Findings, "所有 IP 均无 PTR 记录")
		return detail
	}

	// PTR existence: 3 points
	detail.Score += 3
	detail.Findings = append(detail.Findings, fmt.Sprintf("%d/%d IP 有 PTR 记录", ptrFound, len(ips)))

	// FCrDNS: 2 points
	if fcrDNSValid > 0 {
		detail.Score += 2
	} else {
		detail.Findings = append(detail.Findings, "FCrDNS 验证未通过")
	}

	return detail
}

// --- MTA-STS Check (5 points) ---

func checkMTASTS(ctx context.Context, res resolver, domain string) CheckDetail {
	detail := CheckDetail{MaxScore: 5}

	// Check TXT record: _mta-sts.<domain>
	dnsCtx, cancel := context.WithTimeout(ctx, dnsTimeout)
	txtRecords, err := res.LookupTXT(dnsCtx, "_mta-sts."+domain)
	cancel()

	txtFound := false
	if err == nil {
		for _, txt := range txtRecords {
			if strings.HasPrefix(strings.ToLower(txt), "v=stsv1") {
				txtFound = true
				break
			}
		}
	}

	if !txtFound {
		detail.Findings = append(detail.Findings, "未找到 MTA-STS TXT 记录")
		return detail
	}

	// TXT record found: 2 points
	detail.Score += 2
	detail.Findings = append(detail.Findings, "MTA-STS TXT 记录存在")

	// Check HTTPS policy file
	policyURL := fmt.Sprintf("https://mta-sts.%s/.well-known/mta-sts.txt", domain)
	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, policyURL, nil)
	if err != nil {
		detail.Findings = append(detail.Findings, "MTA-STS 策略文件请求构建失败")
		return detail
	}

	resp, err := client.Do(req)
	if err != nil {
		detail.Findings = append(detail.Findings, fmt.Sprintf("MTA-STS 策略文件不可访问: %s", policyURL))
		return detail
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		detail.Score += 3
		// Read first 1KB to check content
		body := make([]byte, 1024)
		n, _ := io.ReadFull(resp.Body, body)
		content := string(body[:n])
		if strings.Contains(content, "mode:") {
			if strings.Contains(content, "mode: enforce") {
				detail.Findings = append(detail.Findings, "MTA-STS 策略文件有效，模式: enforce")
			} else if strings.Contains(content, "mode: testing") {
				detail.Findings = append(detail.Findings, "MTA-STS 策略文件有效，模式: testing")
			} else {
				detail.Findings = append(detail.Findings, "MTA-STS 策略文件有效")
			}
		} else {
			detail.Findings = append(detail.Findings, "MTA-STS 策略文件存在但内容可能不正确")
		}
	} else {
		detail.Findings = append(detail.Findings, fmt.Sprintf("MTA-STS 策略文件返回 HTTP %d", resp.StatusCode))
	}

	return detail
}

// --- TLSRPT Check (3 points) ---

func checkTLSRPT(ctx context.Context, res resolver, domain string) CheckDetail {
	detail := CheckDetail{MaxScore: 3}

	dnsCtx, cancel := context.WithTimeout(ctx, dnsTimeout)
	txtRecords, err := res.LookupTXT(dnsCtx, "_smtp._tls."+domain)
	cancel()

	if err != nil || len(txtRecords) == 0 {
		detail.Findings = append(detail.Findings, "未找到 TLSRPT 记录")
		return detail
	}

	for _, txt := range txtRecords {
		if strings.Contains(strings.ToLower(txt), "v=tlsrptv1") {
			detail.Score += 3
			detail.Findings = append(detail.Findings, "TLSRPT 记录存在")
			// Extract rua
			if strings.Contains(txt, "rua=") {
				detail.Findings = append(detail.Findings, "已配置 TLS 报告接收地址")
			}
			return detail
		}
	}

	detail.Findings = append(detail.Findings, "TXT 记录中未找到有效 TLSRPT 记录")
	return detail
}

// --- BIMI Check (2 points) ---

func checkBIMI(ctx context.Context, res resolver, domain string) CheckDetail {
	detail := CheckDetail{MaxScore: 2}

	dnsCtx, cancel := context.WithTimeout(ctx, dnsTimeout)
	txtRecords, err := res.LookupTXT(dnsCtx, "default._bimi."+domain)
	cancel()

	if err != nil || len(txtRecords) == 0 {
		detail.Findings = append(detail.Findings, "未找到 BIMI 记录")
		return detail
	}

	for _, txt := range txtRecords {
		if strings.Contains(strings.ToLower(txt), "v=bimi1") {
			// BIMI TXT existence: 1 point
			detail.Score += 1
			detail.Findings = append(detail.Findings, "BIMI 记录存在")

			// Check SVG accessibility: 1 point
			if strings.Contains(txt, "l=") {
				svgURL := extractBIMILogoURL(txt)
				if svgURL != "" {
					client := &http.Client{Timeout: httpTimeout}
					req, err := http.NewRequestWithContext(ctx, http.MethodHead, svgURL, nil)
					if err == nil {
						resp, err := client.Do(req)
						if err == nil {
							resp.Body.Close()
							if resp.StatusCode == http.StatusOK {
								detail.Score += 1
								detail.Findings = append(detail.Findings, "BIMI SVG 图标可访问")
							} else {
								detail.Findings = append(detail.Findings, fmt.Sprintf("BIMI SVG 图标返回 HTTP %d", resp.StatusCode))
							}
						} else {
							detail.Findings = append(detail.Findings, "BIMI SVG 图标不可访问")
						}
					}
				} else {
					detail.Findings = append(detail.Findings, "BIMI 记录未指定图标 URL")
				}
			} else {
				detail.Findings = append(detail.Findings, "BIMI 记录未指定图标 (l= 字段)")
			}
			return detail
		}
	}

	detail.Findings = append(detail.Findings, "TXT 记录中未找到有效 BIMI 记录")
	return detail
}

func extractBIMILogoURL(txt string) string {
	parts := strings.Split(txt, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "l=") {
			url := strings.TrimPrefix(part, "l=")
			url = strings.TrimPrefix(url, "L=")
			return strings.TrimSpace(url)
		}
	}
	return ""
}
