package highlight

import (
	"context"
	"fmt"
	"testing"
)

// TestConsumeError tests every case for ConsumeError
func TestConsumeError(t *testing.T) {
	requester = mockRequester{}
	ctx := context.Background()
	ctx = context.WithValue(ctx, ContextKeys.SessionSecureID, "0")
	ctx = context.WithValue(ctx, ContextKeys.RequestID, "0")
	tests := map[string]struct {
		errorInput    interface{}
		contextInput  context.Context
		tags          []string
		expectedEvent string
		expectedError error
	}{
		"test builtin error":                      {contextInput: ctx, errorInput: fmt.Errorf("error here"), expectedEvent: "error here"},
		"test builtin error with invalid context": {contextInput: context.Background(), errorInput: fmt.Errorf("error here"), expectedError: fmt.Errorf(consumeErrorSessionIDMissing)},
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			Start()
			err := ConsumeError(input.contextInput, input.errorInput, input.tags...)
			if err != nil {
				if input.expectedError == nil || err.Error() != input.expectedError.Error() {
					t.Errorf("received error not equal to expected error: %v != %v", err, input.expectedError)
				}
				return
			}
			a := flush()
			if len(a) != 1 {
				t.Errorf("flush returned the wrong number of errors")
				return
			}
			if string(a[0].Event) != input.expectedEvent {
				t.Errorf("event not equal to expected event: %v != %v", a[0].Event, input.expectedEvent)
				return
			}
		})
	}
	Stop()
}
