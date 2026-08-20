package domain

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// NormalizedDomain is the unified domain representation across all registrars.
type NormalizedDomain struct {
	ID                     uint           `gorm:"primaryKey" json:"id"`
	DomainName             string         `gorm:"uniqueIndex;size:253;not null" json:"domain_name"`
	RegistrarAccountID     *uint          `gorm:"index" json:"registrar_account_id"`
	RegistrarIdentifier    string         `gorm:"size:100" json:"registrar_identifier"`
	CreationDate           *time.Time     `json:"creation_date"`
	ExpirationDate         *time.Time     `gorm:"index" json:"expiration_date"`
	AutoRenew              bool           `json:"auto_renew"`
	RenewalDeadline        *time.Time     `json:"renewal_deadline"`
	Status                 string         `gorm:"size:50;default:'active'" json:"status"`
	Nameservers            JSON           `gorm:"type:text" json:"nameservers"`
	PrivacyProtection      bool           `json:"privacy_protection"`
	LockStatus             bool           `json:"lock_status"`
	DataSourceType         string         `gorm:"size:20" json:"data_source_type"` // "api", "whois", "manual"
	LastSyncAt             *time.Time     `json:"last_sync_at"`
	GroupID                *uint          `gorm:"index" json:"group_id"`
	Notes                  string         `gorm:"type:text" json:"notes"`
	WebsiteURL             string         `gorm:"size:2048" json:"website_url"`
	EmailEnabled           bool           `json:"email_enabled"`
	HealthScore            int            `gorm:"default:100" json:"health_score"`
	CheckIntervalMinutes   int            `gorm:"default:5" json:"check_interval_minutes"`
	ResponseTimeThresholdMs int           `gorm:"default:10000" json:"response_time_threshold_ms"`
	AbsenceCount           int            `gorm:"default:0" json:"absence_count"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`

	// Relations
	Tags             []Tag             `gorm:"many2many:domain_tags" json:"tags,omitempty"`
	Group            *Group            `gorm:"foreignKey:GroupID" json:"group,omitempty"`
	RegistrarAccount *RegistrarAccount `gorm:"foreignKey:RegistrarAccountID" json:"registrar_account,omitempty"`
}

// TableName overrides the default table name.
func (NormalizedDomain) TableName() string {
	return "domains"
}

// RegistrarConfig defines a supported registrar type.
type RegistrarConfig struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	RegistrarType string    `gorm:"size:50;not null" json:"registrar_type"`
	DisplayName   string    `gorm:"size:100;not null" json:"display_name"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// RegistrarAccount holds encrypted credentials for one registrar account.
type RegistrarAccount struct {
	ID                   uint       `gorm:"primaryKey" json:"id"`
	RegistrarConfigID    uint       `gorm:"index;not null" json:"registrar_config_id"`
	AccountName          string     `gorm:"size:100;not null" json:"account_name"`
	CredentialsEncrypted string     `gorm:"type:text" json:"-"`
	Status               string     `gorm:"size:20;default:'disconnected'" json:"status"` // "connected", "disconnected", "error"
	SyncIntervalHours    int        `gorm:"default:24" json:"sync_interval_hours"`
	LastSyncAt           *time.Time `json:"last_sync_at"`
	DomainCount          int        `gorm:"default:0" json:"domain_count"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`

	// Relations
	RegistrarConfig RegistrarConfig `gorm:"foreignKey:RegistrarConfigID" json:"registrar_config,omitempty"`
}

// Group represents a hierarchical domain grouping.
type Group struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:100;not null" json:"name"`
	ParentID  *uint     `gorm:"index" json:"parent_id"`
	Level     int       `json:"level"`
	CreatedAt time.Time `json:"created_at"`

	// Self-referencing relation
	Parent   *Group  `gorm:"foreignKey:ParentID" json:"parent,omitempty"`
	Children []Group `gorm:"foreignKey:ParentID" json:"children,omitempty"`
}

