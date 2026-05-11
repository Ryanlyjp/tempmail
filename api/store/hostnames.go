package store

import (
	"context"
	"errors"
	"strings"

	"tempmail/model"

	"github.com/jackc/pgx/v5"
)

func normalizeHostname(hostname string) string {
	hostname = strings.TrimSpace(strings.ToLower(hostname))
	hostname = strings.TrimSuffix(hostname, ".")
	return hostname
}

func (s *Store) GetDefaultHostname(ctx context.Context) (string, error) {
	var hostname string
	err := s.pool.QueryRow(ctx,
		`SELECT hostname
		 FROM hostnames
		 WHERE is_active = TRUE
		 ORDER BY created_at, id
		 LIMIT 1`,
	).Scan(&hostname)
	if err == nil {
		return hostname, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	legacy, legacyErr := s.GetSetting(ctx, "smtp_hostname")
	if legacyErr != nil {
		if errors.Is(legacyErr, pgx.ErrNoRows) {
			return "", nil
		}
		return "", legacyErr
	}
	return normalizeHostname(legacy), nil
}

func (s *Store) ListHostnames(ctx context.Context, activeOnly bool) ([]model.Hostname, error) {
	query := `
		SELECT h.id, h.hostname, h.is_active, COUNT(d.id)::INT AS domain_count, h.created_at
		FROM hostnames h
		LEFT JOIN domains d ON d.hostname_id = h.id
	`
	if activeOnly {
		query += ` WHERE h.is_active = TRUE`
	}
	query += `
		GROUP BY h.id
		ORDER BY h.is_active DESC, h.created_at, h.id`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[model.Hostname])
}

func (s *Store) GetHostnameByID(ctx context.Context, id int) (*model.Hostname, error) {
	var h model.Hostname
	err := s.pool.QueryRow(ctx,
		`SELECT h.id, h.hostname, h.is_active, COUNT(d.id)::INT AS domain_count, h.created_at
		 FROM hostnames h
		 LEFT JOIN domains d ON d.hostname_id = h.id
		 WHERE h.id = $1
		 GROUP BY h.id`,
		id,
	).Scan(&h.ID, &h.Hostname, &h.IsActive, &h.DomainCount, &h.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func (s *Store) GetHostnameByName(ctx context.Context, hostname string) (*model.Hostname, error) {
	var h model.Hostname
	err := s.pool.QueryRow(ctx,
		`SELECT h.id, h.hostname, h.is_active, COUNT(d.id)::INT AS domain_count, h.created_at
		 FROM hostnames h
		 LEFT JOIN domains d ON d.hostname_id = h.id
		 WHERE h.hostname = $1
		 GROUP BY h.id`,
		normalizeHostname(hostname),
	).Scan(&h.ID, &h.Hostname, &h.IsActive, &h.DomainCount, &h.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func (s *Store) CreateHostname(ctx context.Context, hostname string) (*model.Hostname, error) {
	hostname = normalizeHostname(hostname)
	if hostname == "" {
		return nil, pgx.ErrNoRows
	}

	var id int
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO hostnames (hostname, is_active)
		 VALUES ($1, TRUE)
		 RETURNING id`,
		hostname,
	).Scan(&id); err != nil {
		return nil, err
	}
	return s.GetHostnameByID(ctx, id)
}

func (s *Store) UpsertHostname(ctx context.Context, hostname string) (*model.Hostname, error) {
	hostname = normalizeHostname(hostname)
	if hostname == "" {
		return nil, nil
	}

	var id int
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO hostnames (hostname, is_active)
		 VALUES ($1, TRUE)
		 ON CONFLICT (hostname) DO UPDATE SET hostname = EXCLUDED.hostname
		 RETURNING id`,
		hostname,
	).Scan(&id); err != nil {
		return nil, err
	}
	return s.GetHostnameByID(ctx, id)
}

func (s *Store) ResolveHostname(ctx context.Context, hostnameID *int, hostname string, allowCreate bool) (*model.Hostname, error) {
	if hostnameID != nil && *hostnameID > 0 {
		return s.GetHostnameByID(ctx, *hostnameID)
	}

	hostname = normalizeHostname(hostname)
	if hostname == "" {
		return nil, nil
	}

	found, err := s.GetHostnameByName(ctx, hostname)
	if err == nil {
		return found, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if !allowCreate {
		return nil, pgx.ErrNoRows
	}
	return s.UpsertHostname(ctx, hostname)
}

func (s *Store) UpdateHostname(ctx context.Context, id int, hostname string) (*model.Hostname, error) {
	hostname = normalizeHostname(hostname)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var oldHostname string
	if err := tx.QueryRow(ctx, `SELECT hostname FROM hostnames WHERE id = $1`, id).Scan(&oldHostname); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `UPDATE hostnames SET hostname = $1 WHERE id = $2`, hostname, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE domains
		 SET hostname = $1,
		     hostname_id = CASE WHEN hostname_id IS NULL THEN $2 ELSE hostname_id END
		 WHERE hostname_id = $2 OR LOWER(TRIM(hostname)) = $3`,
		hostname, id, normalizeHostname(oldHostname),
	); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE app_settings
		 SET value = $1, updated_at = NOW()
		 WHERE key = 'smtp_hostname' AND LOWER(TRIM(value)) = $2`,
		hostname, normalizeHostname(oldHostname),
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetHostnameByID(ctx, id)
}

func (s *Store) ToggleHostname(ctx context.Context, id int, active bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE hostnames SET is_active = $1 WHERE id = $2`, active, id)
	return err
}

func (s *Store) DeleteHostname(ctx context.Context, id int) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var hostname string
	if err := tx.QueryRow(ctx, `SELECT hostname FROM hostnames WHERE id = $1`, id).Scan(&hostname); err != nil {
		return 0, err
	}

	tag, err := tx.Exec(ctx,
		`UPDATE domains
		 SET hostname = '', hostname_id = NULL
		 WHERE hostname_id = $1 OR LOWER(TRIM(hostname)) = $2`,
		id, normalizeHostname(hostname),
	)
	if err != nil {
		return 0, err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM hostnames WHERE id = $1`, id); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE app_settings
		 SET value = '', updated_at = NOW()
		 WHERE key = 'smtp_hostname' AND LOWER(TRIM(value)) = $1`,
		normalizeHostname(hostname),
	); err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
