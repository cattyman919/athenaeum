package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/repository"
	"github.com/cattyman919/athenaeum/api/internal/testutil/pgtest"
)

func TestUserRepository(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewUserRepository(pgtest.New(t))

	hash := "$argon2id$stub"
	u := model.User{ID: uuid.New(), Email: "demo@athenaeum.dev", Name: "Demo", Role: model.RoleUser, PasswordHash: &hash}
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	dup := u
	dup.ID = uuid.New()
	dup.Email = "DEMO@athenaeum.dev"
	if err := repo.Create(ctx, dup); !errCode(err, model.CodeEmailTaken) {
		t.Fatalf("dup email: %v", err)
	}

	got, err := repo.ByEmail(ctx, "Demo@Athenaeum.Dev")
	if err != nil || got.ID != u.ID || got.Email != "demo@athenaeum.dev" || got.PasswordHash == nil {
		t.Fatalf("ByEmail %+v %v", got, err)
	}
	if _, err := repo.ByEmail(ctx, "ghost@athenaeum.dev"); !errCode(err, model.CodeNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if got, err := repo.ByID(ctx, u.ID); err != nil || got.Name != "Demo" {
		t.Fatalf("ByID %v", err)
	}

	if err := repo.LinkGoogle(ctx, u.ID, "g-sub"); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.ByGoogleSub(ctx, "g-sub"); err != nil || got.ID != u.ID {
		t.Fatalf("ByGoogleSub %v", err)
	}
}

func TestTokenRepository(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	users := repository.NewUserRepository(pool)
	repo := repository.NewTokenRepository(pool)

	u := model.User{ID: uuid.New(), Email: "a@athenaeum.dev", Name: "A", Role: model.RoleUser}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	live := model.RefreshToken{ID: uuid.New(), UserID: u.ID, Hash: "h-live", ExpiresAt: now.Add(time.Hour)}
	old := model.RefreshToken{ID: uuid.New(), UserID: u.ID, Hash: "h-old", ExpiresAt: now.Add(-time.Hour)}
	for _, tk := range []model.RefreshToken{live, old} {
		if err := repo.Insert(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}

	check := func(hash string, want model.ConsumeResult) {
		t.Helper()
		_, res, err := repo.Consume(ctx, hash, now)
		if err != nil || res != want {
			t.Fatalf("%s: res=%v err=%v want %v", hash, res, err, want)
		}
	}
	check("h-live", model.ConsumeOK)
	check("h-live", model.ConsumeReplayed) // atomic: second consume is a replay
	check("h-old", model.ConsumeExpired)
	check("missing", model.ConsumeUnknown)

	live2 := model.RefreshToken{ID: uuid.New(), UserID: u.ID, Hash: "h-live2", ExpiresAt: now.Add(time.Hour)}
	_ = repo.Insert(ctx, live2)
	if err := repo.RevokeAllForUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	check("h-live2", model.ConsumeReplayed)
}

func errCode(err error, c model.Code) bool {
	de, ok := model.AsError(err)
	return ok && de.Code == c
}
