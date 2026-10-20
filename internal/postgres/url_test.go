package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ardzz/toe3-stormline-cloud/internal/postgres"
)

func TestMalformedURLNeverExposesCredentials(t *testing.T) {
	const url = "postgres://operator:supersecret"
	ctx := context.Background()
	_, openErr := postgres.Open(ctx, url)
	for name, err := range map[string]error{"Open": openErr, "Migrate": postgres.Migrate(ctx, url)} {
		if !errors.Is(err, postgres.ErrInvalidDatabaseURL) || strings.Contains(err.Error(), "supersecret") {
			t.Errorf("%s = %v; want ErrInvalidDatabaseURL without the password", name, err)
		}
	}
}
