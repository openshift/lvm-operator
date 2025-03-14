// Code generated by mockery v2.52.3. DO NOT EDIT.

package lvmd

import (
	context "context"

	app "github.com/topolvm/topolvm/cmd/lvmd/app"

	mock "github.com/stretchr/testify/mock"
)

// MockConfigurator is an autogenerated mock type for the Configurator type
type MockConfigurator struct {
	mock.Mock
}

type MockConfigurator_Expecter struct {
	mock *mock.Mock
}

func (_m *MockConfigurator) EXPECT() *MockConfigurator_Expecter {
	return &MockConfigurator_Expecter{mock: &_m.Mock}
}

// Delete provides a mock function with given fields: ctx
func (_m *MockConfigurator) Delete(ctx context.Context) error {
	ret := _m.Called(ctx)

	if len(ret) == 0 {
		panic("no return value specified for Delete")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context) error); ok {
		r0 = rf(ctx)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// MockConfigurator_Delete_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Delete'
type MockConfigurator_Delete_Call struct {
	*mock.Call
}

// Delete is a helper method to define mock.On call
//   - ctx context.Context
func (_e *MockConfigurator_Expecter) Delete(ctx interface{}) *MockConfigurator_Delete_Call {
	return &MockConfigurator_Delete_Call{Call: _e.mock.On("Delete", ctx)}
}

func (_c *MockConfigurator_Delete_Call) Run(run func(ctx context.Context)) *MockConfigurator_Delete_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(context.Context))
	})
	return _c
}

func (_c *MockConfigurator_Delete_Call) Return(_a0 error) *MockConfigurator_Delete_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *MockConfigurator_Delete_Call) RunAndReturn(run func(context.Context) error) *MockConfigurator_Delete_Call {
	_c.Call.Return(run)
	return _c
}

// Load provides a mock function with given fields: ctx
func (_m *MockConfigurator) Load(ctx context.Context) (*app.Config, error) {
	ret := _m.Called(ctx)

	if len(ret) == 0 {
		panic("no return value specified for Load")
	}

	var r0 *app.Config
	var r1 error
	if rf, ok := ret.Get(0).(func(context.Context) (*app.Config, error)); ok {
		return rf(ctx)
	}
	if rf, ok := ret.Get(0).(func(context.Context) *app.Config); ok {
		r0 = rf(ctx)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*app.Config)
		}
	}

	if rf, ok := ret.Get(1).(func(context.Context) error); ok {
		r1 = rf(ctx)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// MockConfigurator_Load_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Load'
type MockConfigurator_Load_Call struct {
	*mock.Call
}

// Load is a helper method to define mock.On call
//   - ctx context.Context
func (_e *MockConfigurator_Expecter) Load(ctx interface{}) *MockConfigurator_Load_Call {
	return &MockConfigurator_Load_Call{Call: _e.mock.On("Load", ctx)}
}

func (_c *MockConfigurator_Load_Call) Run(run func(ctx context.Context)) *MockConfigurator_Load_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(context.Context))
	})
	return _c
}

func (_c *MockConfigurator_Load_Call) Return(_a0 *app.Config, _a1 error) *MockConfigurator_Load_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

func (_c *MockConfigurator_Load_Call) RunAndReturn(run func(context.Context) (*app.Config, error)) *MockConfigurator_Load_Call {
	_c.Call.Return(run)
	return _c
}

// Save provides a mock function with given fields: ctx, config
func (_m *MockConfigurator) Save(ctx context.Context, config *app.Config) error {
	ret := _m.Called(ctx, config)

	if len(ret) == 0 {
		panic("no return value specified for Save")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, *app.Config) error); ok {
		r0 = rf(ctx, config)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// MockConfigurator_Save_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Save'
type MockConfigurator_Save_Call struct {
	*mock.Call
}

// Save is a helper method to define mock.On call
//   - ctx context.Context
//   - config *app.Config
func (_e *MockConfigurator_Expecter) Save(ctx interface{}, config interface{}) *MockConfigurator_Save_Call {
	return &MockConfigurator_Save_Call{Call: _e.mock.On("Save", ctx, config)}
}

func (_c *MockConfigurator_Save_Call) Run(run func(ctx context.Context, config *app.Config)) *MockConfigurator_Save_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(context.Context), args[1].(*app.Config))
	})
	return _c
}

func (_c *MockConfigurator_Save_Call) Return(_a0 error) *MockConfigurator_Save_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *MockConfigurator_Save_Call) RunAndReturn(run func(context.Context, *app.Config) error) *MockConfigurator_Save_Call {
	_c.Call.Return(run)
	return _c
}

// NewMockConfigurator creates a new instance of MockConfigurator. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMockConfigurator(t interface {
	mock.TestingT
	Cleanup(func())
}) *MockConfigurator {
	mock := &MockConfigurator{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