// Tag represents a label that can be applied to domains.
type Tag struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;size:50;not null" json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// HealthCheck records the result of a website availability check.
type HealthCheck struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	DomainID        uint       `gorm:"index;not null" json:"domain_id"`
	HTTPStatusCode  int        `json:"http_status_code"`
	ResponseTimeMs  int        `json:"response_time_ms"`
	SSLValid        bool       `json:"ssl_valid"`
	SSLExpiry       *time.Time `json:"ssl_expiry"`
	RedirectChain   string     `gorm:"type:text" json:"redirect_chain"`
	CheckType       string     `gorm:"size:20" json:"check_type"`
	FailureCategory string     `gorm:"size:30" json:"failure_category"` // "dns", "connectivity", "http_error"
	FailureDetail   string     `gorm:"type:text" json:"failure_detail"`
	CheckedAt       time.Time  `gorm:"not null" json:"checked_at"`

	// Relations
	Domain NormalizedDomain `gorm:"foreignKey:DomainID" json:"domain,omitempty"`
}

// CertificateMonitor tracks endpoints to monitor for SSL/TLS certificates.
type CertificateMonitor struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	DomainID  uint      `gorm:"index;not null" json:"domain_id"`
	Endpoint  string    `gorm:"size:255;not null" json:"endpoint"` // e.g. "www.example.com:443"
	Label     string    `gorm:"size:100" json:"label"`             // e.g. "主站", "API"
	Enabled       bool       `gorm:"default:true" json:"enabled"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
	NextCheckAt   *time.Time `json:"next_check_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	// Relations
	Domain NormalizedDomain `gorm:"foreignKey:DomainID" json:"domain,omitempty"`
}

// CertificateCheck records SSL/TLS certificate inspection results.
type CertificateCheck struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	DomainID      uint      `gorm:"index;not null" json:"domain_id"`
	MonitorID     uint      `gorm:"index" json:"monitor_id"`
	Issuer        string    `gorm:"size:255" json:"issuer"`
	Subject       string    `gorm:"size:255" json:"subject"`
	ValidFrom     time.Time `json:"valid_from"`
	ValidTo       time.Time `json:"valid_to"`
	ChainComplete bool      `json:"chain_complete"`
	SANs          string    `gorm:"type:text" json:"sans"`
	SerialNumber  string    `gorm:"size:100" json:"serial_number"`
	DaysRemaining int       `json:"days_remaining"`
	Error         string    `gorm:"type:text" json:"error"`
	Chain         string    `gorm:"type:text" json:"chain"`        // JSON array of chain certs
	ConnectedIP   string    `gorm:"size:50" json:"connected_ip"`
	SNI           string    `gorm:"size:255" json:"sni"`
	DNSResolveMs  int64     `json:"dns_resolve_ms"`
	HandshakeMs   int64     `json:"handshake_ms"`
	TotalMs       int64     `json:"total_ms"`
	TLSVersion    string    `gorm:"size:20" json:"tls_version"`
	CipherSuite   string    `gorm:"size:100" json:"cipher_suite"`
	CheckedAt     time.Time `gorm:"not null" json:"checked_at"`

	// Relations
	Domain  NormalizedDomain    `gorm:"foreignKey:DomainID" json:"domain,omitempty"`
	Monitor *CertificateMonitor `gorm:"foreignKey:MonitorID" json:"monitor,omitempty"`
}

// EmailCheck records email service compliance check results.
type EmailCheck struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	DomainID        uint      `gorm:"index;not null" json:"domain_id"`
	MXRecords       string    `gorm:"type:text" json:"mx_records"`
	SPFValid        bool      `json:"spf_valid"`
	DKIMValid       bool      `json:"dkim_valid"`
	DMARCValid      bool      `json:"dmarc_valid"`
	ComplianceScore int       `json:"compliance_score"`
	MXPrevious      string    `gorm:"type:text" json:"mx_previous"`
	MXChanged       bool      `json:"mx_changed"`
	CheckedAt       time.Time `gorm:"not null" json:"checked_at"`

	// Relations
	Domain NormalizedDomain `gorm:"foreignKey:DomainID" json:"domain,omitempty"`
}

