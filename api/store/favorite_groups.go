package store

import (
	"context"
	"errors"
	"strings"

	"tempmail/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrFavoriteGroupConflict = errors.New("favorite group name already exists")
	ErrLastFavoriteGroup     = errors.New("at least one favorite group is required")
)

func (s *Store) ensureFavoriteGroup(ctx context.Context, accountID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO favorite_groups (account_id, name, sort_order)
		 SELECT $1, '收藏夹1', 0
		 WHERE NOT EXISTS (SELECT 1 FROM favorite_groups WHERE account_id = $1)`,
		accountID,
	)
	return err
}

func (s *Store) ListFavoriteGroups(ctx context.Context, accountID uuid.UUID) ([]model.FavoriteGroup, uuid.UUID, error) {
	if err := s.ensureFavoriteGroup(ctx, accountID); err != nil {
		return nil, uuid.Nil, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT g.id, g.account_id, g.name, g.sort_order, COUNT(m.id)::INT,
		        g.created_at, g.updated_at
		 FROM favorite_groups g
		 LEFT JOIN mailboxes m
		   ON m.favorite_group_id = g.id AND m.is_favorite = TRUE
		 WHERE g.account_id = $1
		 GROUP BY g.id
		 ORDER BY g.sort_order, g.created_at, g.id`,
		accountID,
	)
	if err != nil {
		return nil, uuid.Nil, err
	}
	defer rows.Close()

	groups, err := pgx.CollectRows(rows, pgx.RowToStructByPos[model.FavoriteGroup])
	if err != nil {
		return nil, uuid.Nil, err
	}
	if len(groups) == 0 {
		return groups, uuid.Nil, nil
	}

	var selectedID uuid.UUID
	err = s.pool.QueryRow(ctx,
		`SELECT p.group_id
		 FROM favorite_group_preferences p
		 JOIN favorite_groups g ON g.id = p.group_id AND g.account_id = p.account_id
		 WHERE p.account_id = $1`,
		accountID,
	).Scan(&selectedID)
	if err == pgx.ErrNoRows {
		selectedID = groups[0].ID
		_, err = s.pool.Exec(ctx,
			`INSERT INTO favorite_group_preferences (account_id, group_id, updated_at)
			 VALUES ($1, $2, NOW())
			 ON CONFLICT (account_id) DO UPDATE
			 SET group_id = EXCLUDED.group_id, updated_at = NOW()`,
			accountID, selectedID,
		)
	}
	if err != nil {
		return nil, uuid.Nil, err
	}
	return groups, selectedID, nil
}

func (s *Store) CreateFavoriteGroup(ctx context.Context, accountID uuid.UUID, name string) (*model.FavoriteGroup, error) {
	name = strings.TrimSpace(name)
	var group model.FavoriteGroup
	err := s.pool.QueryRow(ctx,
		`INSERT INTO favorite_groups (account_id, name, sort_order)
		 VALUES ($1, $2, COALESCE((SELECT MAX(sort_order) + 1 FROM favorite_groups WHERE account_id = $1), 0))
		 RETURNING id, account_id, name, sort_order, 0, created_at, updated_at`,
		accountID, name,
	).Scan(&group.ID, &group.AccountID, &group.Name, &group.SortOrder, &group.MailboxCount, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "idx_favorite_groups_account_name") {
			return nil, ErrFavoriteGroupConflict
		}
		return nil, err
	}
	return &group, nil
}

func (s *Store) RenameFavoriteGroup(ctx context.Context, accountID, groupID uuid.UUID, name string) (*model.FavoriteGroup, error) {
	name = strings.TrimSpace(name)
	var group model.FavoriteGroup
	err := s.pool.QueryRow(ctx,
		`UPDATE favorite_groups g
		 SET name = $3, updated_at = NOW()
		 WHERE g.id = $1 AND g.account_id = $2
		 RETURNING g.id, g.account_id, g.name, g.sort_order,
		 	(SELECT COUNT(*)::INT FROM mailboxes m WHERE m.favorite_group_id = g.id AND m.is_favorite = TRUE),
		 	g.created_at, g.updated_at`,
		groupID, accountID, name,
	).Scan(&group.ID, &group.AccountID, &group.Name, &group.SortOrder, &group.MailboxCount, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "idx_favorite_groups_account_name") {
			return nil, ErrFavoriteGroupConflict
		}
		return nil, err
	}
	return &group, nil
}

