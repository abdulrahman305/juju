// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/juju/charm/v9 (interfaces: Bundle)

// Package mocks is a generated GoMock package.
package mocks

import (
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	charm "github.com/juju/charm/v9"
)

// MockBundle is a mock of Bundle interface.
type MockBundle struct {
	ctrl     *gomock.Controller
	recorder *MockBundleMockRecorder
}

// MockBundleMockRecorder is the mock recorder for MockBundle.
type MockBundleMockRecorder struct {
	mock *MockBundle
}

// NewMockBundle creates a new mock instance.
func NewMockBundle(ctrl *gomock.Controller) *MockBundle {
	mock := &MockBundle{ctrl: ctrl}
	mock.recorder = &MockBundleMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockBundle) EXPECT() *MockBundleMockRecorder {
	return m.recorder
}

// ContainsOverlays mocks base method.
func (m *MockBundle) ContainsOverlays() bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainsOverlays")
	ret0, _ := ret[0].(bool)
	return ret0
}

// ContainsOverlays indicates an expected call of ContainsOverlays.
func (mr *MockBundleMockRecorder) ContainsOverlays() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainsOverlays", reflect.TypeOf((*MockBundle)(nil).ContainsOverlays))
}

// Data mocks base method.
func (m *MockBundle) Data() *charm.BundleData {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Data")
	ret0, _ := ret[0].(*charm.BundleData)
	return ret0
}

// Data indicates an expected call of Data.
func (mr *MockBundleMockRecorder) Data() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Data", reflect.TypeOf((*MockBundle)(nil).Data))
}

// ReadMe mocks base method.
func (m *MockBundle) ReadMe() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ReadMe")
	ret0, _ := ret[0].(string)
	return ret0
}

// ReadMe indicates an expected call of ReadMe.
func (mr *MockBundleMockRecorder) ReadMe() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ReadMe", reflect.TypeOf((*MockBundle)(nil).ReadMe))
}
