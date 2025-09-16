package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestServer(name string, version string) apiv0.ServerJSON {
	return apiv0.ServerJSON{
		Name:        name,
		Description: "A test server",
		Repository: model.Repository{
			URL:    "https://github.com/testuser/test-repo",
			Source: "github",
			ID:     "testuser/test-repo",
		},
		Version: version,
	}
}

func TestPublish_RateLimiting(t *testing.T) {
	tests := []struct {
		name                 string
		authMethodSubject    string
		hasGlobalPermissions bool
		existingCount        int
		limit                int
		enabled              bool
		exemptions           string
		expectError          bool
		errorContains        string
	}{
		{
			name:                 "under limit allows publish",
			authMethodSubject:    "testuser",
			hasGlobalPermissions: false,
			existingCount:        5,
			limit:                10,
			enabled:              true,
			expectError:          false,
		},
		{
			name:                 "at limit blocks publish",
			authMethodSubject:    "testuser",
			hasGlobalPermissions: false,
			existingCount:        10,
			limit:                10,
			enabled:              true,
			expectError:          true,
			errorContains:        "rate limit exceeded",
		},
		{
			name:                 "disabled allows any",
			authMethodSubject:    "testuser",
			hasGlobalPermissions: false,
			existingCount:        100,
			limit:                10,
			enabled:              false,
			expectError:          false,
		},
		{
			name:                 "global permissions bypasses",
			authMethodSubject:    "testuser",
			hasGlobalPermissions: true,
			existingCount:        100,
			limit:                10,
			enabled:              true,
			expectError:          false,
		},
		{
			name:                 "exempt user bypasses",
			authMethodSubject:    "exemptuser",
			hasGlobalPermissions: false,
			existingCount:        100,
			limit:                10,
			enabled:              true,
			exemptions:           "exemptuser",
			expectError:          false,
		},
		{
			name:                 "wildcard exemption works",
			authMethodSubject:    "anthropic.claude",
			hasGlobalPermissions: false,
			existingCount:        100,
			limit:                10,
			enabled:              true,
			exemptions:           "anthropic/*",
			expectError:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			db := database.NewMemoryDB()
			cfg := &config.Config{
				RateLimitEnabled:         tt.enabled,
				RateLimitPerDay:          tt.limit,
				RateLimitExemptions:      tt.exemptions,
				EnableRegistryValidation: false,
			}

			// Pre-populate existing attempts
			ctx := context.Background()
			for i := 0; i < tt.existingCount; i++ {
				err := db.IncrementPublishCount(ctx, tt.authMethodSubject)
				require.NoError(t, err)
			}

			// Test
			svc := service.NewRegistryService(db, cfg)
			testServer := createTestServer("io.github.test/server", "1.0.0")
			_, err := svc.Publish(testServer, tt.authMethodSubject, tt.hasGlobalPermissions)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPublish_ConcurrentRateLimiting(t *testing.T) {
	ctx := context.Background()
	db := database.NewMemoryDB()
	cfg := &config.Config{
		RateLimitEnabled:         true,
		RateLimitPerDay:          10,
		EnableRegistryValidation: false,
	}
	svc := service.NewRegistryService(db, cfg)

	authMethodSubject := "testuser"
	numGoroutines := 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	successCount := 0
	var mu sync.Mutex
	errors := make([]error, 0)

	// Launch concurrent publish attempts
	for i := 0; i < numGoroutines; i++ {
		go func(version int) {
			defer wg.Done()
			testServer := createTestServer("io.github.test/server", fmt.Sprintf("1.0.%d", version))
			_, err := svc.Publish(testServer, authMethodSubject, false)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successCount++
			} else {
				errors = append(errors, err)
			}
		}(i)
	}

	wg.Wait()

	// With the atomic check-and-increment operation, exactly 10 should succeed
	assert.Equal(t, 10, successCount, "Expected exactly 10 successful publishes")
	assert.Equal(t, 10, len(errors), "Expected exactly 10 rate limit errors")

	// Verify all errors are rate limit errors
	for _, err := range errors {
		assert.Contains(t, err.Error(), "rate limit exceeded")
	}

	// Verify the database count is exactly 10
	count, err := db.GetPublishCount(ctx, authMethodSubject, time.Now())
	require.NoError(t, err)
	assert.Equal(t, 10, count, "Database should show exactly 10 publishes")
}

func TestPublish_DifferentUsersIndependentLimits(t *testing.T) {
	cfg := &config.Config{
		RateLimitEnabled:         true,
		RateLimitPerDay:          3,
		EnableRegistryValidation: false,
	}
	db := database.NewMemoryDB()
	svc := service.NewRegistryService(db, cfg)

	user1 := "user1"
	user2 := "user2"

	// Fill up user1's limit
	for i := 0; i < 3; i++ {
		testServer := createTestServer("io.github.test/server1", fmt.Sprintf("1.0.%d", i))
		_, err := svc.Publish(testServer, user1, false)
		assert.NoError(t, err)
	}

	// user1 should be blocked
	testServer := createTestServer("io.github.test/server1", "1.0.99")
	_, err := svc.Publish(testServer, user1, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")

	// user2 should still have full quota
	for i := 0; i < 3; i++ {
		testServer := createTestServer("io.github.test/server2", fmt.Sprintf("1.0.%d", i))
		_, err := svc.Publish(testServer, user2, false)
		assert.NoError(t, err, "user2 publish %d should succeed", i+1)
	}

	// Now user2 should also be blocked
	testServer = createTestServer("io.github.test/server2", "1.0.99")
	_, err = svc.Publish(testServer, user2, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
}

func TestPublish_ExemptionPatterns(t *testing.T) {
	tests := []struct {
		name              string
		exemptions        string
		authMethodSubject string
		isExempt          bool
	}{
		{
			name:              "exact match exemption",
			exemptions:        "modelcontextprotocol",
			authMethodSubject: "modelcontextprotocol",
			isExempt:          true,
		},
		{
			name:              "wildcard root match",
			exemptions:        "anthropic/*",
			authMethodSubject: "anthropic",
			isExempt:          true,
		},
		{
			name:              "wildcard subdomain match",
			exemptions:        "anthropic/*",
			authMethodSubject: "anthropic.claude",
			isExempt:          true,
		},
		{
			name:              "wildcard deep subdomain match",
			exemptions:        "anthropic/*",
			authMethodSubject: "anthropic.claude.test.deep",
			isExempt:          true,
		},
		{
			name:              "no match for partial",
			exemptions:        "anthropic/*",
			authMethodSubject: "anthropi",
			isExempt:          false,
		},
		{
			name:              "multiple exemptions",
			exemptions:        "test1,example/*,foo.bar",
			authMethodSubject: "example.app",
			isExempt:          true,
		},
		{
			name:              "empty exemptions",
			exemptions:        "",
			authMethodSubject: "anyone",
			isExempt:          false,
		},
		{
			name:              "whitespace in exemptions",
			exemptions:        "test1, example/*, foo.bar",
			authMethodSubject: "example.app",
			isExempt:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				RateLimitEnabled:         true,
				RateLimitPerDay:          1,
				RateLimitExemptions:      tt.exemptions,
				EnableRegistryValidation: false,
			}
			db := database.NewMemoryDB()
			svc := service.NewRegistryService(db, cfg)

			// Pre-fill the limit
			ctx := context.Background()
			err := db.IncrementPublishCount(ctx, tt.authMethodSubject)
			require.NoError(t, err)

			// Try to publish
			testServer := createTestServer("io.github.test/server", "1.0.0")
			_, err = svc.Publish(testServer, tt.authMethodSubject, false)

			if tt.isExempt {
				assert.NoError(t, err, "Exempt user should be able to publish")
			} else {
				assert.Error(t, err, "Non-exempt user should be rate limited")
				assert.Contains(t, err.Error(), "rate limit exceeded")
			}
		})
	}
}