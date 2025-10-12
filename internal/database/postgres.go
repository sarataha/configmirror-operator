/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	corev1 "k8s.io/api/core/v1"
)

// Pool interface for pgxpool.Pool to allow mocking
type Pool interface {
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	Ping(ctx context.Context) error
	Close()
}

// Client wraps PostgreSQL connection pool
type Client struct {
	pool Pool
}

// NewClient creates a new PostgreSQL client with connection pooling
func NewClient(ctx context.Context, connString string) (*Client, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	config.MaxConns = 10
	config.MinConns = 2
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 30 * time.Minute
	config.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Client{pool: pool}, nil
}

// Close closes the database connection pool
func (c *Client) Close() {
	if c.pool != nil {
		c.pool.Close()
	}
}

// Ping checks if the database connection is healthy
func (c *Client) Ping(ctx context.Context) error {
	return c.pool.Ping(ctx)
}

// InitSchema creates the configmaps table if it doesn't exist
func (c *Client) InitSchema(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS configmaps (
			id SERIAL PRIMARY KEY,
			name VARCHAR(253) NOT NULL,
			namespace VARCHAR(253) NOT NULL,
			data JSONB NOT NULL,
			labels JSONB,
			annotations JSONB,
			configmirror_name VARCHAR(253) NOT NULL,
			configmirror_namespace VARCHAR(253) NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			UNIQUE(name, namespace, configmirror_namespace, configmirror_name)
		);

		CREATE INDEX IF NOT EXISTS idx_configmirror
			ON configmaps(configmirror_namespace, configmirror_name);

		CREATE INDEX IF NOT EXISTS idx_created_at
			ON configmaps(created_at DESC);
	`

	_, err := c.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	return nil
}

// ConfigMapRecord represents a ConfigMap record in the database
type ConfigMapRecord struct {
	Name                  string
	Namespace             string
	Data                  map[string]string
	Labels                map[string]string
	Annotations           map[string]string
	ConfigMirrorName      string
	ConfigMirrorNamespace string
}

// SaveConfigMap saves or updates a ConfigMap in the database
func (c *Client) SaveConfigMap(ctx context.Context, cm *corev1.ConfigMap, mirrorName, mirrorNamespace string) error {
	query := `
		INSERT INTO configmaps (
			name, namespace, data, labels, annotations,
			configmirror_name, configmirror_namespace, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (name, namespace, configmirror_namespace, configmirror_name)
		DO UPDATE SET
			data = EXCLUDED.data,
			labels = EXCLUDED.labels,
			annotations = EXCLUDED.annotations,
			updated_at = NOW()
	`

	_, err := c.pool.Exec(ctx, query,
		cm.Name,
		cm.Namespace,
		cm.Data,
		cm.Labels,
		cm.Annotations,
		mirrorName,
		mirrorNamespace,
	)

	if err != nil {
		return fmt.Errorf("failed to save ConfigMap: %w", err)
	}

	return nil
}

// DeleteConfigMap removes a ConfigMap from the database
func (c *Client) DeleteConfigMap(ctx context.Context, name, namespace, mirrorName, mirrorNamespace string) error {
	query := `
		DELETE FROM configmaps
		WHERE name = $1 AND namespace = $2
			AND configmirror_name = $3
			AND configmirror_namespace = $4
	`

	result, err := c.pool.Exec(ctx, query, name, namespace, mirrorName, mirrorNamespace)
	if err != nil {
		return fmt.Errorf("failed to delete ConfigMap: %w", err)
	}

	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	return nil
}

// GetConfigMaps retrieves all ConfigMaps for a specific ConfigMirror
func (c *Client) GetConfigMaps(ctx context.Context, mirrorName, mirrorNamespace string) ([]ConfigMapRecord, error) {
	query := `
		SELECT name, namespace, data, labels, annotations,
			configmirror_name, configmirror_namespace
		FROM configmaps
		WHERE configmirror_name = $1 AND configmirror_namespace = $2
		ORDER BY created_at DESC
	`

	rows, err := c.pool.Query(ctx, query, mirrorName, mirrorNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to query ConfigMaps: %w", err)
	}
	defer rows.Close()

	var records []ConfigMapRecord
	for rows.Next() {
		var record ConfigMapRecord
		err := rows.Scan(
			&record.Name,
			&record.Namespace,
			&record.Data,
			&record.Labels,
			&record.Annotations,
			&record.ConfigMirrorName,
			&record.ConfigMirrorNamespace,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ConfigMap row: %w", err)
		}
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating ConfigMap rows: %w", err)
	}

	return records, nil
}
