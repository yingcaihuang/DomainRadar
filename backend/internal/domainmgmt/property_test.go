package domainmgmt

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"domainradar/internal/domain"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// =============================================================================
// Property 17: CSV/Excel import validation
// Valid rows create records, invalid rows report errors, sum equals total
// =============================================================================

func TestProperty17_ImportValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random domain name (may be empty to test invalid case).
		isEmpty := rapid.Bool().Draw(t, "isEmpty")
		var domainName string
		if isEmpty {
			domainName = ""
		} else {
			domainName = rapid.StringMatching(`[a-z]{3,20}\.[a-z]{2,6}`).Draw(t, "domainName")
		}

		// Generate a random expiration date string (may be invalid).
		isValidDate := rapid.Bool().Draw(t, "isValidDate")
		var expirationDate string
		if isValidDate {
			year := rapid.IntRange(2024, 2030).Draw(t, "year")
			month := rapid.IntRange(1, 12).Draw(t, "month")
			day := rapid.IntRange(1, 28).Draw(t, "day")
			expirationDate = fmt.Sprintf("%04d-%02d-%02d", year, month, day)
		} else {
			// Empty or invalid format.
			invalidType := rapid.IntRange(0, 1).Draw(t, "invalidType")
			if invalidType == 0 {
				expirationDate = ""
			} else {
				expirationDate = rapid.String().Draw(t, "invalidDate")
			}
		}

		name, parsedDate, importErr := ValidateImportRow(domainName, expirationDate)

		if isEmpty {
			// Empty domain name should produce error.
			assert.NotNil(t, importErr, "empty domain name should produce an error")
			assert.Equal(t, "domain_name", importErr.Field)
		} else if !isValidDate {
			// Invalid/empty expiration date should produce error.
			assert.NotNil(t, importErr, "invalid expiration date should produce an error")
			assert.Contains(t, importErr.Field, "expiration_date")
		} else {
			// Valid row should succeed.
			assert.Nil(t, importErr, "valid row should not produce an error")
			assert.Equal(t, strings.TrimSpace(domainName), name)
			assert.NotNil(t, parsedDate)
		}
	})
}

// =============================================================================
// Property 18: Export data round-trip
// Exported CSV contains all fields including tags and groups
// =============================================================================

func TestProperty18_ExportRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		expDate := time.Date(
			rapid.IntRange(2024, 2030).Draw(t, "year"),
			time.Month(rapid.IntRange(1, 12).Draw(t, "month")),
			rapid.IntRange(1, 28).Draw(t, "day"),
			0, 0, 0, 0, time.UTC,
		)
		createDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

		numTags := rapid.IntRange(0, 5).Draw(t, "numTags")
		tags := make([]domain.Tag, numTags)
		for i := 0; i < numTags; i++ {
			tags[i] = domain.Tag{
				ID:   uint(i + 1),
				Name: fmt.Sprintf("tag%d", i+1),
			}
		}

		groupName := rapid.StringMatching(`[a-z]{3,15}`).Draw(t, "groupName")
		group := &domain.Group{
			ID:   1,
			Name: groupName,
		}

		domainName := rapid.StringMatching(`[a-z]{3,20}\.[a-z]{2,6}`).Draw(t, "domainName")

		d := domain.NormalizedDomain{
			DomainName:          domainName,
			RegistrarIdentifier: "godaddy",
			ExpirationDate:      &expDate,
			CreationDate:        &createDate,
			AutoRenew:           rapid.Bool().Draw(t, "autoRenew"),
			Status:              "active",
			Nameservers:         domain.JSON{"ns1.example.com", "ns2.example.com"},
			DataSourceType:      "manual",
			HealthScore:         rapid.IntRange(0, 100).Draw(t, "healthScore"),
			Notes:               "test note",
			Tags:                tags,
			Group:               group,
		}

		row := EncodeExportRow(d)

		// Verify all expected fields are present.
		assert.Equal(t, 12, len(row), "export row should have 12 columns")
		assert.Equal(t, domainName, row[0], "domain_name")
		assert.Equal(t, "godaddy", row[1], "registrar")
		assert.Equal(t, expDate.Format("2006-01-02"), row[2], "expiration_date")
		assert.Equal(t, createDate.Format("2006-01-02"), row[3], "creation_date")
		assert.Equal(t, "active", row[5], "status")
		assert.Contains(t, row[6], "ns1.example.com", "nameservers should include ns1")
		assert.Equal(t, "manual", row[7], "data_source_type")

		// Tags should be comma-separated.
		if numTags > 0 {
			assert.NotEmpty(t, row[9], "tags should not be empty when tags exist")
			tagParts := strings.Split(row[9], ",")
			assert.Equal(t, numTags, len(tagParts), "tag count in export")
		}

		// Group should be present.
		assert.Equal(t, groupName, row[10], "group name in export")

		assert.Equal(t, "test note", row[11], "notes")
	})
}

// =============================================================================
// Property 27: Tag and group constraint enforcement
// Rejects operations exceeding limits (20 tags, 50 char names, 3 levels)
// =============================================================================

