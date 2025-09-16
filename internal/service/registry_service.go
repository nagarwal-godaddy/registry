package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

const maxServerVersionsPerServer = 10000

// registryServiceImpl implements the RegistryService interface using our Database
type registryServiceImpl struct {
	db  database.Database
	cfg *config.Config
}

// NewRegistryService creates a new registry service with the provided database
func NewRegistryService(db database.Database, cfg *config.Config) RegistryService {
	return &registryServiceImpl{
		db:  db,
		cfg: cfg,
	}
}

// List returns registry entries with cursor-based pagination and optional filtering
func (s *registryServiceImpl) List(filter *database.ServerFilter, cursor string, limit int) ([]apiv0.ServerJSON, string, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// If limit is not set or negative, use a default limit
	if limit <= 0 {
		limit = 30
	}

	// Use the database's ListServers method with pagination and filtering
	serverRecords, nextCursor, err := s.db.List(ctx, filter, cursor, limit)
	if err != nil {
		return nil, "", err
	}

	// Return ServerJSONs directly
	result := make([]apiv0.ServerJSON, len(serverRecords))
	for i, record := range serverRecords {
		result[i] = *record
	}

	return result, nextCursor, nil
}

// GetByID retrieves a specific server by its registry metadata ID in flattened format
func (s *registryServiceImpl) GetByID(id string) (*apiv0.ServerJSON, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverRecord, err := s.db.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Return the server record directly
	return serverRecord, nil
}

// Publish publishes a server with flattened _meta extensions
//nolint:cyclop // Complexity is necessary for validation, rate limiting, and version management
func (s *registryServiceImpl) Publish(req apiv0.ServerJSON, authMethodSubject string, hasGlobalPermissions bool) (*apiv0.ServerJSON, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check rate limiting (skip for admins with global permissions or disabled rate limiting)
	if authMethodSubject != "" && s.cfg.RateLimitEnabled && !hasGlobalPermissions {
		// Check if user is exempt from rate limiting
		isExempt := s.isExemptFromRateLimit(authMethodSubject)
		
		if !isExempt {
			// Check and increment the publish count atomically
			currentCount, incrementSuccessful, err := s.db.CheckAndIncrementPublishCount(ctx, authMethodSubject, s.cfg.RateLimitPerDay)
			if err != nil {
				return nil, fmt.Errorf("failed to check rate limit: %w", err)
			}
			
			if !incrementSuccessful {
				return nil, fmt.Errorf("rate limit exceeded: you have published %d servers today (limit: %d per day). If you need a higher limit, please open an issue at https://github.com/modelcontextprotocol/registry/issues",
					currentCount, s.cfg.RateLimitPerDay)
			}
		}
	}

	// Validate the request
	if err := validators.ValidatePublishRequest(req, s.cfg); err != nil {
		return nil, err
	}

	publishTime := time.Now()
	serverJSON := req

	// Check for duplicate remote URLs
	if err := s.validateNoDuplicateRemoteURLs(ctx, serverJSON); err != nil {
		return nil, err
	}

	filter := &database.ServerFilter{Name: &serverJSON.Name}
	existingServerVersions, _, err := s.db.List(ctx, filter, "", maxServerVersionsPerServer)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	// Check we haven't exceeded the maximum versions allowed for a server
	if len(existingServerVersions) >= maxServerVersionsPerServer {
		return nil, database.ErrMaxServersReached
	}

	// Check this isn't a duplicate version
	for _, server := range existingServerVersions {
		existingVersion := server.Version
		if existingVersion == serverJSON.Version {
			return nil, database.ErrInvalidVersion
		}
	}

	// Determine if this version should be marked as latest
	existingLatest := s.getCurrentLatestVersion(existingServerVersions)
	isNewLatest := true
	if existingLatest != nil {
		var existingPublishedAt time.Time
		if existingLatest.Meta != nil && existingLatest.Meta.Official != nil {
			existingPublishedAt = existingLatest.Meta.Official.PublishedAt
		}
		isNewLatest = CompareVersions(
			serverJSON.Version,
			existingLatest.Version,
			publishTime,
			existingPublishedAt,
		) > 0
	}

	// Create complete server with metadata
	server := serverJSON // Copy the input

	// Initialize meta if not present
	if server.Meta == nil {
		server.Meta = &apiv0.ServerMeta{}
	}

	// Set registry metadata
	server.Meta.Official = &apiv0.RegistryExtensions{
		ID:          uuid.New().String(),
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
	}

	// Create server in database
	serverRecord, err := s.db.CreateServer(ctx, &server)
	if err != nil {
		return nil, err
	}

	// Mark previous latest as no longer latest
	if isNewLatest && existingLatest != nil {
		var existingLatestID string
		if existingLatest.Meta != nil && existingLatest.Meta.Official != nil {
			existingLatestID = existingLatest.Meta.Official.ID
		}
		if existingLatestID != "" {
			// Create a deep copy to avoid race conditions
			updatedLatest := *existingLatest
			if updatedLatest.Meta != nil {
				// Create a copy of the Meta structure
				metaCopy := *updatedLatest.Meta
				updatedLatest.Meta = &metaCopy
				
				if updatedLatest.Meta.Official != nil {
					// Create a copy of the Official metadata
					officialCopy := *updatedLatest.Meta.Official
					officialCopy.IsLatest = false
					officialCopy.UpdatedAt = time.Now()
					updatedLatest.Meta.Official = &officialCopy
				}
			}
			
			if _, err := s.db.UpdateServer(ctx, existingLatestID, &updatedLatest); err != nil {
				return nil, err
			}
		}
	}

	// Return the server record directly
	return serverRecord, nil
}

