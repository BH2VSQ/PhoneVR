package main

import (
	"context"
	"time"
)

func httpShutdownContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 3*time.Second)
}
