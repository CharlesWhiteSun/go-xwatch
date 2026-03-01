//go:build !windows

package crypto

import "errors"

func Protect(_ []byte) ([]byte, error)   { return nil, errors.New("dpapi not supported") }
func Unprotect(_ []byte) ([]byte, error) { return nil, errors.New("dpapi not supported") }
func LoadOrCreateKey(_ string, _ int) ([]byte, error) {
	return nil, errors.New("dpapi not supported")
}