// validateNoDuplicateRemoteURLs checks that no other server is using the same remote URLs
func (s *registryServiceImpl) validateNoDuplicateRemoteURLs(ctx context.Context, serverDetail apiv0.ServerJSON) error {
	// Check each remote URL in the new server for conflicts
	for _, remote := range serverDetail.Remotes {
		// Use filter to find servers with this remote URL
		filter := &database.ServerFilter{RemoteURL: &remote.URL}

		conflictingServers, _, err := s.db.List(ctx, filter, "", 1000)
		if err != nil {
			return fmt.Errorf("failed to check remote URL conflict: %w", err)
		}

		// Check if any conflicting server has a different name
		for _, conflictingServer := range conflictingServers {
			if conflictingServer.Name != serverDetail.Name {
				return fmt.Errorf("remote URL %s is already used by server %s", remote.URL, conflictingServer.Name)
			}
		}
	}

	return nil
}

// getCurrentLatestVersion finds the current latest version from existing server versions
func (s *registryServiceImpl) getCurrentLatestVersion(existingServerVersions []*apiv0.ServerJSON) *apiv0.ServerJSON {
	for _, server := range existingServerVersions {
		if server.Meta != nil && server.Meta.Official != nil &&
			server.Meta.Official.IsLatest {
			return server
		}
	}
	return nil
}

// isExemptFromRateLimit checks if an auth subject is exempt from rate limiting
func (s *registryServiceImpl) isExemptFromRateLimit(authMethodSubject string) bool {
	if s.cfg.RateLimitExemptions == "" {
		return false
	}
	
	exemptions := strings.Split(s.cfg.RateLimitExemptions, ",")
	for _, exemption := range exemptions {
		exemption = strings.TrimSpace(exemption)
		if exemption == "" {
			continue
		}
		
		// Handle wildcard exemptions
		if strings.HasSuffix(exemption, "/*") {
			prefix := strings.TrimSuffix(exemption, "/*")
			// Match auth subject exactly or with a separator
			if authMethodSubject == prefix || strings.HasPrefix(authMethodSubject, prefix+".") || strings.HasPrefix(authMethodSubject, prefix+"/") {
				return true
			}
		} else if exemption == authMethodSubject {
			return true
		}
	}
	return false
}

// EditServer updates an existing server with new details (admin operation)
func (s *registryServiceImpl) EditServer(id string, req apiv0.ServerJSON) (*apiv0.ServerJSON, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Validate the request
	if err := validators.ValidatePublishRequest(req, s.cfg); err != nil {
		return nil, err
	}

	serverJSON := req

	// Check for duplicate remote URLs
	if err := s.validateNoDuplicateRemoteURLs(ctx, serverJSON); err != nil {
		return nil, err
	}

	// Update server in database
	serverRecord, err := s.db.UpdateServer(ctx, id, &serverJSON)
	if err != nil {
		return nil, err
	}

	// Return the server record directly
	return serverRecord, nil
}
