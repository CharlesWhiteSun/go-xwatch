//go:build !windows

package service

import "errors"

var ErrAlreadyRunning = errors.New("unsupported platform")

func IsWindowsServiceProcess() bool { return false }
func InstallOrUpdate(_ string, _ string, _ ...string) error {
	return errors.New("unsupported platform")
}
func Start(_ string) error            { return errors.New("unsupported platform") }
func Stop(_ string) error             { return errors.New("unsupported platform") }
func Uninstall(_ string) error        { return errors.New("unsupported platform") }
func Status(_ string) (string, error) { return "", errors.New("unsupported platform") }
func Run(_ string, _ string) error    { return errors.New("unsupported platform") }
