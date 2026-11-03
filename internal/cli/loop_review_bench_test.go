package cli

import (
	"context"
	"errors"

	"github.com/byranZA/smith/internal/loop"
)

// noReview is the part of a bench for a test that never reviews: its review methods fail.
type noReview struct{}

func (noReview) OpenReview(context.Context, int) (string, error) {
	return "", errors.New("unexpected review")
}

func (noReview) ReviewBase(context.Context) (string, string, error) {
	return "", "", errors.New("unexpected review")
}

func (noReview) ReviewStrays(context.Context, string, string) (loop.Strays, error) {
	return loop.Strays{}, errors.New("unexpected review")
}

func (noReview) FindingsFile(int) string { return "" }
