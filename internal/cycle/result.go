// Package cycle implements the autonomous-cycle domain layer.
package cycle

import (
	"fmt"

	"github.com/jazz1x/rallish/pkg/contract"
)

// Result is the Railway-Oriented Programming sum type for cycle operations.
// A Result is either Success (carrying a value) or Failure (carrying an error and optional state).
type Result[T any] struct {
	value   T
	err     error
	reports []contract.GateReport
}

// Success constructs a successful Result.
func Success[T any](value T, reports ...contract.GateReport) Result[T] {
	return Result[T]{value: value, reports: reports}
}

// Failure constructs a failed Result. The optional state is stored as the value
// so that halted state can still be retrieved.
func Failure[T any](value T, err error, reports ...contract.GateReport) Result[T] {
	return Result[T]{value: value, err: err, reports: reports}
}

// IsSuccess returns true if the operation succeeded.
func (r Result[T]) IsSuccess() bool { return r.err == nil }

// IsFailure returns true if the operation failed.
func (r Result[T]) IsFailure() bool { return r.err != nil }

// Value returns the success value. Callers must check IsSuccess first.
func (r Result[T]) Value() T { return r.value }

// Err returns the error, or nil if the operation succeeded.
func (r Result[T]) Err() error { return r.err }

// Reports returns the accumulated gate reports.
func (r Result[T]) Reports() []contract.GateReport { return r.reports }

// Then chains a function if the current result is a success.
// On failure, the function is skipped and the failure propagates.
func (r Result[T]) Then(fn func(T) Result[T]) Result[T] {
	if r.IsFailure() {
		return r
	}
	return fn(r.value)
}

// Map converts a Result[T] to Result[U] via a pure function.
// On failure, the error propagates with the zero value of U.
func Map[T, U any](r Result[T], fn func(T) U) Result[U] {
	if r.IsFailure() {
		var zero U
		return Failure(zero, r.err, r.reports...)
	}
	return Success(fn(r.value), r.reports...)
}

// FlatMap converts a Result[T] to Result[U] via a function that itself returns a Result.
func FlatMap[T, U any](r Result[T], fn func(T) Result[U]) Result[U] {
	if r.IsFailure() {
		var zero U
		return Failure(zero, r.err, r.reports...)
	}
	return fn(r.value)
}

// Combine merges two Results. If either is a failure, the first failure wins.
// On dual success, the combiner function is called.
func Combine[T any](a, b Result[T], combiner func(T, T) T) Result[T] {
	if a.IsFailure() {
		return a
	}
	if b.IsFailure() {
		return b
	}
	return Success(combiner(a.value, b.value), append(a.reports, b.reports...)...)
}

// HaltedError is the canonical error returned when a cycle halts.
type HaltedError struct {
	Reason  contract.HaltReason
	Reports []contract.GateReport
}

func (e *HaltedError) Error() string {
	return fmt.Sprintf("cycle halted: %s", e.Reason)
}
