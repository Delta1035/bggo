package online

import "errors"

var (
	// ErrInvalidProvider indicates a nil provider or one without a source ID.
	ErrInvalidProvider = errors.New("无效的壁纸源")
	// ErrDuplicateProvider indicates that a source ID is already registered.
	ErrDuplicateProvider = errors.New("壁纸源 ID 已注册")
)
