// Package operationmock provides generated mocks for tests outside the operation package.
package operationmock

//go:generate go tool mockgen -source=../../operation/operation.go -destination=interfaces_mock.go -package=operationmock -mock_names=Prepared=MockOperationPrepared,Delivery=MockOperationDelivery
