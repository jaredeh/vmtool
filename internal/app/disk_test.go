package app

import (
	"context"
	"testing"
)

func TestAddDiskRejectsZeroSize(t *testing.T) {
	s := &Service{}
	_, err := s.AddDisk(context.Background(), nil, "web", 0, "")
	if err == nil {
		t.Fatal("expected error")
	}
	var ae *Error
	if !AsError(err, &ae) || ae.Kind != KindInvalid {
		t.Fatalf("got %#v", err)
	}
}