// Alert represents a generated alert for a domain.
type Alert struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	DomainID       uint       `gorm:"index;not null" json:"domain_id"`
	AlertType      string     `gorm:"size:50;not null" json:"alert_type"` // "expiration", "certificate", "downtime", "email", "dns"
	Severity       string     `gorm:"size:20;not null" json:"severity"`   // "informational", "warning", "critical", "expired"
	Message        string     `gorm:"type:text" json:"message"`
	DaysRemaining  *int       `json:"days_remaining"`
	Acknowledged   bool       `gorm:"default:false" json:"acknowledged"`
	DeliveryStatus string     `gorm:"size:20;default:'pending'" json:"delivery_status"` // "pending", "delivered", "failed", "undelivered"
	GeneratedAt    time.Time  `gorm:"index;not null" json:"generated_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at"`

	// Relations
	Domain NormalizedDomain `gorm:"foreignKey:DomainID" json:"domain,omitempty"`
}

// NotificationChannel stores configuration for a notification delivery channel.
type NotificationChannel struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	ChannelType     string     `gorm:"size:30;not null" json:"channel_type"` // "email", "wechat_work", "sms", "webhook"
	Name            string     `gorm:"size:100;not null" json:"name"`
	ConfigEncrypted string     `gorm:"type:text" json:"-"`
	Status          string     `gorm:"size:20;default:'inactive'" json:"status"` // "active", "inactive", "error"
	LastTestedAt    *time.Time `json:"last_tested_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// NotificationRule maps alert severity to notification channels.
type NotificationRule struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	DomainID       uint      `gorm:"index;not null" json:"domain_id"`
	ChannelID      uint      `gorm:"index;not null" json:"channel_id"`
	SeverityFilter string    `gorm:"size:50;not null" json:"severity_filter"` // e.g. "critical", "warning", "informational"
	CreatedAt      time.Time `json:"created_at"`

	// Relations
	Domain  NormalizedDomain    `gorm:"foreignKey:DomainID" json:"domain,omitempty"`
	Channel NotificationChannel `gorm:"foreignKey:ChannelID" json:"channel,omitempty"`
}

// NotificationLog records the delivery status of each notification attempt.
type NotificationLog struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	AlertID     uint      `gorm:"index;not null" json:"alert_id"`
	ChannelID   uint      `gorm:"index;not null" json:"channel_id"`
	Status      string    `gorm:"size:20;not null" json:"status"` // "sent", "failed", "retrying"
	ErrorReason string    `gorm:"type:text" json:"error_reason"`
	RetryCount  int       `gorm:"default:0" json:"retry_count"`
	SentAt      *time.Time `json:"sent_at"`
	CreatedAt   time.Time `json:"created_at"`

	// Relations
	Alert   Alert               `gorm:"foreignKey:AlertID" json:"alert,omitempty"`
	Channel NotificationChannel `gorm:"foreignKey:ChannelID" json:"channel,omitempty"`
}

// User represents an authenticated user (local or SSO).
type User struct {
	ID                 uint       `gorm:"primaryKey" json:"id"`
	ExternalID         string     `gorm:"uniqueIndex;size:255;not null" json:"external_id"`
	Email              string     `gorm:"size:255" json:"email"`
	DisplayName        string     `gorm:"size:255" json:"display_name"`
	PasswordHash       string     `gorm:"size:255" json:"-"`
	AuthSource         string     `gorm:"size:20;default:'local'" json:"auth_source"` // "local" or "oidc"
	MustChangePassword bool       `gorm:"default:false" json:"must_change_password"`
	LastLoginAt        *time.Time `json:"last_login_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`

	// Relations
	Roles []UserRole `gorm:"foreignKey:UserID" json:"roles,omitempty"`
}

