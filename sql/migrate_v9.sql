-- Favorite mailbox groups. Existing favorites are assigned to each account's
-- first group without deleting or recreating mailbox data.

BEGIN;

CREATE TABLE IF NOT EXISTS favorite_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name VARCHAR(64) NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_favorite_groups_account_name
    ON favorite_groups (account_id, LOWER(name));
CREATE INDEX IF NOT EXISTS idx_favorite_groups_account_sort
    ON favorite_groups (account_id, sort_order, created_at);

ALTER TABLE mailboxes ADD COLUMN IF NOT EXISTS favorite_group_id UUID;
ALTER TABLE mailboxes DROP CONSTRAINT IF EXISTS mailboxes_favorite_group_id_fkey;
ALTER TABLE mailboxes
    ADD CONSTRAINT mailboxes_favorite_group_id_fkey
    FOREIGN KEY (favorite_group_id) REFERENCES favorite_groups(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_mailboxes_favorite_group
    ON mailboxes (account_id, favorite_group_id, created_at DESC)
    WHERE is_favorite = TRUE;

CREATE TABLE IF NOT EXISTS favorite_group_preferences (
    account_id UUID PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    group_id UUID REFERENCES favorite_groups(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO favorite_groups (account_id, name, sort_order)
SELECT a.id, '收藏夹1', 0
FROM accounts a
WHERE NOT EXISTS (
    SELECT 1 FROM favorite_groups g WHERE g.account_id = a.id
);

UPDATE mailboxes m
SET favorite_group_id = g.id
FROM favorite_groups g
WHERE m.account_id = g.account_id
  AND m.is_favorite = TRUE
  AND m.favorite_group_id IS NULL
  AND g.id = (
      SELECT g2.id
      FROM favorite_groups g2
      WHERE g2.account_id = m.account_id
      ORDER BY g2.sort_order, g2.created_at, g2.id
      LIMIT 1
  );

INSERT INTO favorite_group_preferences (account_id, group_id)
SELECT a.id, (
    SELECT g.id
    FROM favorite_groups g
    WHERE g.account_id = a.id
    ORDER BY g.sort_order, g.created_at, g.id
    LIMIT 1
)
FROM accounts a
ON CONFLICT (account_id) DO NOTHING;

COMMIT;
