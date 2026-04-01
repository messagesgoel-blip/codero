package grpc

import (
	"context"
	"testing"
)

func TestRegisterWithSession_RequiresSessionID(t *testing.T) {
	client := &SessionClient{}

	if _, err := client.RegisterWithSession(context.Background(), "", "agent", "agent"); err == nil {
		t.Fatal("RegisterWithSession should reject an empty sessionID")
	}
}