// SSOConfig stores OIDC configuration in the database (singleton row).
type SSOConfig struct {
	ID                    uint      `gorm:"primaryKey" json:"id"`
	Enabled               bool      `gorm:"default:false" json:"enabled"`
	IssuerURL             string    `gorm:"size:500" json:"issuer_url"`
	DiscoveryURL          string    `gorm:"size:500" json:"discovery_url"`
	ClientID              string    `gorm:"size:255" json:"client_id"`
	ClientSecret          string    `gorm:"size:500" json:"-"`
	AuthorizationEndpoint string    `gorm:"size:500" json:"authorization_endpoint"`
	TokenEndpoint         string    `gorm:"size:500" json:"token_endpoint"`
	UserinfoEndpoint      string    `gorm:"size:500" json:"userinfo_endpoint"`
	JWKSURI               string    `gorm:"size:500" json:"jwks_uri"`
	EndSessionEndpoint    string    `gorm:"size:500" json:"end_session_endpoint"`
	RedirectURL           string    `gorm:"size:500" json:"redirect_url"`
	Scopes                string    `gorm:"size:255;default:openid profile email groups" json:"scopes"`
	GroupsClaim           string    `gorm:"size:100;default:groups" json:"groups_claim"`
	GroupsSource          string    `gorm:"size:30;default:userinfo" json:"groups_source"` // "id_token" or "userinfo"
	ShowOnLoginPage       bool      `gorm:"default:true" json:"show_on_login_page"`
	CookieSecure          bool      `gorm:"default:false" json:"cookie_secure"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// UserRole defines an assigned role for a user.
type UserRole struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"index;not null" json:"user_id"`
	Role      string    `gorm:"size:30;not null" json:"role"` // "admin", "operator", "viewer"
	CreatedAt time.Time `json:"created_at"`

	// Relations
	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// AuditLog records user actions for auditing purposes.
type AuditLog struct {
	ID            uint            `gorm:"primaryKey" json:"id"`
	UserID        uint            `gorm:"index;not null" json:"user_id"`
	ActionType    string          `gorm:"size:20;not null" json:"action_type"` // "CREATE", "UPDATE", "DELETE"
	ResourceType  string          `gorm:"size:50;not null" json:"resource_type"`
	ResourceID    string          `gorm:"size:50" json:"resource_id"`
	ChangedFields json.RawMessage `gorm:"type:jsonb" json:"changed_fields"`
	CreatedAt     time.Time       `gorm:"index;not null" json:"created_at"`

	// Relations
	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// SyncLog records the results of registrar sync operations.
type SyncLog struct {
	ID                 uint       `gorm:"primaryKey" json:"id"`
	RegistrarAccountID uint       `gorm:"index;not null" json:"registrar_account_id"`
	StartedAt          time.Time  `gorm:"not null" json:"started_at"`
	EndedAt            *time.Time `json:"ended_at"`
	DomainsSynced      int        `gorm:"default:0" json:"domains_synced"`
	DomainsUpdated     int        `gorm:"default:0" json:"domains_updated"`
	Status             string     `gorm:"size:20;not null" json:"status"` // "running", "completed", "failed", "timeout"
	ErrorMessage       string     `gorm:"type:text" json:"error_message"`

	// Relations
	RegistrarAccount RegistrarAccount `gorm:"foreignKey:RegistrarAccountID" json:"registrar_account,omitempty"`
}

// JSON is a custom type for storing JSON arrays (e.g., nameservers) as text.
// This avoids the need for pq.StringArray and works with any PostgreSQL driver.
type JSON []string

// Scan implements the sql.Scanner interface for reading from the database.
func (j *JSON) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		str, ok := value.(string)
		if !ok {
			*j = nil
			return nil
		}
		bytes = []byte(str)
	}
	return json.Unmarshal(bytes, j)
}

// Value implements the driver.Valuer interface for writing to the database.
func (j JSON) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	b, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// GormDataType returns the GORM data type for migrations.
func (JSON) GormDataType() string {
	return "text"
}


