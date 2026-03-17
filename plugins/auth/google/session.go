package google

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/valon-technologies/toolshed/core"
)

var errNotJWT = errors.New("not a JWT")

type sessionClaims struct {
	jwt.RegisteredClaims
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
}

func issueSessionToken(identity *core.UserIdentity, secret []byte, ttl time.Duration) (string, error) {
	claims := sessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Email:       identity.Email,
		DisplayName: identity.DisplayName,
		AvatarURL:   identity.AvatarURL,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

func validateSessionToken(tokenStr string, secret []byte) (*core.UserIdentity, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &sessionClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		// Distinguish "not a JWT at all" from "JWT but invalid"
		if errors.Is(err, jwt.ErrTokenMalformed) {
			return nil, errNotJWT
		}
		return nil, err
	}

	claims, ok := token.Claims.(*sessionClaims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}

	return &core.UserIdentity{
		Email:       claims.Email,
		DisplayName: claims.DisplayName,
		AvatarURL:   claims.AvatarURL,
	}, nil
}
