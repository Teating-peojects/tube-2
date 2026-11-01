// Command devpass sets a password for a seeded DEVELOPMENT user, so you can log in locally.
//
//	DATABASE_URL=postgres://postgres:...@127.0.0.1:5432/redxtube?sslmode=disable \
//	DEV_PASSWORD='your-dev-password' go run ./cmd/devpass admin@redxtube.test
//
// It refuses to run against any non-local database. The hash uses argon2id with the parameters below; if your
// backend verifies passwords with different parameters/encoding, call your backend's own hasher instead.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/argon2"
)

const (
	timeCost  = 3
	memoryKiB = 64 * 1024
	threads   = 4
	keyLen    = 32
	saltLen   = 16
)

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}

// hashPassword returns a PHC-formatted argon2id string accepted by the users_password_hash_format CHECK.
func hashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, timeCost, memoryKiB, threads, keyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memoryKiB, timeCost, threads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: DATABASE_URL=... [DEV_PASSWORD=...] go run ./cmd/devpass <email>")
		os.Exit(2)
	}
	email := strings.ToLower(strings.TrimSpace(os.Args[1]))

	dbURL := os.Getenv("DATABASE_URL")
	u, err := url.Parse(dbURL)
	if dbURL == "" || err != nil {
		fail("DATABASE_URL is missing or invalid")
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
	default:
		fail("refusing to run against a non-local database")
	}

	password := os.Getenv("DEV_PASSWORD")
	if password == "" {
		fmt.Fprint(os.Stderr, "New dev password (input is visible): ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		password = strings.TrimRight(line, "\r\n")
	}
	if len(password) < 10 {
		fail("use at least 10 characters (same rule as the frontend)")
	}

	hash, err := hashPassword(password)
	if err != nil {
		fail(err.Error())
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		fail("cannot connect: " + err.Error())
	}
	defer conn.Close(ctx)

	tag, err := conn.Exec(ctx,
		`UPDATE users SET password_hash = $1, status = 'ACTIVE', is_verified = true WHERE email = $2`, hash, email)
	if err != nil {
		fail(err.Error())
	}
	if tag.RowsAffected() == 0 {
		fail("no user with that email")
	}
	fmt.Println("password updated for", email)
}
