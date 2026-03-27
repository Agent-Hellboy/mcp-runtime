package clickhouse

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// Client wraps a ClickHouse connection.
type Client struct {
	Conn   clickhouse.Conn
	DBName string
}

// Config provides ClickHouse connection configuration.
type Config struct {
	Addr        string
	Database    string
	DialTimeout time.Duration
}

// NewClient creates a new ClickHouse client.
func NewClient(cfg Config) (*Client, error) {
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = 5 * time.Second
	}

	if err := validateDBName(cfg.Database); err != nil {
		return nil, fmt.Errorf("invalid database name: %w", err)
	}

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{cfg.Addr},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
		},
		DialTimeout: cfg.DialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ClickHouse: %w", err)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping ClickHouse: %w", err)
	}

	return &Client{
		Conn:   conn,
		DBName: cfg.Database,
	}, nil
}

// Close closes the ClickHouse connection.
func (c *Client) Close() error {
	return c.Conn.Close()
}

// validateDBName validates ClickHouse database name format.
func validateDBName(name string) error {
	if name == "" {
		return fmt.Errorf("empty")
	}
	matched, err := regexp.MatchString(`^[A-Za-z_][A-Za-z0-9_]*$`, name)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("must match ^[A-Za-z_][A-Za-z0-9_]*$")
	}
	return nil
}

// Ping checks the database connection.
func (c *Client) Ping(ctx context.Context) error {
	return c.Conn.Ping(ctx)
}
