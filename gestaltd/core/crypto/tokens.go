package crypto

import "fmt"

func (e *AESGCMEncryptor) EncryptTokenPair(accessToken, refreshToken string) (accessEnc, refreshEnc string, err error) {
	return transformTokenPair(accessToken, refreshToken, "encrypting", e.Encrypt)
}

func (e *AESGCMEncryptor) DecryptTokenPair(accessEnc, refreshEnc string) (accessToken, refreshToken string, err error) {
	return transformTokenPair(accessEnc, refreshEnc, "decrypting", e.Decrypt)
}

func transformTokenPair(access, refresh, action string, transform func(string) (string, error)) (string, string, error) {
	accessResult, err := transform(access)
	if err != nil {
		return "", "", fmt.Errorf("%s access token: %w", action, err)
	}
	refreshResult, err := transform(refresh)
	if err != nil {
		return "", "", fmt.Errorf("%s refresh token: %w", action, err)
	}
	return accessResult, refreshResult, nil
}
