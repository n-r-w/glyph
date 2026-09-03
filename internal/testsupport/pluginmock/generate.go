// Package pluginmock provides generated mocks for Host plugin tests.
package pluginmock

//go:generate go tool mockgen -source=../../../sdk/plugins/ui/v1/service.go -destination=ui_interfaces_mock.go -package=pluginmock -mock_names=Service=MockUIService,InitializeOperation=MockUIInitializeOperation
//go:generate go tool mockgen -source=../../../sdk/plugins/extension/v1/service.go -destination=extension_interfaces_mock.go -package=pluginmock -mock_names=Service=MockExtensionService,RegisterOperation=MockExtensionRegisterOperation,HandleOperation=MockExtensionHandleOperation,ExecuteOperation=MockExtensionExecuteOperation
