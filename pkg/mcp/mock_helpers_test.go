package mcp

import (
	"go.uber.org/mock/gomock"
)

// setupMockAgentClient configures a generated MockAgentClient with common defaults
// that match the behavior of the old hand-rolled mock constructor.
// CallTool is NOT configured - set it explicitly per test as needed.
func setupMockAgentClient(ctrl *gomock.Controller, name string, tools []Tool) *MockAgentClient {
	mock := NewMockAgentClient(ctrl)
	mock.EXPECT().Name().Return(name).AnyTimes()
	mock.EXPECT().Tools().Return(tools).AnyTimes()
	mock.EXPECT().IsInitialized().Return(true).AnyTimes()
	mock.EXPECT().ServerInfo().Return(ServerInfo{Name: name, Version: "1.0.0"}).AnyTimes()
	mock.EXPECT().Initialize(gomock.Any()).Return(nil).AnyTimes()
	mock.EXPECT().RefreshTools(gomock.Any()).Return(nil).AnyTimes()
	return mock
}
