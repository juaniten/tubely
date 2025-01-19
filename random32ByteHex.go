package main

import (
	"crypto/rand"
	"encoding/base64"
)

func getRandom32ByteHex() (encodedString string, err error) {
	randomBytes := make([]byte, 32)
	_, err = rand.Read(randomBytes)
	if err != nil {
		return "", err
	}
	encodedString = base64.RawURLEncoding.EncodeToString(randomBytes)
	return encodedString, nil
}
