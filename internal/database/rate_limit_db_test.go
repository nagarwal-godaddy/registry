package database_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testAuthMethodSubject = "io.github.testuser"

// TestDatabaseRateLimitPersistence tests that rate limit data persists correctly
func TestDatabaseRateLimitPersistence(t *testing.T) {
	testCases := []struct {
		name string
		db   database.Database
	}{
		{
			name: "MemoryDB",
			db:   database.NewMemoryDB(),
		},
		// PostgreSQL tests would require a test database connection
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			authMethodSubject := testAuthMethodSubject
			today := time.Now()

			// Initial count should be 0
			count, err := tc.db.GetPublishCount(ctx, authMethodSubject, today)
			assert.NoError(t, err)
			assert.Equal(t, 0, count, "Initial count should be 0")

			// Increment count
			err = tc.db.IncrementPublishCount(ctx, authMethodSubject)
			assert.NoError(t, err)

			// Count should be 1
			count, err = tc.db.GetPublishCount(ctx, authMethodSubject, today)
			assert.NoError(t, err)
			assert.Equal(t, 1, count, "Count should be 1 after first increment")

			// Increment multiple times
			for i := 0; i < 4; i++ {
				err = tc.db.IncrementPublishCount(ctx, authMethodSubject)
				assert.NoError(t, err)
			}

			// Count should be 5
			count, err = tc.db.GetPublishCount(ctx, authMethodSubject, today)
			assert.NoError(t, err)
			assert.Equal(t, 5, count, "Count should be 5 after 5 increments")

			// Different authMethodSubject should have independent count
			authMethodSubject2 := "io.github.otheruser"
			count, err = tc.db.GetPublishCount(ctx, authMethodSubject2, today)
			assert.NoError(t, err)
			assert.Equal(t, 0, count, "Different authMethodSubject should have 0 count")

			err = tc.db.IncrementPublishCount(ctx, authMethodSubject2)
			assert.NoError(t, err)

			count, err = tc.db.GetPublishCount(ctx, authMethodSubject2, today)
			assert.NoError(t, err)
			assert.Equal(t, 1, count, "Different authMethodSubject should have independent count")

			// Original authMethodSubject count should be unchanged
			count, err = tc.db.GetPublishCount(ctx, authMethodSubject, today)
			assert.NoError(t, err)
			assert.Equal(t, 5, count, "Original authMethodSubject count should be unchanged")
		})
	}
}

// TestDatabaseRateLimitDateIsolation tests that counts are isolated by date
func TestDatabaseRateLimitDateIsolation(t *testing.T) {
	testCases := []struct {
		name string
		db   database.Database
	}{
		{
			name: "MemoryDB",
			db:   database.NewMemoryDB(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			authMethodSubject := testAuthMethodSubject
			today := time.Now()
			yesterday := today.AddDate(0, 0, -1)
			tomorrow := today.AddDate(0, 0, 1)

			// Increment count for today
			for i := 0; i < 3; i++ {
				err := tc.db.IncrementPublishCount(ctx, authMethodSubject)
				assert.NoError(t, err)
			}

			// Today's count should be 3
			count, err := tc.db.GetPublishCount(ctx, authMethodSubject, today)
			assert.NoError(t, err)
			assert.Equal(t, 3, count, "Today's count should be 3")

			// Yesterday's count should be 0
			count, err = tc.db.GetPublishCount(ctx, authMethodSubject, yesterday)
			assert.NoError(t, err)
			assert.Equal(t, 0, count, "Yesterday's count should be 0")

			// Tomorrow's count should be 0
			count, err = tc.db.GetPublishCount(ctx, authMethodSubject, tomorrow)
			assert.NoError(t, err)
			assert.Equal(t, 0, count, "Tomorrow's count should be 0")
		})
	}
}

// TestDatabaseRateLimitConcurrency tests concurrent increments
func TestDatabaseRateLimitConcurrency(t *testing.T) {
	testCases := []struct {
		name string
		db   database.Database
	}{
		{
			name: "MemoryDB",
			db:   database.NewMemoryDB(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			authMethodSubject := testAuthMethodSubject
			today := time.Now()
			numGoroutines := 100
			incrementsPerGoroutine := 10

			var wg sync.WaitGroup
			wg.Add(numGoroutines)

			// Launch concurrent increments
			for i := 0; i < numGoroutines; i++ {
				go func() {
					defer wg.Done()
					for j := 0; j < incrementsPerGoroutine; j++ {
						err := tc.db.IncrementPublishCount(ctx, authMethodSubject)
						if err != nil {
							t.Errorf("Unexpected error during increment: %v", err)
						}
					}
				}()
			}

			wg.Wait()

			// Verify final count is correct
			expectedCount := numGoroutines * incrementsPerGoroutine
			count, err := tc.db.GetPublishCount(ctx, authMethodSubject, today)
			assert.NoError(t, err)
			assert.Equal(t, expectedCount, count, "Final count should be %d", expectedCount)
		})
	}
}

