package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/seuusuario/ticketing/services/events/internal/domain"
)

// EventRepository isola o SQL do resto da aplicacao. Os handlers so conhecem
// esta interface, o que mantem o acoplamento baixo e facilita teste.
type EventRepository struct {
	pool *pgxpool.Pool
}

func NewEventRepository(pool *pgxpool.Pool) *EventRepository {
	return &EventRepository{pool: pool}
}

const eventColumns = `id, name, description, venue, city, starts_at, capacity, price_cents, status, created_at, updated_at`

func scanEvent(row pgx.Row) (domain.Event, error) {
	var e domain.Event
	err := row.Scan(
		&e.ID, &e.Name, &e.Description, &e.Venue, &e.City,
		&e.StartsAt, &e.Capacity, &e.PriceCents, &e.Status,
		&e.CreatedAt, &e.UpdatedAt,
	)
	return e, err
}

func (r *EventRepository) Create(ctx context.Context, in domain.EventInput) (domain.Event, error) {
	const q = `
		INSERT INTO events (name, description, venue, city, starts_at, capacity, price_cents)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING ` + eventColumns

	e, err := scanEvent(r.pool.QueryRow(ctx, q,
		in.Name, in.Description, in.Venue, in.City, in.StartsAt, in.Capacity, in.PriceCents,
	))
	if err != nil {
		return domain.Event{}, fmt.Errorf("inserindo evento: %w", err)
	}
	return e, nil
}

func (r *EventRepository) GetByID(ctx context.Context, id string) (domain.Event, error) {
	const q = `SELECT ` + eventColumns + ` FROM events WHERE id = $1`

	e, err := scanEvent(r.pool.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Event{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Event{}, fmt.Errorf("buscando evento: %w", err)
	}
	return e, nil
}

func (r *EventRepository) List(ctx context.Context, f domain.ListFilter) (domain.Page, error) {
	where := []string{"1 = 1"}
	args := []any{}

	if f.City != "" {
		args = append(args, strings.ToLower(f.City))
		where = append(where, fmt.Sprintf("lower(city) = $%d", len(args)))
	}
	if f.Status != "" {
		args = append(args, string(f.Status))
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	clause := strings.Join(where, " AND ")

	var total int64
	countQ := `SELECT count(*) FROM events WHERE ` + clause
	if err := r.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return domain.Page{}, fmt.Errorf("contando eventos: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	listQ := fmt.Sprintf(
		`SELECT %s FROM events WHERE %s ORDER BY starts_at ASC LIMIT $%d OFFSET $%d`,
		eventColumns, clause, len(args)-1, len(args),
	)

	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return domain.Page{}, fmt.Errorf("listando eventos: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Event, 0, f.Limit)
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return domain.Page{}, fmt.Errorf("lendo linha de evento: %w", err)
		}
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return domain.Page{}, fmt.Errorf("iterando eventos: %w", err)
	}

	return domain.Page{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}, nil
}

func (r *EventRepository) Update(ctx context.Context, id string, in domain.EventInput) (domain.Event, error) {
	const q = `
		UPDATE events
		   SET name = $2, description = $3, venue = $4, city = $5,
		       starts_at = $6, capacity = $7, price_cents = $8, updated_at = now()
		 WHERE id = $1
		RETURNING ` + eventColumns

	e, err := scanEvent(r.pool.QueryRow(ctx, q,
		id, in.Name, in.Description, in.Venue, in.City, in.StartsAt, in.Capacity, in.PriceCents,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Event{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Event{}, fmt.Errorf("atualizando evento: %w", err)
	}
	return e, nil
}

func (r *EventRepository) UpdateStatus(ctx context.Context, id string, status domain.Status) (domain.Event, error) {
	const q = `
		UPDATE events SET status = $2, updated_at = now()
		 WHERE id = $1
		RETURNING ` + eventColumns

	e, err := scanEvent(r.pool.QueryRow(ctx, q, id, string(status)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Event{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Event{}, fmt.Errorf("atualizando status: %w", err)
	}
	return e, nil
}

func (r *EventRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM events WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("removendo evento: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *EventRepository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}
