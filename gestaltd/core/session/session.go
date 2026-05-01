package session

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/valon-technologies/gestalt/server/core"
)

var ErrNotJWT = errors.New("not a JWT")

const (
	Issuer   = "gestaltd"
	Audience = "gestalt-session"
)

type claims struct {
	jwt.RegisteredClaims
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
}

func IssueToken(identity *core.UserIdentity, secret []byte, ttl time.Duration) (string, error) {
	c := claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Audience:  jwt.ClaimStrings{Audience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Email:       identity.Email,
		DisplayName: identity.DisplayName,
		AvatarURL:   identity.AvatarURL,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return token.SignedString(secret)
}

func ValidateToken(tokenStr string, secret []byte) (*core.UserIdentity, error) {
	token, err := jwt.ParseWithClaims(
		tokenStr,
		&claims{},
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return secret, nil
		},
		jwt.WithIssuer(Issuer),
		jwt.WithAudience(Audience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenMalformed) {
			return nil, ErrNotJWT
		}
		return nil, err
	}

	c, ok := token.Claims.(*claims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}

	return &core.UserIdentity{
		Email:       c.Email,
		DisplayName: c.DisplayName,
		AvatarURL:   c.AvatarURL,
	}, nil
}
