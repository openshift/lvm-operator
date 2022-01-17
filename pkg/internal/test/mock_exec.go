package test

type MockExecutor struct {
	MockExecuteCommandWithOutput       func(command string, arg ...string) (string, error)
	MockExecuteCommandWithOutputAsHost func(command string, arg ...string) (string, error)
}

// ExecuteCommandWithOutput mocks ExecuteCommandWithOutput
func (e *MockExecutor) ExecuteCommandWithOutput(command string, arg ...string) (string, error) {
	if e.MockExecuteCommandWithOutput != nil {
		return e.MockExecuteCommandWithOutput(command, arg...)
	}

	return "", nil
}

// ExecuteCommandWithOutputAsHost mocks ExecuteCommandWithOutputAsHost
func (e *MockExecutor) ExecuteCommandWithOutputAsHost(command string, arg ...string) (string, error) {
	if e.MockExecuteCommandWithOutputAsHost != nil {
		return e.MockExecuteCommandWithOutputAsHost(command, arg...)
	}

	return "", nil
}