func (s *Store) SelectFavoriteGroup(ctx context.Context, accountID, groupID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO favorite_group_preferences (account_id, group_id, updated_at)
		 SELECT $1, id, NOW()
		 FROM favorite_groups
		 WHERE id = $2 AND account_id = $1
		 ON CONFLICT (account_id) DO UPDATE
		 SET group_id = EXCLUDED.group_id, updated_at = NOW()`,
		accountID, groupID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Store) ReorderFavoriteGroup(ctx context.Context, accountID, groupID uuid.UUID, direction string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx,
		`SELECT id, sort_order
		 FROM favorite_groups
		 WHERE account_id = $1
		 ORDER BY sort_order, created_at, id
		 FOR UPDATE`,
		accountID,
	)
	if err != nil {
		return err
	}
	type groupPosition struct {
		id    uuid.UUID
		order int
	}
	var groups []groupPosition
	for rows.Next() {
		var item groupPosition
		if err := rows.Scan(&item.id, &item.order); err != nil {
			rows.Close()
			return err
		}
		groups = append(groups, item)
	}
	rows.Close()

	index := -1
	for i, group := range groups {
		if group.id == groupID {
			index = i
			break
		}
	}
	if index < 0 {
		return pgx.ErrNoRows
	}
	target := index - 1
	if direction == "down" {
		target = index + 1
	}
	if target < 0 || target >= len(groups) {
		return tx.Commit(ctx)
	}

	groups[index], groups[target] = groups[target], groups[index]
	for i, group := range groups {
		if _, err := tx.Exec(ctx,
			`UPDATE favorite_groups SET sort_order = $3, updated_at = NOW()
			 WHERE id = $1 AND account_id = $2`,
			group.id, accountID, i,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteFavoriteGroup(ctx context.Context, accountID, groupID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx,
		`SELECT id FROM favorite_groups WHERE account_id = $1 FOR UPDATE`,
		accountID,
	)
	if err != nil {
		return err
	}
	var groupIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		groupIDs = append(groupIDs, id)
	}
	rows.Close()
	if len(groupIDs) <= 1 {
		return ErrLastFavoriteGroup
	}

	var replacementID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT id FROM favorite_groups
		 WHERE account_id = $1 AND id <> $2
		 ORDER BY sort_order, created_at, id
		 LIMIT 1`,
		accountID, groupID,
	).Scan(&replacementID)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE mailboxes
		 SET favorite_group_id = $3
		 WHERE account_id = $1 AND favorite_group_id = $2`,
		accountID, groupID, replacementID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE favorite_group_preferences
		 SET group_id = $3, updated_at = NOW()
		 WHERE account_id = $1 AND group_id = $2`,
		accountID, groupID, replacementID,
	); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx,
		`DELETE FROM favorite_groups WHERE id = $1 AND account_id = $2`,
		groupID, accountID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return tx.Commit(ctx)
}

func (s *Store) MoveMailboxToFavoriteGroup(ctx context.Context, mailboxID, accountID, groupID uuid.UUID) (*model.Mailbox, error) {
	var mailbox model.Mailbox
	err := s.pool.QueryRow(ctx,
		`UPDATE mailboxes m
		 SET is_favorite = TRUE, favorite_group_id = $3
		 WHERE m.id = $1
		   AND m.account_id = $2
		   AND EXISTS (
		   	SELECT 1 FROM favorite_groups g
		   	WHERE g.id = $3 AND g.account_id = $2
		   )
		 RETURNING id, account_id, address, domain_id, full_address, is_favorite,
		           tg_forward_enabled, created_at, expires_at, favorite_group_id`,
		mailboxID, accountID, groupID,
	).Scan(&mailbox.ID, &mailbox.AccountID, &mailbox.Address, &mailbox.DomainID,
		&mailbox.FullAddress, &mailbox.IsFavorite, &mailbox.TGForwardEnabled,
		&mailbox.CreatedAt, &mailbox.ExpiresAt, &mailbox.FavoriteGroupID)
	if err != nil {
		return nil, err
	}
	return &mailbox, nil
}

func (s *Store) MoveMailboxesToFavoriteGroup(ctx context.Context, mailboxIDs []uuid.UUID, accountID, groupID uuid.UUID) (int64, error) {
	idArray := uuidArrayLiteral(mailboxIDs)
	tag, err := s.pool.Exec(ctx,
		`UPDATE mailboxes m
		 SET is_favorite = TRUE, favorite_group_id = $3
		 WHERE m.id = ANY($1::uuid[])
		   AND m.account_id = $2
		   AND EXISTS (
		   	SELECT 1 FROM favorite_groups g
		   	WHERE g.id = $3 AND g.account_id = $2
		   )`,
		idArray, accountID, groupID,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) DeleteMailboxes(ctx context.Context, mailboxIDs []uuid.UUID, accountID uuid.UUID) (int64, error) {
	idArray := uuidArrayLiteral(mailboxIDs)
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM mailboxes WHERE id = ANY($1::uuid[]) AND account_id = $2`,
		idArray, accountID,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func uuidArrayLiteral(ids []uuid.UUID) string {
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		values = append(values, id.String())
	}
	return "{" + strings.Join(values, ",") + "}"
}
