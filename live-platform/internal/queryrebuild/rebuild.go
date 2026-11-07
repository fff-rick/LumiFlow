package queryrebuild

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/example/live-platform/internal/queryprojection"
)

type SourceDB interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type RebuildCounts struct {
	Users   int
	Danmaku int
	Gifts   int
}

func Rebuild(ctx context.Context, identity, archive, commerce SourceDB, projector queryprojection.Projector, batch int) (RebuildCounts, error) {
	var counts RebuildCounts
	if batch <= 0 {
		return counts, errors.New("rebuild batch must be positive")
	}
	if err := rebuildUsers(ctx, identity, projector, batch, &counts); err != nil {
		return counts, err
	}
	if err := rebuildDanmaku(ctx, archive, projector, batch, &counts); err != nil {
		return counts, err
	}
	if err := rebuildGifts(ctx, commerce, projector, batch, &counts); err != nil {
		return counts, err
	}
	return counts, nil
}

func rebuildUsers(ctx context.Context, source SourceDB, projector queryprojection.Projector, batch int, counts *RebuildCounts) error {
	for after := int64(0); ; {
		rows, err := source.QueryContext(ctx, `
SELECT u.id,u.nickname,u.avatar,u.status,u.created_at,u.updated_at,
       COALESCE((SELECT MAX(o.id) FROM outbox_events o WHERE o.aggregate_type='IDENTITY' AND o.aggregate_id=CAST(u.id AS CHAR)),0)
FROM users u WHERE u.id>? ORDER BY u.id LIMIT ?`, after, batch)
		if err != nil {
			return fmt.Errorf("query identity snapshot after %d: %w", after, err)
		}
		count, next, scanErr := scanUsers(ctx, rows, projector, after)
		if scanErr != nil {
			return scanErr
		}
		counts.Users += count
		if count == 0 {
			return nil
		}
		after = next
	}
}

func scanUsers(ctx context.Context, rows *sql.Rows, projector queryprojection.Projector, after int64) (int, int64, error) {
	defer func() { _ = rows.Close() }()
	count, next := 0, after
	for rows.Next() {
		var user queryprojection.User
		if err := rows.Scan(&user.ID, &user.Nickname, &user.Avatar, &user.Status, &user.CreatedAt, &user.UpdatedAt, &user.AggregateVersion); err != nil {
			return count, next, fmt.Errorf("scan identity snapshot: %w", err)
		}
		if user.ID <= next {
			return count, next, errors.New("identity snapshot cursor did not advance")
		}
		user.ProjectedAt = user.UpdatedAt
		if err := projector.UpsertUser(ctx, user); err != nil {
			return count, next, fmt.Errorf("project identity %d: %w", user.ID, err)
		}
		next, count = user.ID, count+1
	}
	return count, next, rows.Err()
}

func rebuildDanmaku(ctx context.Context, source SourceDB, projector queryprojection.Projector, batch int, counts *RebuildCounts) error {
	for after := int64(0); ; {
		rows, err := source.QueryContext(ctx, `SELECT id,message_id,room_id,user_id,nickname,content,created_at FROM danmaku_records WHERE id>? ORDER BY id LIMIT ?`, after, batch)
		if err != nil {
			return fmt.Errorf("query danmaku snapshot after %d: %w", after, err)
		}
		count, next, scanErr := scanDanmaku(ctx, rows, projector, after)
		if scanErr != nil {
			return scanErr
		}
		counts.Danmaku += count
		if count == 0 {
			return nil
		}
		after = next
	}
}

func scanDanmaku(ctx context.Context, rows *sql.Rows, projector queryprojection.Projector, after int64) (int, int64, error) {
	defer func() { _ = rows.Close() }()
	count, next := 0, after
	for rows.Next() {
		var id int64
		var message queryprojection.Message
		if err := rows.Scan(&id, &message.MessageID, &message.RoomID, &message.UserID, &message.Nickname, &message.Content, &message.CreatedAt); err != nil {
			return count, next, fmt.Errorf("scan danmaku snapshot: %w", err)
		}
		if id <= next {
			return count, next, errors.New("danmaku snapshot cursor did not advance")
		}
		message.Type = "danmaku"
		if err := projector.InsertMessage(ctx, message); err != nil {
			return count, next, fmt.Errorf("project danmaku %d: %w", id, err)
		}
		next, count = id, count+1
	}
	return count, next, rows.Err()
}

func rebuildGifts(ctx context.Context, source SourceDB, projector queryprojection.Projector, batch int, counts *RebuildCounts) error {
	for after := int64(0); ; {
		rows, err := source.QueryContext(ctx, `
SELECT o.id,o.order_no,o.room_id,o.user_id,g.name,o.gift_count,o.created_at
FROM gift_orders o JOIN gifts g ON g.id=o.gift_id
WHERE o.status=1 AND o.id>? ORDER BY o.id LIMIT ?`, after, batch)
		if err != nil {
			return fmt.Errorf("query gift snapshot after %d: %w", after, err)
		}
		count, next, scanErr := scanGifts(ctx, rows, projector, after)
		if scanErr != nil {
			return scanErr
		}
		counts.Gifts += count
		if count == 0 {
			return nil
		}
		after = next
	}
}

func scanGifts(ctx context.Context, rows *sql.Rows, projector queryprojection.Projector, after int64) (int, int64, error) {
	defer func() { _ = rows.Close() }()
	count, next := 0, after
	for rows.Next() {
		var id int64
		var orderNo string
		var message queryprojection.Message
		if err := rows.Scan(&id, &orderNo, &message.RoomID, &message.UserID, &message.GiftName, &message.GiftCount, &message.CreatedAt); err != nil {
			return count, next, fmt.Errorf("scan gift snapshot: %w", err)
		}
		if id <= next {
			return count, next, errors.New("gift snapshot cursor did not advance")
		}
		message.Type, message.MessageID = "gift", "gift:"+orderNo
		if err := projector.InsertMessage(ctx, message); err != nil {
			return count, next, fmt.Errorf("project gift %d: %w", id, err)
		}
		next, count = id, count+1
	}
	return count, next, rows.Err()
}
