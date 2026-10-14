//go:build !integration

package runtime

//go:generate go tool mockgen -destination=stream_mock_test.go -package=runtime github.com/n-r-w/glyph/pkg/plugins/ui/v1 UIService_OpenClient
//go:generate go tool mockgen -destination=ui_sdk_mock_test.go -package=runtime -mock_names=Service=MockSDKUIService,InitializeOperation=MockSDKInitializeOperation github.com/n-r-w/glyph/sdk/plugins/ui/v1 Service,InitializeOperation