func TestProperty27_TagGroupConstraints(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Test tag constraints.
		tagCount := rapid.IntRange(0, 30).Draw(t, "tagCount")
		tagNameLen := rapid.IntRange(0, 60).Draw(t, "tagNameLen")
		tagName := strings.Repeat("a", tagNameLen)

		err := ValidateTagConstraints(tagCount, tagName)

		if tagCount > MaxTagsPerDomain {
			assert.Error(t, err, "should reject when tag count > %d", MaxTagsPerDomain)
		} else if tagNameLen < 1 || tagNameLen > MaxTagNameLength {
			assert.Error(t, err, "should reject tag names outside 1-%d chars", MaxTagNameLength)
		} else {
			assert.NoError(t, err, "should accept valid tag constraints")
		}
	})

	// Test group level constraints.
	rapid.Check(t, func(t *rapid.T) {
		parentLevel := rapid.IntRange(0, 5).Draw(t, "parentLevel")

		err := ValidateGroupLevel(parentLevel)

		if parentLevel+1 > MaxGroupLevels {
			assert.Error(t, err, "should reject groups exceeding %d levels", MaxGroupLevels)
		} else {
			assert.NoError(t, err, "should accept groups within level limit")
		}
	})
}

// =============================================================================
// Property 28: Domain list filtering correctness
// Results include all and only domains matching ALL filter criteria
// =============================================================================

func TestProperty28_DomainFiltering(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a set of domains.
		numDomains := rapid.IntRange(1, 20).Draw(t, "numDomains")
		registrars := []string{"godaddy", "cloudflare", "namecheap"}
		statuses := []string{"active", "expired", "unverified-removed"}

		domains := make([]domain.NormalizedDomain, numDomains)
		for i := 0; i < numDomains; i++ {
			regIdx := rapid.IntRange(0, len(registrars)-1).Draw(t, fmt.Sprintf("reg_%d", i))
			statusIdx := rapid.IntRange(0, len(statuses)-1).Draw(t, fmt.Sprintf("status_%d", i))
			numTags := rapid.IntRange(0, 3).Draw(t, fmt.Sprintf("ntags_%d", i))

			tags := make([]domain.Tag, numTags)
			for j := 0; j < numTags; j++ {
				tags[j] = domain.Tag{ID: uint(rapid.IntRange(1, 5).Draw(t, fmt.Sprintf("tagid_%d_%d", i, j)))}
			}

			groupID := uint(rapid.IntRange(1, 3).Draw(t, fmt.Sprintf("gid_%d", i)))

			domains[i] = domain.NormalizedDomain{
				ID:                  uint(i + 1),
				DomainName:          fmt.Sprintf("domain%d.com", i),
				RegistrarIdentifier: registrars[regIdx],
				Status:              statuses[statusIdx],
				GroupID:             &groupID,
				Tags:                tags,
			}
		}

		// Generate filter criteria.
		filterRegistrar := ""
		if rapid.Bool().Draw(t, "filterByRegistrar") {
			filterRegistrar = registrars[rapid.IntRange(0, len(registrars)-1).Draw(t, "filterReg")]
		}

		filterStatus := ""
		if rapid.Bool().Draw(t, "filterByStatus") {
			filterStatus = statuses[rapid.IntRange(0, len(statuses)-1).Draw(t, "filterStatus")]
		}

		var filterGroupID *uint
		if rapid.Bool().Draw(t, "filterByGroup") {
			gid := uint(rapid.IntRange(1, 3).Draw(t, "filterGroupID"))
			filterGroupID = &gid
		}

		var filterTagIDs []uint
		if rapid.Bool().Draw(t, "filterByTag") {
			filterTagIDs = []uint{uint(rapid.IntRange(1, 5).Draw(t, "filterTagID"))}
		}

		// Apply filter.
		result := FilterDomains(domains, filterTagIDs, filterGroupID, filterRegistrar, filterStatus)

		// Verify: every result matches ALL criteria.
		for _, d := range result {
			if filterRegistrar != "" {
				assert.Equal(t, filterRegistrar, d.RegistrarIdentifier,
					"filtered domain must match registrar filter")
			}
			if filterStatus != "" {
				assert.Equal(t, filterStatus, d.Status,
					"filtered domain must match status filter")
			}
			if filterGroupID != nil {
				assert.NotNil(t, d.GroupID)
				assert.Equal(t, *filterGroupID, *d.GroupID,
					"filtered domain must match group filter")
			}
			if len(filterTagIDs) > 0 {
				hasTag := false
				for _, t := range d.Tags {
					for _, id := range filterTagIDs {
						if t.ID == id {
							hasTag = true
						}
					}
				}
				assert.True(t, hasTag, "filtered domain must have at least one matching tag")
			}
		}

		// Verify: no domain that matches ALL criteria is excluded.
		for _, d := range domains {
			shouldInclude := true

			if filterRegistrar != "" && d.RegistrarIdentifier != filterRegistrar {
				shouldInclude = false
			}
			if filterStatus != "" && d.Status != filterStatus {
				shouldInclude = false
			}
			if filterGroupID != nil && (d.GroupID == nil || *d.GroupID != *filterGroupID) {
				shouldInclude = false
			}
			if len(filterTagIDs) > 0 {
				hasTag := false
				for _, t := range d.Tags {
					for _, id := range filterTagIDs {
						if t.ID == id {
							hasTag = true
						}
					}
				}
				if !hasTag {
					shouldInclude = false
				}
			}

			if shouldInclude {
				found := false
				for _, r := range result {
					if r.ID == d.ID {
						found = true
						break
					}
				}
				assert.True(t, found,
					"domain %d matches all filters and should be included in results", d.ID)
			}
		}
	})
}