// ExpirationRule defines a configurable rule for domain expiration alerts and display.
type ExpirationRule struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	DaysMin   int       `gorm:"not null" json:"days_min"`           // Minimum days remaining (inclusive). Use negative for expired.
	DaysMax   int       `gorm:"not null" json:"days_max"`           // Maximum days remaining (exclusive).
	Severity  string    `gorm:"size:20;not null" json:"severity"`   // "critical", "warning", "info"
	Color     string    `gorm:"size:20;not null" json:"color"`      // Hex color like "#ef4444"
	Label     string    `gorm:"size:50;not null" json:"label"`      // Display label like "已过期", "即将到期"
	Score     int       `gorm:"not null" json:"score"`              // Health score for this range (0-100)
	SortOrder int       `gorm:"default:0" json:"sort_order"`        // Display priority
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EmailMonitor stores email monitoring config per domain.
type EmailMonitor struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	DomainID      uint       `gorm:"uniqueIndex;not null" json:"domain_id"`
	Enabled       bool       `gorm:"default:true" json:"enabled"`
	DKIMSelectors string     `gorm:"size:500" json:"dkim_selectors"`  // comma-separated: "google,selector1,default"
	MailServerIPs string     `gorm:"size:500" json:"mail_server_ips"` // comma-separated IPs for PTR check
	LastCheckedAt *time.Time `json:"last_checked_at"`
	NextCheckAt   *time.Time `json:"next_check_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	// Relations
	Domain NormalizedDomain `gorm:"foreignKey:DomainID" json:"domain,omitempty"`
}

// EmailCheckResult stores the result of an email DNS check.
type EmailCheckResult struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	DomainID    uint      `gorm:"index;not null" json:"domain_id"`
	MonitorID   uint      `gorm:"index;not null" json:"monitor_id"`
	TotalScore  int       `json:"total_score"`                // 0-100
	Grade       string    `gorm:"size:5" json:"grade"`        // A, B, C, D
	MXScore     int       `json:"mx_score"`
	SPFScore    int       `json:"spf_score"`
	DKIMScore   int       `json:"dkim_score"`
	DMARCScore  int       `json:"dmarc_score"`
	PTRScore    int       `json:"ptr_score"`
	MTASTSScore int       `json:"mta_sts_score"`
	TLSRPTScore int       `json:"tlsrpt_score"`
	BIMIScore   int       `json:"bimi_score"`
	Details     string    `gorm:"type:text" json:"details"` // JSON with detailed findings per check
	CheckedAt   time.Time `gorm:"not null" json:"checked_at"`

	// Relations
	Domain NormalizedDomain `gorm:"foreignKey:DomainID" json:"domain,omitempty"`
}


// EmailAlertRule defines configurable alert rules for email security monitoring.
type EmailAlertRule struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	RuleType    string    `gorm:"size:30;not null" json:"rule_type"`    // "total_score", "score_drop", "mx_score", "spf_score", "dkim_score", "dmarc_score"
	Threshold   int       `gorm:"not null" json:"threshold"`           // Score threshold (e.g. 50 for total, 0 for single item)
	Severity    string    `gorm:"size:20;not null" json:"severity"`    // "critical", "warning", "info"
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	Description string    `gorm:"size:200" json:"description"`         // Human readable description
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GroupMapping maps an SSO group name to a platform role.
type GroupMapping struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	GroupName string    `gorm:"size:255;uniqueIndex;not null" json:"group_name"`
	Role      string    `gorm:"size:30;not null" json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// AllModels returns all GORM model types for use in auto-migration.
func AllModels() []interface{} {
	return []interface{}{
		&NormalizedDomain{},
		&RegistrarConfig{},
		&RegistrarAccount{},
		&Group{},
		&Tag{},
		&HealthCheck{},
		&CertificateMonitor{},
		&CertificateCheck{},
		&EmailCheck{},
		&EmailMonitor{},
		&EmailCheckResult{},
		&Alert{},
		&NotificationChannel{},
		&NotificationRule{},
		&NotificationLog{},
		&User{},
		&UserRole{},
		&SSOConfig{},
		&AuditLog{},
		&SyncLog{},
		&ExpirationRule{},
		&EmailAlertRule{},
		&GroupMapping{},
	}
}

// BeforeCreate hook sets default values before creating a domain.
func (d *NormalizedDomain) BeforeCreate(tx *gorm.DB) error {
	if d.Status == "" {
		d.Status = "active"
	}
	if d.HealthScore == 0 {
		d.HealthScore = 100
	}
	if d.CheckIntervalMinutes == 0 {
		d.CheckIntervalMinutes = 5
	}
	if d.ResponseTimeThresholdMs == 0 {
		d.ResponseTimeThresholdMs = 10000
	}
	return nil
}
