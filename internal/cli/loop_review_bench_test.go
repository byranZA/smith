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

// reviewingBench is a bench whose review opens without fault, with a findings file at file.
type reviewingBench struct {
	benchAt
	file string
}

func (reviewingBench) OpenReview(context.Context, int) (string, error) { return "/bench/review", nil }

func (reviewingBench) ReviewBase(context.Context) (string, string, error) {
	return "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222", nil
}

func (reviewingBench) ReviewStrays(context.Context, string, string) (loop.Strays, error) {
	return loop.Strays{}, nil
}

func (r reviewingBench) FindingsFile(int) string { return r.file }
