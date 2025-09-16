-- Add rate limiting table to track publish attempts by authenticated user and date
CREATE TABLE publish_attempts (
    auth_method_subject VARCHAR(255) NOT NULL,
    attempt_date DATE NOT NULL DEFAULT CURRENT_DATE,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    first_attempt_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_attempt_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (auth_method_subject, attempt_date)
);

-- Index for efficient lookups by auth_method_subject
CREATE INDEX idx_publish_attempts_auth_subject ON publish_attempts(auth_method_subject);

-- Index for cleanup queries by date
CREATE INDEX idx_publish_attempts_date ON publish_attempts(attempt_date);

-- Comment for documentation
COMMENT ON TABLE publish_attempts IS 'Tracks daily publish attempts per authenticated user for rate limiting';
COMMENT ON COLUMN publish_attempts.auth_method_subject IS 'The authenticated user identifier (e.g., GitHub username)';
COMMENT ON COLUMN publish_attempts.attempt_date IS 'The date of the attempts (resets daily)';
COMMENT ON COLUMN publish_attempts.attempt_count IS 'Number of successful publishes on this date';