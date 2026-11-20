//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test

package httpapi

import "context"

type readinessChecker interface {
	Ping(ctx context.Context) error
}
