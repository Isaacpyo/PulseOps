package httpapi

import (
	"io"
	"log/slog"
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/isaacpyo/pulseops/apps/api/internal/auth"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	auth.BcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}
