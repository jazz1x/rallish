package adapter

import (
	"context"
	"testing"

	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/stretchr/testify/require"
)

func TestRegistryRegisterGetNames(t *testing.T) {
	r := NewRegistry()

	a1 := &stubAdapter{name: "a1"}
	a2 := &stubAdapter{name: "a2"}

	require.NoError(t, r.Register("a1", a1))
	require.Error(t, r.Register("a1", a1)) // duplicate

	got, ok := r.Get("a1")
	require.True(t, ok)
	require.Equal(t, a1, got)

	_, ok = r.Get("missing")
	require.False(t, ok)

	names := r.Names()
	require.Equal(t, []string{"a1"}, names)

	require.NoError(t, r.Register("a2", a2))
	names = r.Names()
	require.Equal(t, []string{"a1", "a2"}, names)
}

func TestRegistryCheckAll(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register("ok", &stubAdapter{name: "ok"}))
	require.NoError(t, r.Register("fail", &stubAdapter{name: "fail", checkErr: errTest}))

	results := r.CheckAll()
	require.NoError(t, results["ok"])
	require.ErrorIs(t, results["fail"], errTest)
}

var errTest = errTestType{}

type errTestType struct{}

func (errTestType) Error() string { return "test error" }

type stubAdapter struct {
	name     string
	checkErr error
}

func (f *stubAdapter) Name() string { return f.name }
func (f *stubAdapter) Run(_ context.Context, _ contract.TurnRequest) (contract.TurnResponse, error) {
	return contract.TurnResponse{}, nil
}
func (f *stubAdapter) Check() error { return f.checkErr }
