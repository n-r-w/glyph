//go:build !integration

package runtime

//go:generate go tool mockgen -build_constraint=!integration -destination=stream_mock_test.go -package=runtime github.com/n-r-w/glyph/pkg/plugins/ui/v1 UIService_OpenClient
