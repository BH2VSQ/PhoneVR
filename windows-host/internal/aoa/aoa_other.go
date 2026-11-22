//go:build !windows || !amd64

package aoa

import (
	"context"
	"errors"
)

var ErrUnsupported = errors.New("PhoneVR AOA USB transport currently requires Windows amd64")

func NextAccessory(ctx context.Context) (*Device, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type Device struct{}

func (*Device) Read([]byte) (int, error)  { return 0, ErrUnsupported }
func (*Device) Write([]byte) (int, error) { return 0, ErrUnsupported }
func (*Device) Close() error              { return nil }
func (*Device) ID() string                { return "" }
