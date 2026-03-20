package crypto

import "fmt"

func (e *AESGCMEncryptor) EncryptTokenPair(accessToken, refreshToken string) (accessEnc, refreshEnc string, err error) {
	accessEnc, err = e.Encrypt(accessToken)
	if err != nil {
		return "", "", fmt.Errorf("encrypting access token: %w", err)
	}
	refreshEnc, err = e.Encrypt(refreshToken)
	if err != nil {
		return "", "", fmt.Errorf("encrypting refresh token: %w", err)
	}
	return accessEnc, refreshEnc, nil
}

func (e *AESGCMEncryptor) DecryptTokenPair(accessEnc, refreshEnc string) (accessToken, refreshToken string, err error) {
	accessToken, err = e.Decrypt(accessEnc)
	if err != nil {
		return "", "", fmt.Errorf("decrypting access token: %w", err)
	}
	refreshToken, err = e.Decrypt(refreshEnc)
	if err != nil {
		return "", "", fmt.Errorf("decrypting refresh token: %w", err)
	}
	return accessToken, refreshToken, nil
}
