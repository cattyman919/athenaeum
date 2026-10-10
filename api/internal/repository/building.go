package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// DBTX is satisfied by *pgxpool.Pool and pgx.Tx.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// BuildingRepository implements building.Lookup over any DBTX.
type BuildingRepository struct{ db DBTX }

// NewBuildingRepository wraps a pool or transaction.
func NewBuildingRepository(db DBTX) *BuildingRepository { return &BuildingRepository{db: db} }

func (r *BuildingRepository) one(ctx context.Context, what, sql string, args ...any) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, sql, args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, model.New(model.CodeNotFound, what+" not found")
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("lookup %s: %w", what, err)
	}
	return id, nil
}

// Building looks up a building by code.
func (r *BuildingRepository) Building(ctx context.Context, code string) (uuid.UUID, error) {
	return r.one(ctx, "building", `SELECT id FROM buildings WHERE code = $1`, code)
}

// Floor looks up a floor by number.
func (r *BuildingRepository) Floor(ctx context.Context, buildingID uuid.UUID, n int) (uuid.UUID, error) {
	return r.one(ctx, "floor", `SELECT id FROM floors WHERE building_id = $1 AND floor_number = $2`, buildingID, n)
}

// Section looks up a section by code.
func (r *BuildingRepository) Section(ctx context.Context, floorID uuid.UUID, code string) (uuid.UUID, error) {
	return r.one(ctx, "section", `SELECT id FROM sections WHERE floor_id = $1 AND code = $2`, floorID, code)
}

// Aisle looks up an aisle by code.
func (r *BuildingRepository) Aisle(ctx context.Context, sectionID uuid.UUID, code string) (uuid.UUID, error) {
	return r.one(ctx, "aisle", `SELECT id FROM aisles WHERE section_id = $1 AND code = $2`, sectionID, code)
}

// Shelf looks up a shelf by code.
func (r *BuildingRepository) Shelf(ctx context.Context, aisleID uuid.UUID, code string) (uuid.UUID, error) {
	return r.one(ctx, "shelf", `SELECT id FROM shelves WHERE aisle_id = $1 AND code = $2`, aisleID, code)
}
