package apperrors

import "errors"

// Configuration errors
var (
	ErrAddrEmpty      = errors.New("server address is empty")
	ErrDSNEmpty       = errors.New("database DSN is empty")
	ErrAccrualSysAddr = errors.New("accrual system address is empty")
)
