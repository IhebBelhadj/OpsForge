-- name: UserAwards :many
SELECT badge_id, awarded_at FROM user_badges WHERE user_id = ?;

-- name: InsertAward :execrows
INSERT INTO user_badges (user_id, badge_id, awarded_at) VALUES (?, ?, ?)
ON CONFLICT (user_id, badge_id) DO NOTHING;