// TestDatabaseRateLimitMultipleAuthMethodSubjects tests operations across multiple authMethodSubjects
func TestDatabaseRateLimitMultipleAuthMethodSubjects(t *testing.T) {
	testCases := []struct {
		name string
		db   database.Database
	}{
		{
			name: "MemoryDB",
			db:   database.NewMemoryDB(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			today := time.Now()
			authMethodSubjects := []string{
				"io.github.user1",
				"io.github.user2",
				"com.example.app",
				"org.nonprofit.project",
			}

			// Increment each authMethodSubject a different number of times
			for i, ns := range authMethodSubjects {
				for j := 0; j <= i; j++ {
					err := tc.db.IncrementPublishCount(ctx, ns)
					assert.NoError(t, err)
				}
			}

			// Verify each authMethodSubject has the correct count
			for i, ns := range authMethodSubjects {
				count, err := tc.db.GetPublishCount(ctx, ns, today)
				assert.NoError(t, err)
				assert.Equal(t, i+1, count, "AuthMethodSubject %s should have count %d", ns, i+1)
			}
		})
	}
}

// TestDatabaseRateLimitAtomicity tests that increments are atomic
func TestDatabaseRateLimitAtomicity(t *testing.T) {
	testCases := []struct {
		name string
		db   database.Database
	}{
		{
			name: "MemoryDB",
			db:   database.NewMemoryDB(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			authMethodSubject := testAuthMethodSubject
			today := time.Now()

			// Run many concurrent increments and reads
			numOperations := 1000
			var wg sync.WaitGroup
			wg.Add(numOperations * 2)

			incrementCount := 0
			var mu sync.Mutex

			// Half doing increments
			for i := 0; i < numOperations; i++ {
				go func() {
					defer wg.Done()
					err := tc.db.IncrementPublishCount(ctx, authMethodSubject)
					if err == nil {
						mu.Lock()
						incrementCount++
						mu.Unlock()
					}
				}()
			}

			// Half doing reads (to test read/write races)
			counts := make([]int, numOperations)
			for i := 0; i < numOperations; i++ {
				go func(idx int) {
					defer wg.Done()
					count, _ := tc.db.GetPublishCount(ctx, authMethodSubject, today)
					counts[idx] = count
				}(i)
			}

			wg.Wait()

			// Final count should match successful increments
			finalCount, err := tc.db.GetPublishCount(ctx, authMethodSubject, today)
			assert.NoError(t, err)
			assert.Equal(t, incrementCount, finalCount, "Final count should match successful increments")

			// All read counts should be valid (between 0 and final count)
			for _, count := range counts {
				assert.GreaterOrEqual(t, count, 0, "Count should never be negative")
				assert.LessOrEqual(t, count, finalCount, "Count should never exceed final count")
			}
		})
	}
}

// TestDatabaseRateLimitZeroAndNegative tests edge cases with zero and boundary values
func TestDatabaseRateLimitZeroAndNegative(t *testing.T) {
	testCases := []struct {
		name string
		db   database.Database
	}{
		{
			name: "MemoryDB",
			db:   database.NewMemoryDB(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			today := time.Now()

			// Empty authMethodSubject (edge case)
			emptyNS := ""
			count, err := tc.db.GetPublishCount(ctx, emptyNS, today)
			assert.NoError(t, err)
			assert.Equal(t, 0, count, "Empty authMethodSubject should return 0")

			_ = tc.db.IncrementPublishCount(ctx, emptyNS)
			// The behavior for empty authMethodSubject might vary by implementation
			// Just ensure it doesn't panic

			// Very long authMethodSubject
			longNS := "com." + string(make([]byte, 1000))
			count, err = tc.db.GetPublishCount(ctx, longNS, today)
			assert.NoError(t, err)
			assert.Equal(t, 0, count, "Long authMethodSubject should return 0 initially")

			// Special characters in authMethodSubject
			specialNS := "io.github.user-_.test@#$%"
			err = tc.db.IncrementPublishCount(ctx, specialNS)
			assert.NoError(t, err)

			count, err = tc.db.GetPublishCount(ctx, specialNS, today)
			assert.NoError(t, err)
			assert.Equal(t, 1, count, "Special character authMethodSubject should work")
		})
	}
}

