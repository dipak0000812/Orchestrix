package executor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestChecksumExecutor_SingleIteration(t *testing.T) {
	payload, _ := json.Marshal(ChecksumPayload{Data: "hello world"})
	exec := NewChecksumExecutor()

	if err := exec.Execute(context.Background(), payload); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestChainedDigest_MatchesManualChaining(t *testing.T) {
	data := []byte("hello world")

	// Compute the expected 3-iteration chain by hand: hash data, then hash
	// the result twice more.
	sum := sha256.Sum256(data)
	sum = sha256.Sum256(sum[:])
	sum = sha256.Sum256(sum[:])
	want := sum[:]

	got, err := chainedDigest(context.Background(), data, 3)
	if err != nil {
		t.Fatalf("chainedDigest failed: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("chainedDigest(_, _, 3) = %x, want %x", got, want)
	}
}

func TestChainedDigest_SingleIterationIsPlainSHA256(t *testing.T) {
	data := []byte("hello world")
	want := sha256.Sum256(data)

	got, err := chainedDigest(context.Background(), data, 1)
	if err != nil {
		t.Fatalf("chainedDigest failed: %v", err)
	}
	if string(got) != string(want[:]) {
		t.Errorf("chainedDigest(_, _, 1) = %x, want %x", got, want)
	}
}

func TestChecksumExecutor_MissingDataIsPermanent(t *testing.T) {
	payload, _ := json.Marshal(ChecksumPayload{})
	exec := NewChecksumExecutor()

	err := exec.Execute(context.Background(), payload)
	var permErr *PermanentError
	if !errors.As(err, &permErr) {
		t.Errorf("missing data should be wrapped as PermanentError, got: %v", err)
	}
}

func TestChecksumExecutor_InvalidPayloadIsPermanent(t *testing.T) {
	exec := NewChecksumExecutor()

	err := exec.Execute(context.Background(), []byte("not json"))
	var permErr *PermanentError
	if !errors.As(err, &permErr) {
		t.Errorf("invalid JSON should be wrapped as PermanentError, got: %v", err)
	}
}

func TestChecksumExecutor_RespectsCancelledContext(t *testing.T) {
	payload, _ := json.Marshal(ChecksumPayload{Data: "x", WorkFactor: maxWorkFactor})
	exec := NewChecksumExecutor()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := exec.Execute(ctx, payload)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestChecksumExecutor_WorkFactorAboveCapIsPermanent(t *testing.T) {
	payload, _ := json.Marshal(ChecksumPayload{Data: "x", WorkFactor: maxWorkFactor + 1})
	exec := NewChecksumExecutor()

	err := exec.Execute(context.Background(), payload)
	if err == nil {
		t.Fatal("expected an error for work_factor exceeding the cap")
	}
	var permErr *PermanentError
	if !errors.As(err, &permErr) {
		t.Errorf("work_factor above the cap should be PermanentError (retrying won't help), got: %v", err)
	}
}

func TestChecksumExecutor_WorkFactorAtCapIsAllowed(t *testing.T) {
	payload, _ := json.Marshal(ChecksumPayload{Data: "x", WorkFactor: maxWorkFactor})
	exec := NewChecksumExecutor()

	if err := exec.Execute(context.Background(), payload); err != nil {
		t.Errorf("work_factor exactly at the cap should be allowed, got: %v", err)
	}
}

func TestChecksumExecutor_DataAboveMaxSizeIsPermanent(t *testing.T) {
	oversized := make([]byte, maxChecksumDataBytes+1)
	for i := range oversized {
		oversized[i] = 'x'
	}
	payload, _ := json.Marshal(ChecksumPayload{Data: string(oversized)})
	exec := NewChecksumExecutor()

	err := exec.Execute(context.Background(), payload)
	if err == nil {
		t.Fatal("expected an error for data exceeding the max size")
	}
	var permErr *PermanentError
	if !errors.As(err, &permErr) {
		t.Errorf("oversized data should be PermanentError, got: %v", err)
	}
}

func TestChecksumExecutor_DefaultWorkFactor(t *testing.T) {
	// WorkFactor of 0 (unset) and 1 should behave identically.
	payload0, _ := json.Marshal(ChecksumPayload{Data: "x", WorkFactor: 0})
	payload1, _ := json.Marshal(ChecksumPayload{Data: "x", WorkFactor: 1})
	exec := NewChecksumExecutor()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := exec.Execute(ctx, payload0); err != nil {
		t.Fatalf("Execute (WorkFactor=0) failed: %v", err)
	}
	if err := exec.Execute(ctx, payload1); err != nil {
		t.Fatalf("Execute (WorkFactor=1) failed: %v", err)
	}
}
