package gemini

import (
	"context"
	"errors"
	"testing"
)

func TestIsTransientNetworkError(t *testing.T) {
	if !isTransientNetworkError(context.DeadlineExceeded) {
		t.Fatal("expected context deadline exceeded to be transient")
	}
	if !isTransientNetworkError(errors.New("context deadline exceeded (Client.Timeout or context cancellation while reading body)")) {
		t.Fatal("expected client timeout while reading body to be transient")
	}
	if isTransientNetworkError(errors.New("Gemini returned login/consent page")) {
		t.Fatal("expected login/consent errors to remain non-transient")
	}
}