// TestDatabaseRateLimitTimestamps tests timestamp recording functionality
func TestDatabaseRateLimitTimestamps(t *testing.T) {
	testCases := []struct {
		name string
		db   database.Database
	}{
		{
			name: "MemoryDB",
			db:   database.NewMemoryDB(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			authMethodSubject := testAuthMethodSubject
			today := time.Now()

			// First increment
			beforeFirst := time.Now()
			err := tc.db.IncrementPublishCount(ctx, authMethodSubject)
			require.NoError(t, err)
			afterFirst := time.Now()

			// Small delay
			time.Sleep(10 * time.Millisecond)

			// Second increment
			beforeSecond := time.Now()
			err = tc.db.IncrementPublishCount(ctx, authMethodSubject)
			require.NoError(t, err)
			afterSecond := time.Now()

			// Verify count is correct
			count, err := tc.db.GetPublishCount(ctx, authMethodSubject, today)
			assert.NoError(t, err)
			assert.Equal(t, 2, count)

			// Note: Actual timestamp verification would require access to the
			// first_attempt_at and last_attempt_at fields, which aren't exposed
			// through the current interface. This test ensures the operations
			// complete successfully with timing considerations.
			_ = beforeFirst
			_ = afterFirst
			_ = beforeSecond
			_ = afterSecond
		})
	}
}

// TestDatabaseRateLimitLargeScale tests with a large number of authMethodSubjects and operations
func TestDatabaseRateLimitLargeScale(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large scale test in short mode")
	}

	testCases := []struct {
		name string
		db   database.Database
	}{
		{
			name: "MemoryDB",
			db:   database.NewMemoryDB(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			today := time.Now()
			numAuthMethodSubjects := 100
			incrementsPerAuthMethodSubject := 50

			// Create many authMethodSubjects
			authMethodSubjects := make([]string, numAuthMethodSubjects)
			for i := 0; i < numAuthMethodSubjects; i++ {
				authMethodSubjects[i] = fmt.Sprintf("io.github.user%d", i)
			}

			// Increment each authMethodSubject multiple times concurrently
			var wg sync.WaitGroup
			wg.Add(numAuthMethodSubjects)

			for _, ns := range authMethodSubjects {
				go func(authMethodSubject string, expectedCount int) {
					defer wg.Done()
					for j := 0; j < expectedCount; j++ {
						err := tc.db.IncrementPublishCount(ctx, authMethodSubject)
						if err != nil {
							t.Errorf("Error incrementing %s: %v", authMethodSubject, err)
						}
					}
				}(ns, incrementsPerAuthMethodSubject)
			}

			wg.Wait()

			// Verify all counts
			for _, ns := range authMethodSubjects {
				count, err := tc.db.GetPublishCount(ctx, ns, today)
				assert.NoError(t, err)
				assert.Equal(t, incrementsPerAuthMethodSubject, count, "AuthMethodSubject %s should have correct count", ns)
			}
		})
	}
}

// TestDatabaseRateLimitDateBoundaries tests behavior around date boundaries
func TestDatabaseRateLimitDateBoundaries(t *testing.T) {
	testCases := []struct {
		name string
		db   database.Database
	}{
		{
			name: "MemoryDB",
			db:   database.NewMemoryDB(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			authMethodSubject := testAuthMethodSubject

			// Test with various dates
			dates := []time.Time{
				time.Now(),                          // Today
				time.Now().AddDate(0, 0, -1),        // Yesterday
				time.Now().AddDate(0, 0, 1),         // Tomorrow
				time.Now().AddDate(-1, 0, 0),        // Last year
				time.Now().AddDate(1, 0, 0),         // Next year
				time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), // Y2K
				time.Date(2038, 1, 19, 3, 14, 7, 0, time.UTC), // Near Unix timestamp limit
			}

			for _, date := range dates {
				count, err := tc.db.GetPublishCount(ctx, authMethodSubject, date)
				assert.NoError(t, err)
				assert.Equal(t, 0, count, "Count for date %v should be 0", date)
			}

			// Increment today's count
			err := tc.db.IncrementPublishCount(ctx, authMethodSubject)
			assert.NoError(t, err)

			// Only today should have a count
			count, err := tc.db.GetPublishCount(ctx, authMethodSubject, time.Now())
			assert.NoError(t, err)
			assert.Equal(t, 1, count, "Today's count should be 1")

			// All other dates should still be 0
			for _, date := range dates[1:] {
				count, err = tc.db.GetPublishCount(ctx, authMethodSubject, date)
				assert.NoError(t, err)
				assert.Equal(t, 0, count, "Count for date %v should still be 0", date)
			}
		})
	}
}

