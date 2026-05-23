package cycle

import (
	"errors"
	"fmt"
	"testing"
)

func TestResultIsSuccess(t *testing.T) {
	r := Success(42)
	if !r.IsSuccess() {
		t.Fatal("expected success")
	}
	if r.IsFailure() {
		t.Fatal("expected not failure")
	}
	if r.Value() != 42 {
		t.Fatalf("value = %d, want 42", r.Value())
	}
	if r.Err() != nil {
		t.Fatalf("err = %v, want nil", r.Err())
	}
}

func TestResultIsFailure(t *testing.T) {
	err := errors.New("boom")
	r := Failure(0, err)
	if r.IsSuccess() {
		t.Fatal("expected not success")
	}
	if !r.IsFailure() {
		t.Fatal("expected failure")
	}
	if r.Value() != 0 {
		t.Fatalf("value = %d, want 0", r.Value())
	}
	if !errors.Is(r.Err(), err) {
		t.Fatalf("err = %v, want %v", r.Err(), err)
	}
}

func TestResultThenSuccess(t *testing.T) {
	r := Success(1).
		Then(func(v int) Result[int] { return Success(v + 1) }).
		Then(func(v int) Result[int] { return Success(v * 2) })
	if !r.IsSuccess() {
		t.Fatalf("expected success, got %v", r.Err())
	}
	if r.Value() != 4 {
		t.Fatalf("value = %d, want 4", r.Value())
	}
}

func TestResultThenShortCircuit(t *testing.T) {
	boom := errors.New("boom")
	r := Success(1).
		Then(func(_ int) Result[int] { return Failure(0, boom) }).
		Then(func(v int) Result[int] { return Success(v + 100) }) // never called
	if r.IsSuccess() {
		t.Fatal("expected failure")
	}
	if !errors.Is(r.Err(), boom) {
		t.Fatalf("err = %v, want %v", r.Err(), boom)
	}
}

func TestMap(t *testing.T) {
	r := Success(21)
	mapped := Map(r, func(v int) string { return fmt.Sprintf("%d", v*2) })
	if !mapped.IsSuccess() {
		t.Fatalf("expected success, got %v", mapped.Err())
	}
	if mapped.Value() != "42" {
		t.Fatalf("value = %q, want 42", mapped.Value())
	}
}

func TestMapFailure(t *testing.T) {
	boom := errors.New("boom")
	r := Failure(0, boom)
	mapped := Map(r, func(_ int) string { return "never" })
	if mapped.IsSuccess() {
		t.Fatal("expected failure")
	}
	if !errors.Is(mapped.Err(), boom) {
		t.Fatalf("err = %v, want %v", mapped.Err(), boom)
	}
}

func TestFlatMap(t *testing.T) {
	r := Success(1)
	mapped := FlatMap(r, func(v int) Result[string] {
		return Success(fmt.Sprintf("%d", v+1))
	})
	if !mapped.IsSuccess() {
		t.Fatalf("expected success, got %v", mapped.Err())
	}
	if mapped.Value() != "2" {
		t.Fatalf("value = %q, want 2", mapped.Value())
	}
}

func TestCombine(t *testing.T) {
	a := Success(10)
	b := Success(20)
	r := Combine(a, b, func(x, y int) int { return x + y })
	if !r.IsSuccess() {
		t.Fatalf("expected success, got %v", r.Err())
	}
	if r.Value() != 30 {
		t.Fatalf("value = %d, want 30", r.Value())
	}
}

func TestCombineFirstFailureWins(t *testing.T) {
	boom := errors.New("boom")
	a := Failure(0, boom)
	b := Success(20)
	r := Combine(a, b, func(x, y int) int { return x + y })
	if r.IsSuccess() {
		t.Fatal("expected failure")
	}
	if !errors.Is(r.Err(), boom) {
		t.Fatalf("err = %v, want %v", r.Err(), boom)
	}
}

func TestHaltedError(t *testing.T) {
	he := &HaltedError{Reason: "test-reason"}
	if he.Error() != "cycle halted: test-reason" {
		t.Fatalf("error = %q", he.Error())
	}
}
