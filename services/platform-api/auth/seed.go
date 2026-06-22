package auth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
)

const (
	DefaultDevUserEmail     = "test@mcpruntime.org"
	DefaultDevUserPassword  = "test@123"
	DefaultDevAdminEmail    = "admin@mcpruntime.org"
	DefaultDevAdminPassword = "admin@123"
)

func PlatformDSNFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("POSTGRES_DSN")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("DATABASE_URL"))
}

func JWTSecretFromEnv() ([]byte, error) {
	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if secret == "" {
		return nil, errors.New("JWT_SECRET is required when platform identity is enabled")
	}
	return []byte(secret), nil
}

func RunPlatformAdminBootstrap(ctx context.Context, opener func(context.Context, string, []byte) (PasswordUserEnsurer, error)) error {
	dsn := PlatformDSNFromEnv()
	if dsn == "" {
		return errors.New("POSTGRES_DSN (or DATABASE_URL) is required for PLATFORM_ADMIN_BOOTSTRAP_ONLY")
	}
	jwtSecret, err := JWTSecretFromEnv()
	if err != nil {
		return err
	}
	store, err := opener(ctx, dsn, jwtSecret)
	if err != nil {
		return err
	}
	return SeedPlatformAdminFromEnv(ctx, store)
}

func SeedPlatformAdminFromEnv(ctx context.Context, store PasswordUserEnsurer) error {
	email := strings.TrimSpace(os.Getenv("PLATFORM_ADMIN_EMAIL"))
	password := strings.TrimSpace(os.Getenv("PLATFORM_ADMIN_PASSWORD"))
	if email == "" && password == "" {
		return nil
	}
	if email == "" || password == "" {
		return errors.New("PLATFORM_ADMIN_EMAIL and PLATFORM_ADMIN_PASSWORD must both be set")
	}
	u, err := store.EnsurePasswordUser(ctx, email, password, "admin")
	if err != nil {
		return err
	}
	log.Printf("platform admin user ensured email=%q", u.Email)
	return nil
}

func SeedPlatformDevUsersFromEnv(ctx context.Context, store PasswordUserEnsurer, boolEnv func(string) (bool, bool), envOr func(string, string) string) error {
	enabled, ok := boolEnv("PLATFORM_DEV_LOGIN_ENABLED")
	if !ok || !enabled {
		return nil
	}
	seeds := []struct {
		label           string
		role            string
		emailEnv        string
		passwordEnv     string
		defaultEmail    string
		defaultPassword string
	}{
		{
			label:           "test",
			role:            "user",
			emailEnv:        "PLATFORM_DEV_USER_EMAIL",
			passwordEnv:     "PLATFORM_DEV_USER_PASSWORD",
			defaultEmail:    DefaultDevUserEmail,
			defaultPassword: DefaultDevUserPassword,
		},
		{
			label:           "admin",
			role:            "admin",
			emailEnv:        "PLATFORM_DEV_ADMIN_EMAIL",
			passwordEnv:     "PLATFORM_DEV_ADMIN_PASSWORD",
			defaultEmail:    DefaultDevAdminEmail,
			defaultPassword: DefaultDevAdminPassword,
		},
	}
	for _, seed := range seeds {
		email := strings.ToLower(strings.TrimSpace(envOr(seed.emailEnv, seed.defaultEmail)))
		password := strings.TrimSpace(envOr(seed.passwordEnv, seed.defaultPassword))
		if email == "" || password == "" {
			return fmt.Errorf("%s dev login requires both email and password", seed.label)
		}
		u, err := store.EnsurePasswordUser(ctx, email, password, seed.role)
		if err != nil {
			return fmt.Errorf("ensure %s dev login: %w", seed.label, err)
		}
		log.Printf("platform dev %s login ensured email=%q", seed.label, u.Email)
	}
	return nil
}
